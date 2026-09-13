// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

// Package docsserver runs a small, defense-in-depth HTTP server for the
// offline docs shipped with the zreview binary.
//
// It is deliberately narrow: read-only, embedded assets, loopback bind,
// Host-header allowlist, strict CSP. There are no write routes and no
// state beyond the embedded FS.
package docsserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Options configures Start.
type Options struct {
	// Addr is the listen address, host:port. host==127.0.0.1 is
	// recommended; port 0 asks the OS for a free port.
	Addr string
	// Assets is the embedded docs FS. Callers pass in docs.Assets from
	// the sibling package so this server has no compile-time coupling
	// to a specific FS layout.
	Assets fs.FS
	// Stdout is where the server prints its listen URL.
	Stdout io.Writer
	// OpenMode is "auto" (default), "always", or "never".
	OpenMode string
}

// Start binds a listener, wires the handler chain, and blocks until ctx
// is cancelled. The listener is bound synchronously so the printed URL
// is always live before we return.
func Start(ctx context.Context, opts Options) error {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Assets == nil {
		return errors.New("docsserver: Assets is required")
	}
	if opts.Addr == "" {
		opts.Addr = "127.0.0.1:0"
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(opts.Assets)))

	allowed := resolveAllowedHostsFromEnv(opts.Addr)
	handler := securityHeaders(hostGuard(allowed, mux))

	ln, err := net.Listen("tcp", opts.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", opts.Addr, err)
	}
	realAddr := ln.Addr().String()
	url := "http://" + displayAddr(realAddr) + "/"

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	fmt.Fprintf(opts.Stdout, "z-code-reviewer docs listening at %s\n", url)
	fmt.Fprintln(opts.Stdout, "(press Ctrl+C to stop)")

	if shouldOpen(opts.OpenMode, opts.Stdout) {
		openBrowser(url)
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve: %w", err)
	}
}

// shouldOpen decides whether to launch the browser. auto skips when
// stdout is not a TTY (piped output, CI, etc.). always forces open.
// never suppresses.
func shouldOpen(mode string, out io.Writer) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "always":
		return true
	case "never":
		return false
	default:
		f, ok := out.(*os.File)
		if !ok {
			return false
		}
		info, err := f.Stat()
		if err != nil {
			return false
		}
		return (info.Mode() & os.ModeCharDevice) != 0
	}
}

// openBrowser attempts to open url in the user's default browser. Errors
// are non-fatal — the URL is already printed on stdout.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
