# 1. Record architecture decisions

Date: 2026-09-25

## Status

Accepted

## Context

We need a lightweight way to record the architectural decisions made on this project as it grows past what fits in `docs/architecture.md`.

## Decision

We will use Architecture Decision Records, as described by Michael Nygard, and keep them in `docs/adr/`. Each ADR is numbered sequentially, immutable once accepted (superseded by a new ADR rather than edited), and follows this template:

```
# N. Title

Date: YYYY-MM-DD

## Status
Proposed | Accepted | Superseded by ADR-M

## Context
What forces are at play, including technological, political, social, and project-local.

## Decision
What we decided to do.

## Consequences
What becomes easier or harder as a result.
```

## Consequences

Future significant decisions (e.g. choosing Rust for the daemon if Bun's footprint is too heavy, adding a privileged helper) get a durable, dated record independent of `docs/architecture.md`, which stays focused on the current state of the system.
