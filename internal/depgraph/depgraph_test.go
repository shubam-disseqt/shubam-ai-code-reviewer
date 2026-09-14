// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package depgraph

import (
	"strings"
	"testing"
)

const testModule = "github.com/example/repo"

func TestRender_EmptyWhenNoGoFiles(t *testing.T) {
	got := Render([]File{{Path: "README.md", Content: []byte("hi")}}, DefaultOptions(testModule))
	if got != "" {
		t.Errorf("non-empty diagram from non-Go input:\n%s", got)
	}
}

func TestRender_EmptyWhenSingleFileWithNoRepoImports(t *testing.T) {
	// The e2e footgun that started this whole redesign — the tester's
	// cmd/api/main.go imports only stdlib. The old LLM diagram
	// hallucinated internal-package edges; the deterministic one must
	// return "" and let the caller omit the section entirely.
	src := `package main

import (
	"html"
	"log"
	"net/http"
	"os"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<h1>" + html.EscapeString(r.URL.Path) + "</h1>"))
	})
	log.Fatal(http.ListenAndServe(":"+os.Getenv("PORT"), mux))
}
`
	got := Render([]File{{Path: "cmd/api/main.go", Content: []byte(src)}}, DefaultOptions(testModule))
	if got != "" {
		t.Errorf("stdlib-only main.go should yield no diagram; got:\n%s", got)
	}
}

func TestRender_RealCrossPackageEdgeShowsUp(t *testing.T) {
	handlerSrc := `package handler

import (
	"github.com/example/repo/internal/service"
	"net/http"
)

func H(w http.ResponseWriter, r *http.Request) { service.Do() }
`
	serviceSrc := `package service

func Do() {}
`
	got := Render([]File{
		{Path: "internal/handler/handler.go", Content: []byte(handlerSrc)},
		{Path: "internal/service/service.go", Content: []byte(serviceSrc)},
	}, DefaultOptions(testModule))
	if got == "" {
		t.Fatal("expected non-empty diagram for a real cross-package import")
	}
	if !strings.Contains(got, "\"internal/handler\"") {
		t.Errorf("missing source node:\n%s", got)
	}
	if !strings.Contains(got, "\"internal/service\"") {
		t.Errorf("missing target node:\n%s", got)
	}
}

func TestRender_ExternalImportsDroppedByDefault(t *testing.T) {
	src := `package foo

import (
	"net/http"
	"github.com/spf13/cobra"
)

var _ = http.Handler(nil)
var _ = cobra.Command{}
`
	got := Render([]File{{Path: "cmd/foo/foo.go", Content: []byte(src)}}, DefaultOptions(testModule))
	if got != "" {
		t.Errorf("external-only imports should yield empty diagram; got:\n%s", got)
	}
}

func TestRender_ExternalIncludedWhenOptedIn(t *testing.T) {
	src := `package foo

import "net/http"

var _ = http.Handler(nil)
`
	opts := DefaultOptions(testModule)
	opts.IncludeExternal = true
	got := Render([]File{{Path: "cmd/foo/foo.go", Content: []byte(src)}}, opts)
	if !strings.Contains(got, "net/http") {
		t.Errorf("expected external node net/http; got:\n%s", got)
	}
}

func TestRender_Deterministic(t *testing.T) {
	// Reproducibility — call twice, expect byte-identical output.
	src := `package handler

import "github.com/example/repo/internal/service"

var _ = service.X
`
	files := []File{{Path: "internal/handler/x.go", Content: []byte(src)}}
	a := Render(files, DefaultOptions(testModule))
	b := Render(files, DefaultOptions(testModule))
	if a != b {
		t.Errorf("Render is not deterministic:\n a=%q\n b=%q", a, b)
	}
}

func TestRender_MalformedSourceSkippedNotFatal(t *testing.T) {
	good := `package handler

import "github.com/example/repo/internal/service"

var _ = service.X
`
	bad := `not go code at all`
	files := []File{
		{Path: "internal/handler/x.go", Content: []byte(good)},
		{Path: "internal/broken/y.go", Content: []byte(bad)},
	}
	got := Render(files, DefaultOptions(testModule))
	if !strings.Contains(got, "internal/handler") {
		t.Errorf("well-formed file should still render despite broken sibling; got:\n%s", got)
	}
}

func TestRender_SelfEdgeSuppressed(t *testing.T) {
	// A package importing itself would be malformed Go — but the
	// heuristic that turns a file path into "cmd/api" could produce a
	// self-loop if not guarded. Verify no A→A edges.
	src := `package foo

import "github.com/example/repo/internal/foo"

var _ = foo.Bar
`
	got := Render([]File{{Path: "internal/foo/x.go", Content: []byte(src)}}, DefaultOptions(testModule))
	if strings.Contains(got, "N0 --> N0") {
		t.Errorf("self-edge should be suppressed:\n%s", got)
	}
}
