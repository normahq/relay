# relay

[![test](https://github.com/normahq/relay/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/normahq/relay/actions/workflows/test.yml)
[![lint](https://github.com/normahq/relay/actions/workflows/lint.yml/badge.svg?branch=main)](https://github.com/normahq/relay/actions/workflows/lint.yml)

Relay is a Telegram-first control plane for long-running Norma agent sessions.
It gives you one authenticated owner channel, root orchestration in direct chat, and topic-scoped sessions started with `/new`.

## Install

```bash
npm install -g -y @normahq/relay
```

## Quickstart

1. Initialize relay in your project:

```bash
relay init
```

`relay init` will:
- require and validate your Telegram bot token
- let you store the token in `.env` (default) or `config.yaml`
- create `.config/relay/config.yaml`
- create `.config/relay/relay.db`
- generate an owner token and print auth/start commands

2. Start relay:

```bash
relay start
```

3. Authenticate in Telegram:
- open the auth URL printed by `relay init`/`relay start`, or
- send `/start <owner_token>` in a direct message to your bot

4. Start a topic session:

```text
/new [provider_id]
```

## Bot Commands

- `/start <owner_token>`: direct-message auth/bootstrap (also accepts collaborator invite token).
- `/new [provider_id]`: owner only, direct message only; starts a topic session (uses `relay.provider` when omitted).
- `/close`: owner only, direct message only; closes current topic session or stops root session.
- `/cancel`: owner only; cancels in-flight turn and drops queued turns for current session.
- `/user add`: owner only; generates collaborator invite link.
- `/user list`: owner only; lists collaborators and active invites.
- `/user remove <user_id>`: owner only; removes a collaborator.

## Config At A Glance

Relay loads `.config/relay/config.yaml` and then applies `RELAY_*` environment overrides.
If present, `.env` from the current working directory is auto-loaded before config resolution.

```yaml
runtime:
  providers: {}
  mcp_servers: {}
relay:
  provider: ""
  telegram:
    token: ""
  workspace:
    mode: "auto"
    base_branch: ""
  mcp_servers: []
  system_instructions: ""
profiles: {}
```

## Troubleshooting

- `telegram token is required`:
  - set `RELAY_TELEGRAM_TOKEN` in `.env` (default init flow), or
  - set `relay.telegram.token` in `.config/relay/config.yaml`
- `relay.provider is required`:
  - run `relay init` and choose a provider, or
  - set `relay.provider` to a value from `runtime.providers`
- `provider "<id>" not available` on `/new`:
  - use one of the provider IDs registered in `runtime.providers`
- workspace import/export issues:
  - verify `relay.workspace.mode` and `relay.workspace.base_branch`
  - when `mode: on`, run relay in a Git repository

## Documentation

- Technical specification: [`docs/relay.md`](docs/relay.md)
- Contributing guide: [`CONTRIBUTING.md`](CONTRIBUTING.md)
- Agent workflow/policies: [`AGENTS.md`](AGENTS.md)

## Release

- GitHub Releases: <https://github.com/normahq/relay/releases>
- npm package: <https://www.npmjs.com/package/@normahq/relay>
