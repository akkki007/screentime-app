# Security Policy

## Our commitment

Screentime is a local-first app: the daemon never makes network calls and all data stays on the user's machine in `~/.local/share/screentime/`. That said, the daemon runs continuously as a `systemd --user` service and listens on a Unix socket, so security issues in it are still taken seriously.

## Reporting a vulnerability

**Please do not open a public GitHub issue for security vulnerabilities.**

Instead, report it privately using [GitHub's private vulnerability reporting](../../security/advisories/new) for this repository. Include:

- A description of the issue and its potential impact
- Steps to reproduce (or a PoC, if available)
- Affected version/commit

You should receive an acknowledgment within a few days. We'll work with you to understand and fix the issue before any public disclosure.

## Scope

Examples of in-scope issues:

- Unix socket (`$XDG_RUNTIME_DIR/screentime/daemon.sock`) permission or authentication bypass
- Privilege escalation via the daemon, GNOME extension, or (v2) privileged helper
- Ability for a limit/block to be silently bypassed in a way that misrepresents tracked data
- Injection via the native messaging host (browser extension → daemon)

Out of scope: the enforcement of screentime limits themselves is a "nudge," not a security boundary — the [architecture doc](docs/architecture.md#risks-and-open-questions) already treats v1 limits as bypassable by design, with tamper resistance planned for the v2 privileged helper.

## Supported versions

Until a 1.0 release, only the latest commit on `main` is supported.
