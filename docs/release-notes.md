# Release Notes

## Next Patch Release

- Keeps `/start owner=<owner_token>` for owner bootstrap and `/start invite=<invite_token>` for collaborator onboarding.
- Keeps Telegram-safe deep-link payloads as `owner_<token>` and `invite_<token>`.
- Stops emitting owner authentication tokens and auth URLs in normal startup logs.
- Adds `relay --version` output with release version, commit, and build date metadata.
- Aligns CI linting with the project-local Go toolchain and adds `govulncheck` to the release-readiness gate.
