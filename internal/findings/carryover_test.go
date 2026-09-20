// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package findings

import (
	"testing"
	"time"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/model"
)

// mkFinding builds a Finding with just enough shape for reconcile tests.
func mkFinding(fp, path string) Finding {
	return Finding{
		Fingerprint: fp,
		Comment:     model.LlmComment{Path: path, Content: "x", StartLine: 1, EndLine: 1},
	}
}

// findByFP returns the (state, ok) for a given fingerprint in a slice.
func findByFP(fs []Finding, fp string) (State, bool) {
	for _, f := range fs {
		if f.Fingerprint == fp {
			return f.State, true
		}
	}
	return "", false
}

func TestReconcileStateMachine(t *testing.T) {
	tests := []struct {
		name         string
		previous     []Finding
		fresh        []Finding
		changedPaths []string
		want         map[string]State // fp → expected state; missing fp = must not appear
	}{
		{
			name:         "previous matched by fresh → keep",
			previous:     []Finding{mkFinding("a", "foo.go")},
			fresh:        []Finding{mkFinding("a", "foo.go")},
			changedPaths: []string{"foo.go"},
			want:         map[string]State{"a": StateKeep},
		},
		{
			name:         "previous absent from fresh, file untouched → carried",
			previous:     []Finding{mkFinding("a", "foo.go")},
			fresh:        nil,
			changedPaths: []string{"bar.go"},
			want:         map[string]State{"a": StateCarried},
		},
		{
			name:         "previous absent from fresh, file touched → resolved (dropped)",
			previous:     []Finding{mkFinding("a", "foo.go")},
			fresh:        nil,
			changedPaths: []string{"foo.go"},
			want:         map[string]State{}, // "a" must not appear
		},
		{
			name:         "fresh only → new",
			previous:     nil,
			fresh:        []Finding{mkFinding("b", "foo.go")},
			changedPaths: []string{"foo.go"},
			want:         map[string]State{"b": StateNew},
		},
		{
			name: "mix: keep + carried + new + resolved",
			previous: []Finding{
				mkFinding("keep-fp", "keep.go"),
				mkFinding("carried-fp", "untouched.go"),
				mkFinding("resolved-fp", "touched-fixed.go"),
			},
			fresh: []Finding{
				mkFinding("keep-fp", "keep.go"),
				mkFinding("new-fp", "keep.go"),
			},
			changedPaths: []string{"keep.go", "touched-fixed.go"},
			want: map[string]State{
				"keep-fp":    StateKeep,
				"carried-fp": StateCarried,
				"new-fp":     StateNew,
				// "resolved-fp" must be absent
			},
		},
		{
			name:         "empty changedPaths → everything unmatched carries",
			previous:     []Finding{mkFinding("a", "foo.go"), mkFinding("b", "bar.go")},
			fresh:        nil,
			changedPaths: nil,
			want: map[string]State{
				"a": StateCarried,
				"b": StateCarried,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := Reconcile(tt.previous, tt.fresh, tt.changedPaths)
			// Every expected fp must be present with expected state.
			for fp, wantState := range tt.want {
				got, ok := findByFP(out, fp)
				if !ok {
					t.Errorf("fp %q missing from output", fp)
					continue
				}
				if got != wantState {
					t.Errorf("fp %q: got state %q, want %q", fp, got, wantState)
				}
			}
			// Nothing unexpected: every output fp is either in want or a
			// leftover; loudly flag if the set diverges.
			for _, f := range out {
				if _, expected := tt.want[f.Fingerprint]; !expected {
					t.Errorf("unexpected fp in output: %q (state=%s)", f.Fingerprint, f.State)
				}
			}
		})
	}
}

func TestReconcilePreservesFirstSeen(t *testing.T) {
	origin := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	prev := Finding{
		Fingerprint: "a",
		Comment:     model.LlmComment{Path: "foo.go"},
		FirstSeen:   origin,
	}
	fresh := Finding{Fingerprint: "a", Comment: model.LlmComment{Path: "foo.go"}}

	out := Reconcile([]Finding{prev}, []Finding{fresh}, []string{"foo.go"})
	if len(out) != 1 {
		t.Fatalf("want 1 finding, got %d", len(out))
	}
	if !out[0].FirstSeen.Equal(origin) {
		t.Errorf("first_seen not preserved: got %v, want %v", out[0].FirstSeen, origin)
	}
	if !out[0].LastSeen.After(origin) {
		t.Errorf("last_seen should advance; got %v", out[0].LastSeen)
	}
}

func TestReconcileSetsFirstSeenForNew(t *testing.T) {
	fresh := Finding{Fingerprint: "b", Comment: model.LlmComment{Path: "foo.go"}}
	out := Reconcile(nil, []Finding{fresh}, []string{"foo.go"})
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	if out[0].FirstSeen.IsZero() {
		t.Error("first_seen must be populated for new findings")
	}
	if out[0].State != StateNew {
		t.Errorf("want new, got %s", out[0].State)
	}
}

func TestSummarize(t *testing.T) {
	prev := []Finding{
		{Fingerprint: "kept"},
		{Fingerprint: "carried"},
		{Fingerprint: "resolved"},
	}
	reconciled := []Finding{
		{Fingerprint: "kept", State: StateKeep},
		{Fingerprint: "carried", State: StateCarried},
		{Fingerprint: "new", State: StateNew},
	}
	got := Summarize(prev, reconciled)
	if got.Kept != 1 || got.Carried != 1 || got.New != 1 || got.Resolved != 1 {
		t.Errorf("summary wrong: %+v", got)
	}
}
