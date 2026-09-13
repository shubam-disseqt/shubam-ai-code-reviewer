# Governance

## Goals

- Ship and maintain a Go-native CLI that combines the strengths of
  `alibaba/open-code-review` and `miracodeai/mira` into a single
  tool suitable for CI-driven pull request review.
- Keep the tool auditable, self-hosted, and free of third-party
  service dependencies beyond the LLM provider the user chooses.

## Scope

Fixed by [docs/ARCHITECTURE.md § 9. Non-goals](docs/ARCHITECTURE.md#9-non-goals).
Scope changes go through a design issue with maintainer approval.

## Values

1. Predictable output over clever output.
2. Deterministic engineering around LLM behavior, not away from it.
3. Fewer moving parts. Every dependency and every feature earns its keep.
4. Attribution is not optional. Both upstream projects are Apache-2.0
   and we honor that visibly.
5. Documentation is a first-class artifact. If a feature is not
   documented, it is not shipped.

## Roles

### Contributors

Anyone opening a PR or issue that follows [CONTRIBUTING.md](CONTRIBUTING.md)
and [AGENTS.md](AGENTS.md). Contributions are welcome; scope creep is
not.

### Maintainers

Maintainers have merge rights on `main`. Their job:

- Triage issues, review PRs, keep the roadmap honest.
- Hold the line on scope, quality, and attribution.
- Cut releases.
- Enforce the code of conduct.

Maintainer status is granted by consensus of existing maintainers,
usually after a sustained pattern of high-quality contribution.

### Project lead

The project lead has final say on scope disputes, direction changes,
and roles. The current project lead is named in the repository owner
(`shubam-disseqt`).

## Decision making

**Day-to-day decisions** (bug fixes, refactors, small features) — one
maintainer's approval on a PR is sufficient.

**Significant decisions** (new features, dependency additions, scope
changes, breaking flag changes) require:

1. A design issue with at least 72 hours of open comment period.
2. Rough consensus among active maintainers. Objections must be
   substantive, not just "I don't like it."
3. On failure to reach consensus, the project lead decides.

**Voting** is used only when consensus stalls and a decision is
time-sensitive. Simple majority of active maintainers. Abstentions do
not count.

## Reviews and merges

- Every PR needs at least one maintainer approval.
- Security-touching PRs (auth, TLS, path handling, exec) need two
  approvals and a review from the security-listed contact in
  [SECURITY.md](SECURITY.md).
- Attribution-touching PRs (new lifts from OCR or Mira, or changes to
  [NOTICE](NOTICE) or [docs/PORTING.md](docs/PORTING.md)) need approval from a
  maintainer who has read the upstream license.
- No self-merge on non-trivial changes.

## Releases

- Semver. `v0.x.y` while API is in flux; `v1.0.0` after Phase 11 pilot.
- Cut from `main`. No release branches unless a security patch needs
  backporting.
- Release notes generated from Conventional Commit history. Bucketed
  by `feat` / `fix` / `refactor` / `docs` / other.
- Every release is attested via GitHub's SLSA build-provenance.
- Breaking changes require a minor version bump pre-1.0 and a major
  bump post-1.0. Deprecation warnings ship one minor version before
  removal.

## Continuity

If the project lead becomes unavailable, the maintainer group elects a
new lead by majority vote. If the maintainer group dwindles below two
active members, active contributors may petition GitHub for repository
transfer per GitHub's ToS.

The repository history and issues are the authoritative record.
Governance decisions land in the repo — no side channels.

## Amendments

Changes to this file are themselves significant decisions and follow
the process above.
