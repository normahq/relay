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

<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:ca08a54f -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

## Session Completion

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd dolt push
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
<!-- END BEADS INTEGRATION -->
