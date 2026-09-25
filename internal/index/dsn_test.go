// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package index

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultDSNStablePerRepo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // windows

	a, err := DefaultDSN("/tmp/repo-a")
	if err != nil {
		t.Fatalf("DefaultDSN: %v", err)
	}
	again, _ := DefaultDSN("/tmp/repo-a")
	b, _ := DefaultDSN("/tmp/repo-b")

	if a != again {
		t.Errorf("same repo must map to same DSN: %q vs %q", a, again)
	}
	if a == b {
		t.Errorf("different repos must map to different DSNs: %q", a)
	}
	if !strings.HasPrefix(a, "sqlite:///") {
		t.Errorf("want sqlite scheme, got %q", a)
	}
	wantDir := filepath.ToSlash(filepath.Join(home, ".sacr", "index"))
	if !strings.Contains(a, wantDir) {
		t.Errorf("want DSN under %q, got %q", wantDir, a)
	}
}

func TestResolveDSNEnvWins(t *testing.T) {
	t.Setenv("SACR_DB_URL", "sqlite:///:memory:")
	got, err := ResolveDSN(".")
	if err != nil {
		t.Fatalf("ResolveDSN: %v", err)
	}
	if got != "sqlite:///:memory:" {
		t.Errorf("env must win, got %q", got)
	}
}

func TestResolveDSNFallsBackToDefault(t *testing.T) {
	t.Setenv("SACR_DB_URL", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	got, err := ResolveDSN(".")
	if err != nil {
		t.Fatalf("ResolveDSN: %v", err)
	}
	want, _ := DefaultDSN(".")
	if got != want {
		t.Errorf("got %q, want default %q", got, want)
	}
}

// The default location's parent dir does not exist on a fresh machine;
// NewStore must create it rather than fail on open.
func TestNewStoreCreatesParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deeper", "index.db")
	s, err := NewStore(context.Background(), "sqlite:///"+path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()
	if err := s.Ping(context.Background()); err != nil {
		t.Errorf("ping: %v", err)
	}
}
