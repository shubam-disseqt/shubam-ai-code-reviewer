// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors
// Portions Copyright 2026 shubam-ai-code-reviewer contributors — adapted from alibaba/open-code-review
// Adapted from alibaba/open-code-review internal/diff/relocation_test.go

package diff

import (
	"context"
	"errors"
	"testing"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/llm"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/model"
)

type mockLLMClient struct {
	response  *llm.ChatResponse
	err       error
	callCount int
}

func (m *mockLLMClient) CompletionsWithCtx(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	m.callCount++
	return m.response, m.err
}

func newMockResponse(content string) *llm.ChatResponse {
	return &llm.ChatResponse{
		Choices: []llm.Choice{
			{Message: llm.ResponseMessage{Role: "assistant", Content: &content}},
		},
	}
}

const (
	testSystemPrompt = "you are a helper"
	testUserTemplate = "diff:\n{diff}\ncode:\n{existing_code}\nsuggestion:\n{suggestion_content}"
)

func makeDiff() *model.Diff {
	return &model.Diff{
		NewPath: "main.go",
		Diff: `@@ -10,6 +10,8 @@
 import "fmt"

 func main() {
+    x := 1
+    y := 2
     fmt.Println("hello")
 }
`,
	}
}

func TestResolveComment_TextMatchSuccess(t *testing.T) {
	cm := model.LlmComment{
		Path:         "main.go",
		Content:      "unused variable",
		ExistingCode: "x := 1\ny := 2",
	}
	d := makeDiff()

	ok := ResolveComment(&cm, d)
	if !ok {
		t.Fatal("expected ResolveComment to succeed")
	}
	if cm.StartLine == 0 || cm.EndLine == 0 {
		t.Fatalf("expected non-zero lines, got %d-%d", cm.StartLine, cm.EndLine)
	}
}

func TestResolveComment_AlreadyResolved(t *testing.T) {
	cm := model.LlmComment{
		Path:         "main.go",
		Content:      "test",
		ExistingCode: "whatever",
		StartLine:    5,
		EndLine:      10,
	}
	d := makeDiff()
	ok := ResolveComment(&cm, d)
	if !ok {
		t.Fatal("expected true for already-resolved comment")
	}
	if cm.StartLine != 5 || cm.EndLine != 10 {
		t.Fatal("should not change already-resolved lines")
	}
}

func TestResolveComment_EmptyExistingCode(t *testing.T) {
	cm := model.LlmComment{Path: "main.go", Content: "test"}
	d := makeDiff()
	ok := ResolveComment(&cm, d)
	if ok {
		t.Fatal("expected false for empty ExistingCode")
	}
}

