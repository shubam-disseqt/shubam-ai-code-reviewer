# Provider Testing

Manual test protocols for each LLM provider sacr supports. Nobody has
actually run every provider against a real account yet; these protocols let
someone with credentials smoke-test in ~15 minutes.

Sections are markered so parallel agents can extend this file without
stepping on each other. New providers: add a new `<!-- FOO START -->` /
`<!-- FOO END -->` block; do not touch other sections.

<!-- DEEPSEEK START -->

## DeepSeek

DeepSeek routes through the OpenAI Chat Completions protocol
(`internal/llm/providers.go` `Protocol: ProtocolOpenAIChatCompletions`).
There is no DeepSeek-specific adapter — the same `OpenAIClient` handles
`https://api.deepseek.com` requests by pointing `BaseURL` at the DeepSeek
host and letting the OpenAI Go SDK append `/chat/completions`.

### Code-path audit findings

- **Endpoint**: `https://api.deepseek.com` (registry entry).
  `NewOpenAIClient` in `internal/llm/client.go` normalizes this to
  `https://api.deepseek.com/chat/completions`. DeepSeek's platform accepts
  both `/chat/completions` and `/v1/chat/completions`, so the current base
  URL is fine even without `/v1`.
- **Auth**: Bearer token via `Authorization: Bearer $DEEPSEEK_API_KEY` —
  standard OpenAI SDK `WithAPIKey`.
- **Models registered**: `deepseek-v4-pro`, `deepseek-v4-flash`,
  `deepseek-flash`. These are aspirational / branding-forward names and do
  **not** match DeepSeek's public catalog as of writing, which exposes
  `deepseek-chat` and `deepseek-reasoner`. **Bug risk (LOW-MEDIUM)**: an
  operator running `sacr` with no `SACR_MODEL` will hit
  `deepseek-v4-pro` (first entry), and DeepSeek will 400 with
  `model_not_found`. Workaround: always set `SACR_MODEL=deepseek-chat`
  until the registry is refreshed. Fix path: update `Models` slice in
  `providers.go` to match the live catalog. See "Common errors" below.
- **Token counting**: no DeepSeek-specific tokenizer. `CountTokensForModel`
  falls through to `cl100k_base` via tiktoken (`encodingForModel` in
  `client.go`). DeepSeek's real tokenizer differs, especially for Chinese
  text — expect ~10-20% drift on multilingual content, negligible on ASCII.
  Context-budget accounting is therefore approximate.
- **Rate limits & error shapes**: no DeepSeek-specific handling. Retries
  use the SDK default (5) plus any `retry_codes` configured in the resolver.
  DeepSeek uses OpenAI-shaped error JSON (`{error: {message, type, code}}`),
  which the SDK already understands.
- **Output cap**: `buildOpenAIParams` clamps `max_completion_tokens` at
  16384. DeepSeek's max output is 8192 (chat) / 32768 (reasoner); the clamp
  is safe for chat, mildly conservative for reasoner.
- **Cheap tier**: `internal/llm/tiers.go` accepts `deepseek` as a valid
  `SACR_CHEAP_PROVIDER`. Independent tier resolution works — see
  `tiers_test.go` `TestResolveTiers_IndependentCheap`.

### 15-minute manual test protocol

**Prerequisites**

- DeepSeek account with a top-up balance (free credit works for smoke).
- API key from https://platform.deepseek.com/api_keys — starts with `sk-`.
- `sacr` binary built (`make build` at the repo root).
- A trivial git repo with at least one commit to review, e.g. `/tmp/simple-go-repo`.

**Env setup — main tier**

```bash
export DEEPSEEK_API_KEY=sk-...
# The registry names are wrong (see audit above). Force a live model:
export SACR_MODEL=deepseek-chat
# Optional; provider auto-selects when unset because DEEPSEEK_API_KEY is set:
export SACR_PROVIDER=deepseek
```

**Env setup — cheap tier only** (recommended: DeepSeek is the cheapest
option for the summarizer / labeler; keep Claude/GPT for the reviewer)

```bash
export ANTHROPIC_API_KEY=sk-ant-...          # main tier stays Claude
export DEEPSEEK_API_KEY=sk-...
export SACR_CHEAP_PROVIDER=deepseek
export SACR_CHEAP_MODEL=deepseek-chat
```

**Doctor check** (no API cost)

```bash
sacr doctor
```

Expected: a row `deepseek key    ok    key present (base URL https://api.deepseek.com)`.
If the key is malformed, the row reports `fail` with a suggestion pointing
at https://platform.deepseek.com/api_keys. The doctor does NOT call the
DeepSeek API — no surprise billing.

**Smoke review**

```bash
cd /tmp/simple-go-repo
sacr review --repo . --commit HEAD --format stdout
```

Expected: a normal review report; no `model_not_found` or `401`.

**Expected cost**

