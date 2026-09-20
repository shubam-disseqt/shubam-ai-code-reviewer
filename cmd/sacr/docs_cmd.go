// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/docs"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/docsserver"
)

func newDocsCmd() *cobra.Command {
	var (
		addr     string
		openMode string
	)
	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Serve the offline docs site from an embedded bundle",
		Long: `Serve the offline docs site bundled with the binary.

The server binds to a loopback address by default, enforces a Host
header allowlist (defeats DNS-rebinding attacks against localhost),
and applies strict CSP + security headers. To reach the docs from a
non-loopback address, set SACR_DOCS_ALLOWED_HOSTS to a
comma-separated list of hostnames you want to accept.

Press Ctrl+C to stop the server.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			opts := docsserver.Options{
				Addr:     addr,
				Assets:   docs.Assets,
				Stdout:   cmd.OutOrStdout(),
				OpenMode: openMode,
			}
			if err := docsserver.Start(ctx, opts); err != nil {
				return fmt.Errorf("docs server: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:0", "listen address (host:port; port 0 picks a free port)")
	cmd.Flags().StringVar(&openMode, "open", "auto", "open browser on start: auto | always | never")
	return cmd
}
