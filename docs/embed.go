// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

// Package docs bundles the static HTML and CSS shipped with `sacr docs`.
//
// The docs directory doubles as a Go package purely so the sibling static
// files can be embedded via //go:embed. Nothing else in the tree imports
// package docs directly for anything except its Assets FS.
package docs

import "embed"

// Assets carries the offline docs site served by the `sacr docs`
// command. It is a small, first-party bundle — no third-party scripts,
// fonts, or images — so the docs viewer can enforce a strict CSP.
//
//go:embed *.html *.css
var Assets embed.FS
