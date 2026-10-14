# AGENTS.md

Instructions for every coding agent working in this repo. This is the only agent-instruction file — never create CLAUDE.md, .cursorrules, .windsurfrules, .clinerules, GEMINI.md, or any other per-tool variant (CI fails the build if one appears).

## Agent skills

### Issue tracker

Issues and specs live in GitHub Issues (`thedavidweng/amazon-photos`). See `docs/agents/issue-tracker.md`.

### Triage labels

Five canonical roles; label strings equal role names (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: root `GLOSSARY.md` / `CONTEXT.md` + `docs/adr/`. See `docs/agents/domain.md`.

## Code principles

1. Self-explanatory code, minimal comments. Write a comment only for a constraint the code cannot express (a protocol quirk, a required ordering, a spec reference). Decision rationale goes in `docs/adr/`, conventions go here — never in code. CI enforces a comment budget of 5% of non-test lines.
2. Fewest lines that stay clear. No fallback code, no speculative features, no dead code, no abstraction with a single caller. Delete before you add.
3. Code and docs move together: a change to commands, flags, output, or behavior updates the cobra help, `docs/reference/cli-commands.md`, and `docs/reference/json-schema.md` in the same change.
4. SDK package (`aphotos`) has zero framework/CLI dependencies, zero I/O on `NewClient`, and injects `http.Client` / `http.RoundTripper`.

## Verification

`mise run check` (fmt-check + mod-tidy-check + vet + build + test + test-race + lint + conventions) must pass before every push. CI runs the same gates.

## Testing

- Never write unit tests after code.
- Primary SDK seam is injected `http.RoundTripper` (plus `httptest.Server` for streaming/redirect integration).
- CLI seam is Cobra root execution capturing stdout/stderr, exit codes, and JSON envelope.
- Never include real account cookies, tokens, or unredacted personal data in fixtures or tests.

## Decisions

Architectural decisions live in `docs/adr/NNNN-slug.md` (Status / Context / Decision / Consequences).
