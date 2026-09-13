// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package overlap

import "testing"

func TestParseVerdict_Happy(t *testing.T) {
	raw := `{"overlaps":[
	  {"pr_number":10,"kind":"merge_conflict","reason":"same file","confidence":0.9},
	  {"pr_number":11,"kind":"both","reason":"same file, same fix","confidence":0.75}
	]}`
	got := parseVerdict(raw)
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2", len(got))
	}
	if got[10].Kind != "merge_conflict" || got[10].Conf != 0.9 {
		t.Errorf("got[10] = %+v", got[10])
	}
	if got[11].Kind != "both" {
		t.Errorf("got[11].Kind = %q", got[11].Kind)
	}
}

func TestParseVerdict_ClampConfidence(t *testing.T) {
	raw := `{"overlaps":[{"pr_number":1,"kind":"none","reason":"","confidence":9.9},{"pr_number":2,"kind":"none","reason":"","confidence":-3}]}`
	got := parseVerdict(raw)
	if got[1].Conf != 1.0 {
		t.Errorf("got[1].Conf = %v, want 1.0", got[1].Conf)
	}
	if got[2].Conf != 0.0 {
		t.Errorf("got[2].Conf = %v, want 0.0", got[2].Conf)
	}
}

func TestParseVerdict_UnknownKind(t *testing.T) {
	raw := `{"overlaps":[{"pr_number":5,"kind":"cosmic_ray","reason":"","confidence":0.5}]}`
	got := parseVerdict(raw)
	if got[5].Kind != "none" {
		t.Errorf("kind = %q, want none", got[5].Kind)
	}
}

func TestParseVerdict_MalformedTolerated(t *testing.T) {
	// Entry 2 is malformed (pr_number is a string that isn't a number);
	// entries 1 and 3 must still land.
	raw := `{"overlaps":[
	  {"pr_number":1,"kind":"merge_conflict","reason":"a","confidence":0.5},
	  {"pr_number":"not-a-number","kind":"none","reason":"","confidence":0.1},
	  {"pr_number":3,"kind":"duplicate_effort","reason":"b","confidence":0.8}
	]}`
	got := parseVerdict(raw)
	if _, ok := got[1]; !ok {
		t.Errorf("entry 1 missing")
	}
	if _, ok := got[3]; !ok {
		t.Errorf("entry 3 missing")
	}
	if _, ok := got[0]; ok {
		t.Errorf("malformed entry should be dropped, got %+v", got[0])
	}
}

func TestParseVerdict_NotJSON(t *testing.T) {
	if got := parseVerdict("not json at all"); len(got) != 0 {
		t.Errorf("want empty, got %v", got)
	}
	if got := parseVerdict(""); len(got) != 0 {
		t.Errorf("want empty for empty input")
	}
}

func TestParseVerdict_ThinkBlockAndFences(t *testing.T) {
	raw := "<think>reasoning goes here\nmulti-line</think>\n```json\n" +
		`{"overlaps":[{"pr_number":42,"kind":"both","reason":"z","confidence":0.7}]}` +
		"\n```"
	got := parseVerdict(raw)
	if got[42].Kind != "both" {
		t.Errorf("got=%+v", got)
	}
}

func TestParseVerdict_EmptyOverlaps(t *testing.T) {
	got := parseVerdict(`{"overlaps":[]}`)
	if len(got) != 0 {
		t.Errorf("want empty, got %v", got)
	}
}

func TestParseVerdict_NonObjectRoot(t *testing.T) {
	if got := parseVerdict(`["not","an","object"]`); len(got) != 0 {
		t.Errorf("want empty, got %v", got)
	}
}

func TestStripCodeFences(t *testing.T) {
	cases := map[string]string{
		"plain":            "plain",
		"```json\nhi\n```": "hi",
		"```\nhi\n```":     "hi",
		"  ```\nhi\n```  ": "hi",
		"```":              "",
	}
	for in, want := range cases {
		if got := stripCodeFences(in); got != want {
			t.Errorf("stripCodeFences(%q) = %q, want %q", in, got, want)
		}
	}
}
