// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package index

import (
	"context"
	"testing"
)

func TestIndexManifests_PersistsPackageJSON(t *testing.T) {
	t.Parallel()
	root := scratchRepo(t, map[string]string{
		"package.json": `{"name":"x","dependencies":{"react":"18.2.0"},"devDependencies":{"vitest":"1.0.0"}}`,
		// Kept under trivialFileBytes so IndexRepo doesn't reach for the
		// (nil) LLM client — this test only exercises the manifest pass.
		"a.js": "console.log(1);\n",
	})
	store := openTestStore(t)
	// LLM not required for manifest pass — pass nil, guarded upstream.
	idx := NewIndexer(store, nil, IndexerOptions{Model: "test"})
	if _, err := idx.IndexRepo(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	pkgs, err := store.ListManifestPackages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) < 2 {
		t.Fatalf("expected >=2 packages from package.json, got %d: %+v", len(pkgs), pkgs)
	}
	names := map[string]bool{}
	for _, p := range pkgs {
		names[p.Name] = true
	}
	if !names["react"] || !names["vitest"] {
		t.Fatalf("expected react + vitest in manifest rows, got %+v", names)
	}
}

func TestIndexManifests_NoManifestsIsNoOp(t *testing.T) {
	t.Parallel()
	root := scratchRepo(t, map[string]string{
		"a.go": "package a\n",
	})
	store := openTestStore(t)
	idx := NewIndexer(store, nil, IndexerOptions{Model: "test"})
	if _, err := idx.IndexRepo(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	pkgs, err := store.ListManifestPackages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 0 {
		t.Fatalf("expected 0 packages, got %d: %+v", len(pkgs), pkgs)
	}
}
