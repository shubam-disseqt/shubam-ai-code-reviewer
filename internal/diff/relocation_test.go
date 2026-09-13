// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors
// Portions Copyright 2026 disseqt — adapted from alibaba/open-code-review
// Adapted from alibaba/open-code-review internal/diff/relocation_test.go

package diff

import (
	"testing"

	"github.com/shubam-disseqt/z-code-reviewer/internal/model"
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
