// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package reviewctx

import "testing"

func TestCountTokens(t *testing.T) {
	if got := CountTokens(""); got != 0 {
		t.Errorf("empty: %d", got)
	}
	// Mira's ratio is 4 chars per token.
	if got := CountTokens("abcdefgh"); got != 2 {
		t.Errorf("8 chars: %d", got)
	}
	if got := CountTokens("abcde"); got != 1 {
		t.Errorf("5 chars floor to 1 token: %d", got)
	}
}

func TestTokensToChars(t *testing.T) {
	if got := tokensToChars(0); got != 0 {
		t.Errorf("zero: %d", got)
	}
	if got := tokensToChars(-1); got != 0 {
		t.Errorf("negative: %d", got)
	}
	if got := tokensToChars(100); got != 400 {
		t.Errorf("100 tokens: %d", got)
	}
}

func TestOptionsResolvedFillsDefaults(t *testing.T) {
	o := Options{}.resolved()
	if o.MaxFiles != DefaultMaxFiles {
		t.Errorf("MaxFiles: %d", o.MaxFiles)
	}
	if o.MaxPerFileChars != DefaultMaxPerFileChars {
		t.Errorf("MaxPerFileChars: %d", o.MaxPerFileChars)
	}
	if o.CharBudget != DefaultCharBudget {
		t.Errorf("CharBudget: %d", o.CharBudget)
	}
	if o.TokenBudget != DefaultTokenBudget {
		t.Errorf("TokenBudget: %d", o.TokenBudget)
	}
}
