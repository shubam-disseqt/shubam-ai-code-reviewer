// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package reviewctx

import (
	"context"
	"strings"
	"testing"
)

func TestBuildJIT_PythonCrossFile(t *testing.T) {
	opts := Options{
		ChangedPaths: []string{"app/main.py"},
		NewFileContent: map[string]string{
			"app/main.py":   "from lib.helper import greet\n\ndef go():\n    return greet('hi')\n",
			"lib/helper.py": "def greet(name):\n    return f'hello {name}'\n\ndef unused():\n    pass\n",
		},
		RepoTree:     []string{"app/main.py", "lib/helper.py"},
		EnableJavaGo: true,
	}
	out, err := BuildJIT(context.Background(), opts)
	if err != nil {
		t.Fatalf("BuildJIT err: %v", err)
	}
	if !strings.Contains(out, "lib/helper.py") {
		t.Errorf("expected lib/helper.py in output:\n%s", out)
	}
	if !strings.Contains(out, "def greet") {
		t.Errorf("expected greet symbol source:\n%s", out)
	}
	if !strings.Contains(out, "```python") {
		t.Errorf("expected language-tagged fence:\n%s", out)
	}
}

func TestBuildJIT_UnknownLanguageIsEmpty(t *testing.T) {
	opts := Options{
		ChangedPaths:   []string{"README.md"},
		NewFileContent: map[string]string{"README.md": "# hi\n"},
	}
	out, err := BuildJIT(context.Background(), opts)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out != "" {
		t.Errorf("expected empty output, got %q", out)
	}
}

func TestBuildJIT_CharBudgetCap(t *testing.T) {
	big := strings.Repeat("x = 1\n", 400) // ~2400 chars — well over any per-file budget
	opts := Options{
		ChangedPaths: []string{"a.py"},
		NewFileContent: map[string]string{
			"a.py":     "from b import foo\n",
			"b.py":     big,
			"__stub__": "",
		},
		RepoTree:        []string{"a.py", "b.py"},
		MaxPerFileChars: 200,
		CharBudget:      1000,
		EnableJavaGo:    true,
	}
	out, err := BuildJIT(context.Background(), opts)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(out) > opts.CharBudget+256 {
		t.Errorf("output %d chars exceeded budget %d by more than one block", len(out), opts.CharBudget)
	}
	if !strings.Contains(out, "truncated") {
		t.Errorf("expected truncation marker when file exceeds MaxPerFileChars:\n%s", out)
	}
}

func TestBuildJIT_MaxFilesCap(t *testing.T) {
	opts := Options{
		ChangedPaths: []string{"root.py"},
		NewFileContent: map[string]string{
			"root.py": "from a import x\nfrom b import x\nfrom c import x\n",
			"a.py":    "def x(): pass\n",
			"b.py":    "def x(): pass\n",
			"c.py":    "def x(): pass\n",
		},
		RepoTree:     []string{"root.py", "a.py", "b.py", "c.py"},
		MaxFiles:     2,
		EnableJavaGo: true,
	}
	out, err := BuildJIT(context.Background(), opts)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	blocks := strings.Count(out, "### `")
	if blocks != 2 {
		t.Errorf("expected 2 file blocks under MaxFiles=2, got %d\n%s", blocks, out)
	}
}

func TestBuildJIT_JavaGoDisabledDropsResolution(t *testing.T) {
	opts := Options{
		ChangedPaths: []string{"App.java"},
		NewFileContent: map[string]string{
			"App.java":             "import com.foo.Bar;\n",
			"src/com/foo/Bar.java": "class Bar {}\n",
		},
		RepoTree:     []string{"App.java", "src/com/foo/Bar.java"},
		EnableJavaGo: false,
	}
	out, err := BuildJIT(context.Background(), opts)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out != "" {
		t.Errorf("with EnableJavaGo=false expected empty output, got:\n%s", out)
	}
}

func TestBuildJIT_SkipsCandidatesAlreadyInChanged(t *testing.T) {
	opts := Options{
		ChangedPaths: []string{"a.py", "b.py"},
		NewFileContent: map[string]string{
			"a.py": "from b import foo\n",
			"b.py": "def foo(): pass\n",
		},
		RepoTree:     []string{"a.py", "b.py"},
		EnableJavaGo: true,
	}
	out, err := BuildJIT(context.Background(), opts)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out != "" {
		// b.py is already changed — we don't re-inject it as related context.
		t.Errorf("expected empty output when candidate is already changed, got:\n%s", out)
	}
}

func TestBuildJIT_RepoTreeFilterBlocksMissingCandidates(t *testing.T) {
	opts := Options{
		ChangedPaths: []string{"a.py"},
		NewFileContent: map[string]string{
			"a.py":     "from ghost import x\n",
			"ghost.py": "def x(): pass\n",
		},
		// ghost.py is NOT in repo tree -> must not be injected.
		RepoTree:     []string{"a.py"},
		EnableJavaGo: true,
	}
	out, err := BuildJIT(context.Background(), opts)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out != "" {
		t.Errorf("repo-tree filter didn't block; got:\n%s", out)
	}
}

func TestBuildJIT_GoModulePickedUp(t *testing.T) {
	opts := Options{
		ChangedPaths: []string{"main.go"},
		NewFileContent: map[string]string{
			"go.mod": "module example.com/m\n\ngo 1.21\n",
			"main.go": `package main
import "example.com/m/pkg/util"
func main() { util.Do() }
`,
			"pkg/util/util.go": "package util\nfunc Do() {}\n",
		},
		RepoTree:     []string{"go.mod", "main.go", "pkg/util/util.go"},
		EnableJavaGo: true,
	}
	out, err := BuildJIT(context.Background(), opts)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "pkg/util/util.go") {
		t.Errorf("expected go.mod-resolved candidate:\n%s", out)
	}
}

func TestTrimExcerpt(t *testing.T) {
	if got := trimExcerpt("", 100); got != "" {
		t.Errorf("empty input: %q", got)
	}
	if got := trimExcerpt("short", 100); got != "short" {
		t.Errorf("no-trim: %q", got)
	}
	long := strings.Repeat("a\n", 50) // 100 chars, 50 lines
	got := trimExcerpt(long, 30)
	if !strings.HasSuffix(got, "(truncated)") {
		t.Errorf("expected truncation marker: %q", got)
	}
	if len(got) > 30+len("\n// ... (truncated)") {
		t.Errorf("truncation overshoot: %d chars", len(got))
	}
	if got := trimExcerpt("stuff", 0); got != "" {
		t.Errorf("zero budget: %q", got)
	}
}

func TestBuildJIT_EmptyInputs(t *testing.T) {
	if out, err := BuildJIT(context.Background(), Options{}); err != nil || out != "" {
		t.Errorf("empty opts: out=%q err=%v", out, err)
	}
	// Zero char budget explicitly -> empty output. We must set CharBudget
	// to -1 because 0 triggers the default.
	out, err := BuildJIT(context.Background(), Options{
		ChangedPaths:   []string{"a.py"},
		NewFileContent: map[string]string{"a.py": "x"},
		CharBudget:     -1,
	})
	if err != nil || out != "" {
		t.Errorf("negative budget: out=%q err=%v", out, err)
	}
}
