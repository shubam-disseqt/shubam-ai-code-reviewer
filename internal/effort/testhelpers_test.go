// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package effort

import "os"

func writeAll(p string, b []byte) error {
	return os.WriteFile(p, b, 0o644)
}
