# Contributing

## Getting Started

```bash
git clone https://github.com/thedavidweng/amazon-photos.git
cd amazon-photos
mise install
mise run check
```

Requires Go 1.26 or later.

## Development Workflow

```bash
# Run all quality gates: fmt, build, test, lint, conventions
mise run check

# Individual tasks
mise run fmt
mise run build
mise run test
mise run lint
mise run conventions
```

## Project Structure

```text
cmd/aphotos/          CLI entrypoint
pkg/aphotos/          Public Go SDK (zero third-party dependencies)
internal/
  cli/                Cobra commands and CLI E2E contract tests
  config/             Atomic 0600 profile config and multi-format cookie parser
  model/              JSON envelope, error codes, and exit code mapping
  output/             Human/JSON renderer with strict redaction
docs/
  adr/                Architecture Decision Records
  agents/             Agent conventions and triage labels
```

## Engineering Rules

1. **SDK Purity**: `pkg/aphotos` must have zero CLI/GUI framework dependencies, perform zero network or filesystem I/O in `NewClient`, and route all HTTP requests through an injectable `http.Client` / `http.RoundTripper`.
2. **Test-First & Offline Determinism**: Write contract tests against injected `http.RoundTripper` / `httptest.Server` seams before implementation. Never require live Amazon credentials in CI or commit real session cookies.
3. **Code and Docs Move Together**: Any change to CLI commands, flags, JSON envelope fields, or exit codes must update `COMMANDS.md` and `JSON_SCHEMA.md` in the same change.
4. **Comment Budget**: Keep code self-explanatory. CI enforces a comment budget of `<= 5%` of non-test Go lines (`mise run conventions`).
5. **Credential Redaction**: Never log or print raw cookie values (`session-id`, `ubid-*`, `at-*`), `x-amzn-sessionid`, or pre-signed `contentRedirection` URLs.

## Pull Requests

1. Fork the repository and create a topic branch.
2. Add or update contract tests covering the change.
3. Verify `mise run check` passes with zero warnings.
4. Open a pull request referencing the relevant issue.

## License

By contributing to `amazon-photos`, you agree that your contributions will be licensed under the [MIT License](LICENSE).
