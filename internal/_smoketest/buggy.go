//go:build ignore

// Package smoketest seeds obvious bugs so a sacr dogfood review has
// something concrete to catch on this PR. Directory name starts with `_`
// and the file uses `//go:build ignore` so nothing here reaches the real
// build or test targets.
package smoketest

import (
	"crypto/md5"
	"database/sql"
	"fmt"
	"io"
	"net/http"
)

// Note: no hardcoded token seeded here — the pre-commit gitleaks hook
// already proved itself by blocking that variant. The remaining bugs
// below exercise the semgrep + LLM paths.

func lookupUser(db *sql.DB, name string) *sql.Row {
	return db.QueryRow(fmt.Sprintf("SELECT id FROM users WHERE name = '%s'", name)) // seeded: SQL injection
}

func fetch(url string) []byte {
	resp, _ := http.Get(url) // seeded: swallowed error + missing resp.Body.Close()
	buf, _ := io.ReadAll(resp.Body)
	return buf
}

func hashPassword(pw string) []byte { return md5.New().Sum([]byte(pw)) } // seeded: weak hash for passwords

func divide(a, b int) int { return a / b } // seeded: no zero-check
