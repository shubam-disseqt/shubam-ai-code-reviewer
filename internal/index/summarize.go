// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/indexer.py under Apache License 2.0.

package index

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"text/template"

	"github.com/shubam-disseqt/z-code-reviewer/internal/filetype"
	"github.com/shubam-disseqt/z-code-reviewer/internal/llm"
	"github.com/shubam-disseqt/z-code-reviewer/internal/prompts"
)

// summarizeTemplate is loaded lazily on first use so a corrupt embedded
// prompt surfaces as a normal error at index time, not a panic at package
// init that kills every zreview subcommand (including unrelated ones like
// `zreview docs`).
var (
	summarizeTemplateOnce sync.Once
	summarizeTemplateVal  *template.Template
	summarizeTemplateErr  error
)

func getSummarizeTemplate() (*template.Template, error) {
	summarizeTemplateOnce.Do(func() {
		data, err := prompts.Templates.ReadFile("summarize.md")
		if err != nil {
			summarizeTemplateErr = fmt.Errorf("index: read summarize template: %w", err)
			return
		}
		tpl, err := template.New("summarize").Parse(string(data))
		if err != nil {
			summarizeTemplateErr = fmt.Errorf("index: parse summarize template: %w", err)
			return
		}
		summarizeTemplateVal = tpl
	})
	return summarizeTemplateVal, summarizeTemplateErr
}

// summarizeFileJSON is the parsed shape of one file entry in the LLM
// response. All fields are optional — missing or explicit-null values are
// tolerated because the LLM occasionally emits either.
type summarizeFileJSON struct {
	Path             string             `json:"path"`
	Language         string             `json:"language"`
	Summary          string             `json:"summary"`
	Symbols          []summarizeSymbol  `json:"symbols"`
	Imports          []string           `json:"imports"`
	SymbolReferences []symbolRefBlock   `json:"symbol_references"`
	ExternalRefs     []externalRefBlock `json:"external_refs"`
}

type summarizeSymbol struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Signature   string `json:"signature"`
	Description string `json:"description"`
}

type symbolRefBlock struct {
	Source string        `json:"source"`
	Calls  []refCallBody `json:"calls"`
}

type refCallBody struct {
	Path   string `json:"path"`
	Symbol string `json:"symbol"`
}

type externalRefBlock struct {
	Kind        string `json:"kind"`
	Target      string `json:"target"`
	Description string `json:"description"`
}

type summarizeEnvelope struct {
	Files []summarizeFileJSON `json:"files"`
}

// summarizeBatch calls the LLM to summarize one batch of files. Failures
// return an empty slice and log context — one bad batch never aborts the
// run (see PORTING.md §7). ModelCap bounds max_tokens.
func summarizeBatch(ctx context.Context, client llm.LLMClient, model string, modelCap int, files []filePair) ([]summarizeFileJSON, error) {
	prompt, err := renderSummarizePrompt(files)
	if err != nil {
		return nil, err
	}
	req := buildSummarizeRequest(model, modelCap, prompt)
	resp, err := client.CompletionsWithCtx(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("index: summarize batch: %w", err)
	}
	raw := resp.Content()
	return parseSummarizeResponse(raw), nil
}

// renderSummarizePrompt fills the summarize template with the batch. The
// template holds the JSON schema documentation plus per-file bodies.
func renderSummarizePrompt(files []filePair) (string, error) {
	type tplFile struct {
		Path    string
		Content string
	}
	data := struct{ Files []tplFile }{Files: make([]tplFile, len(files))}
	for i, f := range files {
		data.Files[i] = tplFile{Path: f.Path, Content: f.Content}
	}
	tpl, err := getSummarizeTemplate()
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("index: render summarize prompt: %w", err)
	}
	return buf.String(), nil
}

func buildSummarizeRequest(model string, modelCap int, prompt string) llm.ChatRequest {
	// Mira sends the prompt body as system + a short user nudge — same here.
	temp := 0.0
	maxTokens := 16384
	if modelCap > 0 && modelCap < maxTokens {
		maxTokens = modelCap
	}
	if maxTokens > 32768 {
		maxTokens = 32768
	}
	return llm.ChatRequest{
		Model: model,
		Messages: []llm.Message{
			llm.NewTextMessage("system", prompt),
			llm.NewTextMessage("user", "Summarize the files above."),
		},
		Temperature: &temp,
		MaxTokens:   maxTokens,
	}
}

// parseSummarizeResponse extracts the file entries from raw. It's tolerant
// of markdown fences and `<think>...</think>` blocks and applies the
// two-pass backslash-escape repair — matches Mira's semantics.
func parseSummarizeResponse(raw string) []summarizeFileJSON {
	text := stripThinkBlocks(stripCodeFences(raw))

	// First pass: strict parse.
	if files, ok := tryParseSummary([]byte(text)); ok {
		return files
	}
	// Repair pass: fix lone backslashes inside string literals.
	repaired := escapeLoneBackslashes(text)
	if files, ok := tryParseSummary([]byte(repaired)); ok {
		return files
	}
	return nil
}