DeepSeek `deepseek-chat` is ~\$0.27/M input and ~\$1.10/M output (cache miss).
A small commit review is typically <5k input + <1k output tokens, so
**under \$0.002** per run. Cheap-tier-only usage (summary + labels) is
another order of magnitude lower.

### Common errors

| Symptom | Likely cause | Fix |
|---|---|---|
| `401 Unauthorized` | bad key, revoked key, or key from a different DeepSeek project | rotate at https://platform.deepseek.com/api_keys |
| `400 model_not_found` on `deepseek-v4-pro` / `deepseek-v4-flash` | registry defaults do not match live catalog | set `SACR_MODEL=deepseek-chat` explicitly |
| `429 Too Many Requests` | DeepSeek's per-account concurrency cap | retry with backoff; SDK retries 5 times automatically |
| `context_length_exceeded` | 64k context ceiling on `deepseek-chat` | raise `--min-severity` or narrow `--paths` to shrink review scope |
| Empty response body / `stream ended before choice finished` | rare gateway hiccup | rerun; the client retries `io.ErrUnexpectedEOF` once |

### Follow-ups

- Refresh `internal/llm/providers.go` `Models` for DeepSeek to reflect the
  live catalog (`deepseek-chat`, `deepseek-reasoner`). Until then, callers
  must always set `SACR_MODEL`.
- If token accounting for Chinese-heavy reviews starts to matter, wire a
  DeepSeek tokenizer or bias the `cl100k_base` estimate.

<!-- DEEPSEEK END -->

<!-- BEDROCK START -->

## AWS Bedrock (Anthropic models)

Bedrock reuses the Anthropic Messages API wholesale. The wire format is
identical to `api.anthropic.com`; the SDK's `bedrock.WithConfig` middleware
handles what differs — SigV4 signing, moving the model from the JSON body
into the URL path, and deriving the host from the resolved region. See
`NewAnthropicBedrockClient` in `internal/llm/client.go`.

### Code-path audit findings

- **SDKs**: `github.com/aws/aws-sdk-go-v2/config` for credentials and
  region, `github.com/anthropics/anthropic-sdk-go/bedrock` for the signing
  middleware. No hand-rolled SigV4.
- **Auth chain** (order of precedence at the SDK layer, then a deliberate
  override): `AWS_BEARER_TOKEN_BEDROCK` wins if set; otherwise the standard
  AWS default chain resolves — static keys → shared profile → SSO cache →
  container role → EC2 instance role → `credential_process`. The Bedrock
  constructor **clears** `awsCfg.BearerAuthTokenProvider` before installing
  `bedrock.WithConfig`, so an SSO-authenticated caller does not silently
  ship their OIDC access token as an Authorization header (which Bedrock
  rejects with `Invalid API Key format`). Good defensive fix; keep it.
- **Region**: hard fail if unresolved. `AWS_REGION` env var, or the region
  attached to the active profile. No default. Bedrock derives the runtime
  host (`bedrock-runtime.<region>.amazonaws.com`) from this.
- **Model IDs registered** (`providers.go`): `us.anthropic.claude-opus-5`,
  `us.anthropic.claude-sonnet-5`, `us.anthropic.claude-opus-4-8`,
  `us.anthropic.claude-opus-4-7`, `us.anthropic.claude-sonnet-4-6`,
  plus the `global.` cross-region-inference variants. The list is **not**
  an allowlist for `--model` (`gateOverrideOnModelList := !ambientAuth`) —
  any foundation model ID, inference-profile ID, or application-profile ARN
  Bedrock will route to is accepted. Run
  `aws bedrock list-inference-profiles --region <r>` to see what an account
  actually has.
- **`SACR_PROVIDER=bedrock` alone is NOT enough.** The resolver's
  explicit-provider path (`ResolveEndpointWithOptions`) delegates to
  `tryOCRConfig`, which requires `~/.opencodereview/config.json` to exist
  and to define a `bedrock` entry (preset or custom). `tryProviderEnv`
  skips Bedrock because its `EnvVar` is empty — there is no API key. See
  the "Config file" step below.
- **Request/response shape**: unchanged from Anthropic Messages — the
  middleware handles `model` path translation and `anthropic_version`
  injection. `option.WithHeaderDel("Authorization")` and
  `option.WithHeaderDel("X-Api-Key")` scrub headers before signing so a
  stale env or a Claude Code cross-pollinated setup can't leak a key into
  a signed request.
- **Error mapping** (`explainError`): translates the misleading Bedrock
  wordings — `Invalid API Key format` → bearer-token diagnosis; `don't
  have access to the model` → console model-access; `model identifier is
  invalid` / `inference profile ... not found` → `aws bedrock
  list-inference-profiles`; `ExpiredToken` / `SSOProviderInvalidToken` →
  `aws sso login`; generic `AccessDenied` → IAM policy. `ThrottlingException`
  is **not** explicitly matched; it falls through to the passthrough with
  `bedrockWhere(region, profile)` context, and the SDK's built-in retry
  (5 attempts, exponential) handles the transient case.
