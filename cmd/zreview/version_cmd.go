// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print zreview version, commit, build date, and Go toolchain",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "zreview %s\n", Version)
			fmt.Fprintf(out, "  commit: %s\n", GitCommit)
			fmt.Fprintf(out, "  built:  %s\n", BuildDate)
			fmt.Fprintf(out, "  go:     %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
			return nil
		},
	}
}