func TestReLocateComment_LLMReturnsValidCode(t *testing.T) {
	cm := model.LlmComment{
		Path:         "main.go",
		Content:      "unused variable",
		ExistingCode: "totally wrong code that won't match",
	}
	d := makeDiff()

	client := &mockLLMClient{
		response: newMockResponse("Here is the code:\n```go\nx := 1\ny := 2\n```\n"),
	}

	msgs := BuildReLocationMessages(&cm, d, testSystemPrompt, testUserTemplate)
	if len(msgs) == 0 {
		t.Fatal("expected non-empty messages")
	}

	ok, resp := ReLocateComment(context.Background(), &cm, d, client, msgs, "test-model", 1000)
	if !ok {
		t.Fatal("expected re-location to succeed")
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if cm.StartLine == 0 || cm.EndLine == 0 {
		t.Fatalf("expected non-zero lines after re-location, got %d-%d", cm.StartLine, cm.EndLine)
	}
}

func TestReLocateComment_LLMReturnsInvalidContent(t *testing.T) {
	cm := model.LlmComment{
		Path:         "main.go",
		Content:      "unused variable",
		ExistingCode: "totally wrong code",
	}
	d := makeDiff()

	client := &mockLLMClient{
		response: newMockResponse("I cannot find the code."),
	}

	msgs := BuildReLocationMessages(&cm, d, testSystemPrompt, testUserTemplate)
	ok, resp := ReLocateComment(context.Background(), &cm, d, client, msgs, "test-model", 1000)
	if ok {
		t.Fatal("expected re-location to fail for invalid LLM response")
	}
	if resp == nil {
		t.Fatal("expected non-nil response even on failure")
	}
	if cm.StartLine != 0 || cm.EndLine != 0 {
		t.Fatal("lines should remain 0-0")
	}
}

func TestReLocateComment_LLMError(t *testing.T) {
	cm := model.LlmComment{
		Path:         "main.go",
		Content:      "test",
		ExistingCode: "bad code",
	}
	d := makeDiff()

	client := &mockLLMClient{err: errors.New("network error")}

	msgs := BuildReLocationMessages(&cm, d, testSystemPrompt, testUserTemplate)
	ok, resp := ReLocateComment(context.Background(), &cm, d, client, msgs, "test-model", 1000)
	if ok {
		t.Fatal("expected false on LLM error")
	}
	if resp != nil {
		t.Fatal("expected nil response on error")
	}
}

// TestBuildReLocationMessages_Rendering pins the prompt the model receives,
// byte for byte, across the placeholder substitutions.
func TestBuildReLocationMessages_Rendering(t *testing.T) {
	cm := model.LlmComment{
		Path:         "main.go",
		Content:      "unused variable",
		ExistingCode: "x := 1",
	}
	d := makeDiff()

	msgs := BuildReLocationMessages(&cm, d, testSystemPrompt, testUserTemplate)
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if msgs[0].Role != "system" || msgs[0].ExtractText() != testSystemPrompt {
		t.Errorf("message 0 = %q/%q", msgs[0].Role, msgs[0].ExtractText())
	}
	want := "diff:\n" + d.Diff + "\ncode:\nx := 1\nsuggestion:\nunused variable"
	if msgs[1].Role != "user" || msgs[1].ExtractText() != want {
		t.Errorf("message 1 = %q/%q, want user/%q", msgs[1].Role, msgs[1].ExtractText(), want)
	}
}

// Empty userTemplate → nil messages; empty systemPrompt → user-only conversation.
func TestBuildReLocationMessages_NilOrEmptyTask(t *testing.T) {
	cm := model.LlmComment{
		Path:         "main.go",
		Content:      "test",
		ExistingCode: "bad code",
	}
	d := makeDiff()

	if msgs := BuildReLocationMessages(&cm, d, "", ""); msgs != nil {
		t.Fatalf("expected nil messages for empty template, got %d", len(msgs))
	}
	msgs := BuildReLocationMessages(&cm, d, "", testUserTemplate)
	if len(msgs) != 1 || msgs[0].Role != "user" {
		t.Fatalf("expected single user message when system is empty, got %+v", msgs)
	}
}

// Rollback branch: model returned a well-formed snippet that still does not
// appear in the diff, so the original ExistingCode must be restored.
func TestReLocateComment_CodeBlockStillUnresolvable(t *testing.T) {
	const original = "totally wrong code"
	cm := model.LlmComment{
		Path:         "main.go",
		Content:      "unused variable",
		ExistingCode: original,
	}
	d := makeDiff()

	client := &mockLLMClient{
		response: newMockResponse("```go\nnot in the diff either\n```"),
	}

	msgs := BuildReLocationMessages(&cm, d, testSystemPrompt, testUserTemplate)
	ok, resp := ReLocateComment(context.Background(), &cm, d, client, msgs, "test-model", 1000)
	if ok {
		t.Fatal("expected false when the new snippet still does not match")
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if cm.ExistingCode != original {
		t.Errorf("ExistingCode = %q, want the original %q restored", cm.ExistingCode, original)
	}
	if cm.StartLine != 0 || cm.EndLine != 0 {
		t.Errorf("lines = %d-%d, want 0-0", cm.StartLine, cm.EndLine)
	}
}

// ReLocateComment must not reach the client when messages is nil/empty.
func TestReLocateComment_NoMessages(t *testing.T) {
	cm := model.LlmComment{
		Path:         "main.go",
		Content:      "test",
		ExistingCode: "bad code",
	}
	d := makeDiff()
	client := &mockLLMClient{response: newMockResponse("```go\nx := 1\n```")}

	ok, resp := ReLocateComment(context.Background(), &cm, d, client, nil, "test-model", 1000)
	if ok {
		t.Fatal("expected false when there are no messages")
	}
	if resp != nil {
		t.Fatal("expected nil response when there are no messages")
	}
	if client.callCount != 0 {
		t.Fatalf("expected no LLM call, got %d", client.callCount)
	}
}

func TestExtractCodeBlock(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"with language tag", "```go\nfoo\nbar\n```", "foo\nbar"},
		{"without language tag", "```\nfoo\n```", "foo"},
		{"with surrounding text", "Here:\n```\ncode\n```\ndone", "code"},
		{"no code block", "just text", ""},
		{"empty block", "```\n```", ""},
		{"opening fence without newline", "```go", ""},
		{"no closing fence", "```\nfoo\nbar", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractCodeBlock(tt.input)
			if got != tt.want {
				t.Errorf("extractCodeBlock() = %q, want %q", got, tt.want)
			}
		})
	}
}
