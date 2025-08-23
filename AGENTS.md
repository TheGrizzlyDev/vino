# Repository Guidelines

## Project Structure & Module Organization
- Root module: `github.com/TheGrizzlyDev/vino` (Go 1.25).
- Commands: `cmd/delegatec/` — entrypoint binary that delegates to runc.
- Library code: `internal/pkg/runc/` — command model, CLI parsing, client.
- Tests: unit tests alongside packages (`*_test.go`), integration tests in `tests/integration/dind/`.
- Images: Dockerfiles under `images/` for development/integration use.
 - Parser spec: see `internal/pkg/runc/cli_parser_design.md` (authoritative Slots semantics and parsing rules).

## Build, Test, and Development Commands
- Build all: `go build ./...` — verifies packages compile.
- Build CLI: `go build -o bin/delegatec ./cmd/delegatec` — produces the delegating shim.
- Run CLI (example): `go run ./cmd/delegatec --delegate_path /usr/bin/runc run myct`.
- Unit tests: `go test ./internal/...` — fast package tests.
- Full test suite: `go test -v -cover ./...` — includes all packages.
- Integration (DinD): `go test ./tests/integration/dind -v` — requires Docker daemon.

## Coding Style & Naming Conventions
- Formatting: run `gofmt -s -w .`; CI-friendly: `go vet ./...` before pushing.
- Indentation: tabs (standard Go); files and packages use lowercase, underscores when needed.
- Exports: export only what is required from `internal/`; prefer clear, noun-based types and verb-based funcs.
- Errors: wrap with context (`fmt.Errorf("…: %w", err)`).

## Testing Guidelines
- Framework: standard `testing` with table-driven tests where practical.
- File names: `*_test.go`; test funcs `TestXxx` and benchmarks `BenchmarkXxx`.
- Coverage: aim for meaningful coverage in `internal/pkg/runc` (parsing and slot logic).
- Integration notes: DinD tests spin containers; ensure Docker is available and not running in rootless mode that blocks privileges.

## Commit & Pull Request Guidelines
- Commits: short, imperative subjects (e.g., “add tests for ParseAny”); prefixes like “WIP:” and “Breaking:” appear in history — use sparingly and squash before merge when possible.
- PRs: include problem statement, summary of changes, test evidence (`go test` output), and any breaking changes.
- Link issues: reference with `Fixes #123` or `Refs #123`.
- Screenshots/logs: attach relevant CLI output (e.g., failing `runc` args) when debugging parser changes.