- **Timeout**: `bedrockConfigLoadTimeout = 60s` bounds `LoadDefaultConfig`
  so a broken profile cannot stall startup. Credential retrieval itself
  (SSO refresh, `credential_process`, IMDS lookup) is lazy and bounded by
  the per-request `cfg.Timeout` (default 5 min).

### 15-minute manual test protocol

**Prerequisites**

- AWS account with Bedrock enabled in `us-east-1` or `us-west-2`.
- **Model access requested and approved** in the Bedrock console
  (Model access → Manage model access → Anthropic Claude family). This is
  a per-account, per-region toggle; an IAM policy alone does not grant it.
- IAM identity (user or role) with `bedrock:InvokeModel` on the target
  model ARN in the target region. A permissive policy is fine for smoke:
  ```json
  {"Effect":"Allow","Action":"bedrock:InvokeModel","Resource":"*"}
  ```
- `~/.aws/config` with a profile, or `AWS_ACCESS_KEY_ID`+`AWS_SECRET_ACCESS_KEY`
  exported, or an active SSO session (`aws sso login --profile ...`).
- `sacr` binary built (`make build`).
- A trivial git repo for the smoke review, e.g. `/tmp/simple-go-repo`.

**Env setup**

```bash
export AWS_REGION=us-east-1
export AWS_PROFILE=your-profile
export SACR_PROVIDER=bedrock
export SACR_MODEL=us.anthropic.claude-sonnet-4-6
```

`SACR_MODEL` can be any model ID / inference-profile ID / application
inference-profile ARN Bedrock will route to. The registry list is a hint,
not an allowlist.

**Config file** (required — see audit note above)

Create `~/.opencodereview/config.json`:

```json
{
  "provider": "bedrock",
  "providers": {
    "bedrock": {
      "aws_region": "us-east-1"
    }
  }
}
```

`aws_region` in config is optional if `AWS_REGION` is exported; keeping it
here makes the run reproducible without exporting first. `aws_profile` may
also live under the `bedrock` entry.

**Doctor check first** (no API cost, no Retrieve, no SSO prompt)

```bash
sacr doctor
```

Expected rows:

```
bedrock                  ok     region=us-east-1, auth=profile (AWS_PROFILE=your-profile)
llm provider             ok     us.anthropic.claude-sonnet-4-6 (OCR config file)
```

The `bedrock` check inspects `awsconfig.LoadDefaultConfig` output only —
it never calls Bedrock, so it costs nothing. If AWS_REGION is unset and
the profile also has no region, it fails with a suggestion to export one.

**Smoke review**

```bash
cd /tmp/simple-go-repo
sacr review --repo . --commit HEAD --format stdout
```

Expected: a review report; `--format stdout` shows findings + a token/cost
line. First run pays the SigV4 handshake cost (~1-2s); subsequent runs
reuse credentials from the SDK's in-memory cache.

**Expected cost**

Claude Sonnet 4.6 on Bedrock is priced per Anthropic's standard rate
(~\$3/M input, \$15/M output at time of writing). A small commit review
is typically <5k input + <2k output, so **under \$0.05** per run.

### Common errors

| Symptom | Likely cause | Fix |
|---|---|---|
| `AccessDeniedException: You don't have access to the model` | Bedrock model access not enabled for this account/region | request access in the Bedrock console — per-account, per-region toggle |
| `AccessDeniedException` (generic) / `not authorized to invoke this API operation` | IAM identity missing `bedrock:InvokeModel` | attach a policy granting `bedrock:InvokeModel` on the model ARN |
| `ValidationException: model identifier is invalid` | wrong model ID / stale version suffix / model exists in another region | `aws bedrock list-inference-profiles --region <r>` and use an ID from the output |
| `Invalid API Key format` | `AWS_BEARER_TOKEN_BEDROCK` is set to a bad token, or SSO OIDC token leaked (should not happen — the code clears it) | unset `AWS_BEARER_TOKEN_BEDROCK` to fall back to SigV4 |
| `ExpiredToken` / `SSOProviderInvalidToken` | SSO session expired | `aws sso login --profile <p>` |
| `ThrottlingException` | Bedrock per-account concurrency / TPM limit | SDK auto-retries 5x with backoff; lower `--concurrency` if persistent |
| `no AWS region resolved` (from sacr, not AWS) | neither `AWS_REGION` nor profile-region is set | `export AWS_REGION=us-east-1` |

### Follow-ups

- Explicit `ThrottlingException` case in `explainError` would give a
  clearer "lower your concurrency" hint than the passthrough currently
  does; low priority since retries already absorb the transient case.
- The config-file requirement for `SACR_PROVIDER=bedrock` is surprising
  — every other provider works from env vars alone. A short-circuit in
  `tryProviderEnv` for Bedrock (build a synthetic entry when
  `AWS_REGION`+`AWS_PROFILE` are set) would remove the extra file.

<!-- BEDROCK END -->
