// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package rules

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestRenderForPrompt_Empty(t *testing.T) {
	if got := RenderForPrompt(nil); got != "" {
		t.Errorf("want empty, got %q", got)
	}
	if got := RenderForPrompt([]Rule{}); got != "" {
		t.Errorf("want empty on []Rule{}, got %q", got)
	}
}

func TestRenderForPrompt_Single(t *testing.T) {
	r := Rule{Title: "No console.log", Body: "Flag console.log in prod code."}
	got := RenderForPrompt([]Rule{r})
	want := "## Custom Review Rules\n\n" +
		"Follow these rules when reviewing code. They take priority over your default behavior:\n\n" +
		"### No console.log\n" +
		"Flag console.log in prod code.\n\n"
	if got != want {
		t.Fatalf("mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestRenderForPrompt_MultiTrimsBodyNewline(t *testing.T) {
	got := RenderForPrompt([]Rule{
		{Title: "A", Body: "body A\n"},
		{Title: "B", Body: "body B"},
	})
	if !strings.Contains(got, "### A\nbody A\n\n### B\nbody B\n") {
		t.Fatalf("format wrong:\n%s", got)
	}
}

func TestRenderForPrompt_CapsAt15(t *testing.T) {
	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(orig)

	rules := make([]Rule, 20)
	for i := range rules {
		rules[i] = Rule{Title: "T", Body: "B"}
	}
	got := RenderForPrompt(rules)
	// 15 rendered rules → 15 occurrences of "### T"
	if n := strings.Count(got, "### T\n"); n != 15 {
		t.Fatalf("want 15 rendered rules, got %d", n)
	}
	if !strings.Contains(buf.String(), "capping injected rules at 15 (dropped 5)") {
		t.Fatalf("expected log about cap, got: %q", buf.String())
	}
}