func tryParseSummary(data []byte) ([]summarizeFileJSON, bool) {
	dec := json.NewDecoder(bytes.NewReader(data))
	// Try {"files": [...]} first.
	var env summarizeEnvelope
	if err := dec.Decode(&env); err == nil && env.Files != nil {
		return env.Files, true
	}
	// Try a bare array.
	dec = json.NewDecoder(bytes.NewReader(data))
	var arr []summarizeFileJSON
	if err := dec.Decode(&arr); err == nil {
		return arr, true
	}
	return nil, false
}

// stripCodeFences removes a leading ```json / ``` fence and trailing ```
// from raw. Only touches fences that wrap the whole payload.
func stripCodeFences(raw string) string {
	text := strings.TrimSpace(raw)
	if !strings.HasPrefix(text, "```") {
		return text
	}
	// Drop the opening fence line entirely (```json, ```JSON, etc.).
	if nl := strings.IndexByte(text, '\n'); nl >= 0 {
		text = text[nl+1:]
	}
	text = strings.TrimRightFunc(text, func(r rune) bool { return r == ' ' || r == '\n' || r == '\t' || r == '\r' })
	if strings.HasSuffix(text, "```") {
		text = strings.TrimSuffix(text, "```")
	}
	return strings.TrimSpace(text)
}

// stripThinkBlocks removes <think>...</think> segments from raw. Some
// models leak reasoning into the visible response.
func stripThinkBlocks(raw string) string {
	out := raw
	for {
		start := strings.Index(out, "<think>")
		if start < 0 {
			return out
		}
		end := strings.Index(out[start:], "</think>")
		if end < 0 {
			// Unterminated <think> — drop the rest to be safe.
			return out[:start]
		}
		out = out[:start] + out[start+end+len("</think>"):]
	}
}

// validJSONEscapes is the set of characters that legally follow a
// backslash inside a JSON string literal.
var validJSONEscapes = map[byte]struct{}{
	'"': {}, '\\': {}, '/': {}, 'b': {}, 'f': {}, 'n': {}, 'r': {}, 't': {}, 'u': {},
}

// escapeLoneBackslashes doubles any backslash inside a JSON string literal
// that doesn't start a valid escape sequence. Fixes the DeepSeek/Windows-path
// case ("\App\Models") where the model emits raw `\` and json.Decoder bails
// with `invalid character 'A' after escape`. Runs on the raw text as a
// preprocessor; a well-formed response passes through unchanged.
func escapeLoneBackslashes(text string) string {
	var b strings.Builder
	b.Grow(len(text) + 8)
	inString := false
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if !inString {
			if ch == '"' {
				inString = true
			}
			b.WriteByte(ch)
			continue
		}
		if ch == '\\' {
			var next byte
			if i+1 < len(text) {
				next = text[i+1]
			}
			if _, ok := validJSONEscapes[next]; ok {
				b.WriteByte(ch)
				b.WriteByte(next)
				i++
				continue
			}
			b.WriteString(`\\`)
			continue
		}
		if ch == '"' {
			inString = false
		}
		b.WriteByte(ch)
	}
	return b.String()
}

// buildFileSummary converts one summarize response entry back into a
// FileSummary. Path and content come from the caller; content_hash and LOC
// are computed here so the same fields land whether the LLM echoed the path
// or not.
func buildFileSummary(path, content string, data summarizeFileJSON) FileSummary {
	fs := FileSummary{
		Path:        path,
		Language:    filetype.Language(strings.ToLower(data.Language)),
		Summary:     data.Summary,
		ContentHash: contentHash(content),
		LOC:         countLOC(content),
	}
	// If the LLM omitted or misidentified the language, fall back to the
	// extension-based guess so the row is never blank.
	if fs.Language == filetype.LangUnknown {
		fs.Language = filetype.LanguageFromPath(path)
	}

	for _, sym := range data.Symbols {
		if sym.Name == "" {
			continue
		}
		kind := sym.Kind
		if kind == "" {
			kind = "function"
		}
		fs.Symbols = append(fs.Symbols, SymbolInfo{
			Name:        sym.Name,
			Kind:        kind,
			Signature:   sym.Signature,
			Description: sym.Description,
		})
	}

	fs.Imports = append(fs.Imports, data.Imports...)

	for _, block := range data.SymbolReferences {
		if block.Source == "" {
			continue
		}
		for _, call := range block.Calls {
			if call.Path == "" || call.Symbol == "" {
				continue
			}
			fs.SymbolRefs = append(fs.SymbolRefs, SymbolRef{
				SourcePath:   path,
				SourceSymbol: block.Source,
				TargetPath:   call.Path,
				TargetSymbol: call.Symbol,
			})
		}
	}

	for _, xr := range data.ExternalRefs {
		if xr.Kind == "" || xr.Target == "" {
			continue
		}
		fs.ExternalRefs = append(fs.ExternalRefs, ExternalRef{
			Kind:        xr.Kind,
			Target:      xr.Target,
			Description: xr.Description,
		})
	}
	return fs
}
