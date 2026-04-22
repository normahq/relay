# relay — AGENTS.md

## Development Standards

- Follow idiomatic Go and Google Go best practices.
- Prefer project-local tooling via `go tool ...` when available.
- Use Conventional Commits for all commits.
- Sync shared branches with merge (`git pull --no-rebase`), not rebase.

## Quality Gates (Required)

Run before submitting changes:

```bash
go test -race ./...
go tool golangci-lint run
```

## Logging Policy

- Allowed: `github.com/rs/zerolog`, `log/slog`.
- Disallowed: `logrus`, `zap`, direct standard `log` usage.
- Initialize logging through `internal/logging.Init()`.
- Prefer structured logging fields over formatted strings.

## Relay Guardrails

- Keep Relay startup order strict: config load -> bundled MCP lifecycle -> root provider -> channel runtime.
- Keep channel/session boundaries stable (`chat_id`, `topic_id`) and preserve lazy restore semantics.
- Keep workspace mode behavior stable (`on|off|auto`) with safe defaults and explicit failures.
- Keep Relay MCP/server contracts backward compatible (`relay.agents.*`, `relay.workspace.*`, and alias tools).
- Keep config loading via app-specific `.config/relay/config.yaml` with `RELAY_*` env overrides.

## Bot Commands (Current Contract)

- `/start <owner_token>`: direct message only; owner authentication/bootstrap entrypoint, also used for invite-token collaborator onboarding.
- `/new [provider_id]`: owner only, direct message only; creates a topic session for the selected provider or defaults to `relay.provider`.
- `/close`: owner only, direct message only; closes a topic session or stops the root session.
- `/cancel`: owner only; cancels in-flight turn processing for the current session and drops queued turns.
- `/user add|list|remove <user_id>`: owner only; collaborator invite and management commands.
- Keep command behavior and access expectations backward compatible; when changing commands, update `README.md` and `docs/relay.md` as part of the same change.

## Documentation

- Product installation/usage docs are in `README.md`.
- Development/contribution workflow is in `CONTRIBUTING.md`.
- Relay technical spec and operational details are in `docs/relay.md`.

## Release

- Omnidist profile is authoritative (`.omnidist/omnidist.yaml`, profile `relay`).
- Version source is Git tags (`version.source: git-tag`).
- Publish flows are tag-driven via GitHub Actions (`release.yaml`, `omnidist-release.yaml`).
