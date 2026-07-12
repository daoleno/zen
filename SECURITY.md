# Security Policy

## Supported versions

This project is in an early public beta. Security fixes target the default branch (`main`). There are no long-term supported release trains yet.

## What Zen trusts

- You operate the daemon host and any tunnel/proxy in front of it.
- Device pairing and signed requests protect the control plane once enrolled.
- AI executor CLIs on the host may run with powerful local permissions; see `docs/executors.md`.

## Reporting a vulnerability

Please report security issues privately to the repository maintainers (GitHub Security Advisories preferred when enabled for the repo, otherwise a private maintainer contact).

Include:

- affected component (daemon, app, pairing, upload, executor launch)
- reproduction steps
- impact assessment (auth bypass, remote code execution on host, data exposure)

Do **not** open a public issue for unpatched remotely exploitable flaws.

## Out of scope for “Zen cloud” language

Zen does not operate a hosted relay or store your agent transcripts in a Zen-operated cloud. Host compromise, misconfigured public tunnels, and bypass-permission executors are operator risks documented in `docs/security-and-privacy.md`.
