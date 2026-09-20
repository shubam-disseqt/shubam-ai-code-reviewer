# Security Policy

## Supported versions

Only the latest tagged release is supported. Older tags do not receive
security patches — pin to a known-good tag and upgrade to receive
fixes.

## Reporting a vulnerability

Do NOT open a public issue for a security bug.

Use GitHub's private vulnerability reporting:

**[Report a vulnerability](https://github.com/shubam-disseqt/shubam-ai-code-reviewer/security/advisories/new)**

If GitHub advisories are unavailable to you, email
`security@disseqt.ai` with the subject prefix `[shubam-ai-code-reviewer]`.

Response targets:

| Milestone | Target |
|---|---|
| Acknowledgement | 3 business days |
| Initial assessment | 7 business days |
| Fix / mitigation released | 14 business days for High / Critical severity |

## AI policy for contributions

`shubam-ai-code-reviewer` is an AI tool authored partly with AI assistance. That
places extra responsibility on human contributors. All rules in
[AGENTS.md](AGENTS.md) apply, in particular:

- Disclose AI/LLM use in the initial issue or PR.
- Understand every line of code — including the parts an AI wrote.
- Reviewer Q&A substance comes from the human, not the model.
- No `AI-generated → fixed → fixed → fixed` cycles.

Contributions that appear to be unreviewed AI output will be closed
without merge.

## Release signatures

Release binaries are attested via GitHub's SLSA build-provenance
mechanism (`actions/attest-build-provenance@v4`). This is not the same
as GPG or cosign — the trust root is GitHub's OIDC.

To verify a downloaded binary:

```sh
gh attestation verify --owner shubam-disseqt ./sacr-linux-amd64
```

The `scripts/install.sh` script performs a SHA-256 checksum check against a
`sha256sum.txt` published alongside each release. Users who need
supply-chain verification beyond checksums should run the `gh
attestation verify` step manually — do not skip it in production
provisioning.

## Threat model

The full threat model, actors, and trust boundaries live in
[docs/ARCHITECTURE.md § 5. Trust boundaries and threats](docs/ARCHITECTURE.md#5-trust-boundaries-and-threats).
Top summary:

| ID | Threat | Mitigation |
|---|---|---|
| T1 | Command injection via crafted diff content | Only `git` is exec'd, hardcoded subcommands, `--end-of-options`, no shell |
| T2 | API key leakage | Env-var only; never logged; never written to session files |
| T3 | Path traversal via LLM-suggested file paths | `pathutil.WithinBase()` on every path, pre- and post-symlink |
| T4 | DNS rebinding against `sacr docs` server | Host-header allowlist; loopback binds by default |
| T5 | MITM on API communication | TLS 1.2+, full certificate verification, `InsecureSkipVerify` never used |
| T6 | Malicious LLM response | JSON schema + line-number bounds validation; line-snapping re-derives positions |
| T7 | Malicious LLM response (over-escaped JSON) | Comment-args-repair refuses partial recovery |
| T8 | Dependency CVEs | `govulncheck` in CI; Dependabot; `go.sum` integrity |
| T9 | Sensitive code to third-party LLM | Same posture as user's existing LLM tooling; document; org rules can steer scrutiny |

## Scope

In scope:

- The `sacr` binary and its Go source.
- The `scripts/install.sh` and `scripts/install.ps1` scripts.
- The GitHub Action wrapper (`action.yml`).
- The embedded docs served by `sacr docs`.

Out of scope:

- Third-party LLM provider APIs — report vulns to the provider directly.
- User-provided org-rules repositories and their contents.
- User-provided Postgres / SQLite databases used as the index backend.
- Documentation site hosted at `https://shubam-disseqt.github.io/shubam-ai-code-reviewer/`
  (report to the CI/CD pipeline maintainer instead).
