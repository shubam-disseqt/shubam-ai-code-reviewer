// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt Contributors
// Portions Copyright 2026 alibaba/open-code-review Contributors
// Adapted helper extracted from alibaba/open-code-review internal/llm/responses_client_test.go.

package llm

import (
	"encoding/json"
	"testing"

	"github.com/openai/openai-go/v3/responses"
)

// unmarshalResponsesBody parses a raw JSON body into a responses.Response for
// tests that assert on mapResponsesResponse output.
func unmarshalResponsesBody(t *testing.T, body string) *responses.Response {
	t.Helper()
	var r responses.Response
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatalf("unmarshal responses body: %v", err)
	}
	return &r
}
