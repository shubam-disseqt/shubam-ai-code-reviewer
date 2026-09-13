// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCmd_PrintsVersionInfo(t *testing.T) {
	t.Parallel()
	cmd := newRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"version"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("version cmd failed: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"zreview", "commit:", "built:", "go:"} {
		if !strings.Contains(out, want) {
			t.Errorf("version output missing %q; got: %s", want, out)
		}
	}
}

func TestRootCmd_HasExpectedSubcommands(t *testing.T) {
	t.Parallel()
	cmd := newRootCmd()
	got := map[string]bool{}
	for _, sub := range cmd.Commands() {
		got[sub.Name()] = true
	}
	for _, want := range []string{"version", "docs"} {
		if !got[want] {
			t.Errorf("root command missing subcommand %q", want)
		}
	}
}
