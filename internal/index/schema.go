// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/store.py under Apache License 2.0.

package index

// sqliteSchema is the SQLite DDL applied on Store open. Every statement is
// idempotent (CREATE TABLE IF NOT EXISTS / CREATE INDEX IF NOT EXISTS) so
// repeated opens on an existing DB are safe. Timestamps are REAL columns
// storing unix epoch seconds — matches Mira's SQLite for cross-tool file
// portability. Dashboard-only tables from Mira (review_events, feedback,
// learned_rules, vulnerabilities) are deliberately excluded — out of scope
// for the reviewer core.
const sqliteSchema = `
CREATE TABLE IF NOT EXISTS files (
    path TEXT PRIMARY KEY,
    language TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    content_hash TEXT NOT NULL DEFAULT '',
    loc INTEGER NOT NULL DEFAULT 0,
    updated_at REAL NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS symbols (
    file_path TEXT NOT NULL,
    name TEXT NOT NULL,
    kind TEXT NOT NULL DEFAULT 'function',
    signature TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (file_path, name),
    FOREIGN KEY (file_path) REFERENCES files(path) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS imports (
    source_path TEXT NOT NULL,
    target_path TEXT NOT NULL,
    PRIMARY KEY (source_path, target_path),
    FOREIGN KEY (source_path) REFERENCES files(path) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS symbol_refs (
    source_path TEXT NOT NULL,
    source_symbol TEXT NOT NULL,
    target_path TEXT NOT NULL,
    target_symbol TEXT NOT NULL,
    PRIMARY KEY (source_path, source_symbol, target_path, target_symbol),
    FOREIGN KEY (source_path) REFERENCES files(path) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS directories (
    path TEXT PRIMARY KEY,
    summary TEXT NOT NULL DEFAULT '',
    file_count INTEGER NOT NULL DEFAULT 0,
    updated_at REAL NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS external_refs (
    file_path TEXT NOT NULL,
    kind TEXT NOT NULL,
    target TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (file_path, kind, target),
    FOREIGN KEY (file_path) REFERENCES files(path) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS package_manifests (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    kind TEXT NOT NULL DEFAULT '',
    version TEXT NOT NULL DEFAULT '',
    file_path TEXT NOT NULL DEFAULT '',
    is_dev INTEGER NOT NULL DEFAULT 0,
    updated_at REAL NOT NULL DEFAULT 0,
    UNIQUE(name, kind, file_path)
);

CREATE INDEX IF NOT EXISTS idx_pkg_manifest_name ON package_manifests(name);
CREATE INDEX IF NOT EXISTS idx_imports_target ON imports(target_path);
CREATE INDEX IF NOT EXISTS idx_symbol_refs_target ON symbol_refs(target_path, target_symbol);
`
