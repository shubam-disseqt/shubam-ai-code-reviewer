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

func TestRender_TypeScriptImports(t *testing.T) {
	handlerSrc := `import { helper } from './helper';
import type { Config } from '../config/types';
export { util } from './util';
const lazy = import('./lazy');
`
	got := Render([]File{
		{Path: "src/handler/handler.ts", Content: []byte(handlerSrc)},
		{Path: "src/handler/helper.ts", Content: []byte(`export const helper = 1;`)},
	}, DefaultOptions(testModule))
	if got == "" {
		t.Fatal("expected non-empty diagram for TS relative imports")
	}
	if !strings.Contains(got, "\"src/handler\"") {
		t.Errorf("missing source node src/handler:\n%s", got)
	}
	if !strings.Contains(got, "\"src/config/types\"") {
		t.Errorf("missing target src/config/types (parent-relative):\n%s", got)
	}
	if !strings.Contains(got, "\"src/handler/util\"") {
		t.Errorf("missing target src/handler/util (export-from):\n%s", got)
	}
	if !strings.Contains(got, "\"src/handler/lazy\"") {
		t.Errorf("missing dynamic import src/handler/lazy:\n%s", got)
	}
}

func TestRender_PythonImports(t *testing.T) {
	src := `from .helper import Thing
from ..config import settings
import os
`
	got := Render([]File{
		{Path: "myapp/handler/service.py", Content: []byte(src)},
	}, DefaultOptions(testModule))
	if got == "" {
		t.Fatal("expected non-empty diagram for Python relative imports")
	}
	if !strings.Contains(got, "\"myapp/handler\"") {
		t.Errorf("missing source node myapp/handler:\n%s", got)
	}
	if !strings.Contains(got, "\"myapp/handler/helper\"") {
		t.Errorf("missing sibling-relative target myapp/handler/helper:\n%s", got)
	}
	if !strings.Contains(got, "\"myapp/config\"") {
		t.Errorf("missing parent-relative target myapp/config:\n%s", got)
	}
	if strings.Contains(got, "\"os\"") {
		t.Errorf("stdlib import 'os' should be dropped:\n%s", got)
	}
}

func TestRender_MixedLanguages(t *testing.T) {
	tsSrc := `import { x } from './x';`
	pySrc := `from .helper import Y`
	got := Render([]File{
		{Path: "web/src/app.ts", Content: []byte(tsSrc)},
		{Path: "api/handler.py", Content: []byte(pySrc)},
	}, DefaultOptions(testModule))
	if !strings.Contains(got, "\"web/src\"") || !strings.Contains(got, "\"web/src/x\"") {
		t.Errorf("expected TS edge web/src → web/src/x:\n%s", got)
	}
	if !strings.Contains(got, "\"api\"") || !strings.Contains(got, "\"api/helper\"") {
		t.Errorf("expected Python edge api → api/helper:\n%s", got)
	}
}

func TestRender_ExternalTSDropped(t *testing.T) {
	src := `import React from 'react';
import { useState } from 'react';
`
	opts := DefaultOptions(testModule)
	got := Render([]File{{Path: "web/src/app.tsx", Content: []byte(src)}}, opts)
	if got != "" {
		t.Errorf("bare specifier 'react' should not appear without IncludeExternal:\n%s", got)
	}
	opts.IncludeExternal = true
	got = Render([]File{{Path: "web/src/app.tsx", Content: []byte(src)}}, opts)
	if !strings.Contains(got, "\"react\"") {
		t.Errorf("expected 'react' node when IncludeExternal is set:\n%s", got)
	}
}

func TestRender_ExternalPythonDropped(t *testing.T) {
	src := `import numpy
from pandas import DataFrame
`
	opts := DefaultOptions(testModule)
	got := Render([]File{{Path: "analytics/report.py", Content: []byte(src)}}, opts)
	if got != "" {
		t.Errorf("third-party imports should not appear without IncludeExternal:\n%s", got)
	}
	opts.IncludeExternal = true
	got = Render([]File{{Path: "analytics/report.py", Content: []byte(src)}}, opts)
	if !strings.Contains(got, "\"numpy\"") {
		t.Errorf("expected 'numpy' node when IncludeExternal is set:\n%s", got)
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
