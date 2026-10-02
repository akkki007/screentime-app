# Daemon IPC contract

The frozen contract between the daemon and its clients (the UI, the browser native host): what every RPC method accepts and answers, recorded from the current Bun daemon. This is step 1 of the [migration to Go](../docs/migration-to-go.md). The Go daemon is done when it replays these fixtures.

```
schema/rpc.schema.json   JSON Schema for every method, notification and the browser host message
fixtures/seed.sql        data every fixture starts from
fixtures/<scenario>.json golden request/response exchanges, one file per scenario
harness/                 recorder, replayer and the scenario definitions
```

## Commands

```bash
bun test contract                    # replay every fixture against the Bun daemon
bun run --cwd contract schema        # regenerate schema/ after changing packages/shared/src/ipc.ts
bun run --cwd contract record        # re-record all fixtures (or: record <scenario>)
```

Fixtures change only on purpose. After re-recording, read the diff: every changed line is a behaviour change that every implementation must follow. CI fails if the schema is stale, if a scenario has no fixture, or if a replay doesn't match.

## How a fixture runs

Each scenario starts a fresh daemon in a throwaway directory:

1. A new database is created with the migrations from `packages/db/migrations`, then `fixtures/seed.sql` is applied.
2. The daemon starts with this environment:

   | Variable | Value | Why |
   | --- | --- | --- |
   | `SCREENTIME_FAKE_NOW` | `1790762400000` (Wed 2026-09-30 15:30 local) | Frozen clock. Timers still run in real time. |
   | `TZ` | `Asia/Kolkata` | +05:30 with no DST, so off-by-30-minutes day bugs show up |
   | `SCREENTIME_FOCUS_PROVIDER` | `none` | No desktop session: focus never changes, never idle |
   | `XDG_DATA_HOME`, `XDG_RUNTIME_DIR`, `HOME`, `XDG_DATA_DIRS` | temp dirs | Isolation; no `.desktop` files to look up |
   | `DBUS_SESSION_BUS_ADDRESS` | a path that doesn't exist | Nothing reaches the real desktop |

3. Each step is sent as one line on the socket. The harness collects everything that comes back until the response with the same id arrives, plus 30 ms. Steps without an id wait 150 ms, and must get no response.

A scenario must finish within 4 s, because the rules engine first ticks after 5 s; `record` enforces this. Notifications that only the tick or a real desktop can produce (`event.focus`, `event.limitHit`, `event.reminder`) are therefore not in the fixtures. They are covered by the tracker and rules unit tests, which are ported to Go as table-driven tests (migration step 2).

## Comparison rules

- **Responses** must match exactly: id, `result`, `error.code` and `error.message`.
- **Notifications** around a request must match as a multiset. Their order relative to the response is not part of the contract.
- **Loose messages** (`CONTRACT_LOOSE_MESSAGES=1`): the Bun daemon's validation messages come from Zod, and another implementation shouldn't have to copy Zod's wording. In this mode, a `field: reason` message only needs the same field, and any other message only needs the same error code. The UI shows these messages, so keep the `field: reason` shape.

## Running another implementation

The harness spawns `CONTRACT_DAEMON_CMD` (space-separated) instead of the Bun daemon:

```bash
CONTRACT_DAEMON_CMD="./bin/screentimed" CONTRACT_LOOSE_MESSAGES=1 bun test contract
```

The implementation must:

- honour the environment variables in the table above. `SCREENTIME_FAKE_NOW` is a test hook: it replaces every read of the current time. (The Bun daemon gets it from `harness/fake-clock.ts`, preloaded.)
- create its socket at `$XDG_RUNTIME_DIR/screentime/daemon.sock` and its database at `$XDG_DATA_HOME/screentime/screentime.db`, using the database the harness prepared (`PRAGMA user_version` is already current).
- exit on `SIGTERM`.

## Behaviour that is easy to get wrong in a port

These are all recorded in the fixtures; they are listed here because they surprise people.

- **Day boundaries are local.** `usage.summary` by `day` or `hour` splits sessions at local midnight and local hours. A session that crosses midnight is counted on both days. `usage.timeline` returns every session that overlaps the date, so that session appears on both dates.
- **Ranges clip usage but not exports.** `usage.summary` and `usage.web` count only the part of a session inside the range; a session overlaps when `end > from` and `start < to`. `data.export` includes every overlapping session in full.
- **Exports are byte-exact.** JSON export is `JSON.stringify(rows, null, 2)`. In Go, use `json.MarshalIndent` with a two-space indent and disable HTML escaping (`SetEscapeHTML(false)`). Timestamps are ISO 8601 UTC with milliseconds (`2026-09-30T03:30:00.000Z`). The filename uses the local date.
- **Params handling.** A method without params rejects any `params`, even `{}`. `data.wipe` requires a params object, even though `everything` defaults to `false`. Unknown keys are dropped silently, except by `settings.set`, which rejects them.
- **Limits are upserted by target.** `limits.set` without an id, for a target that already has a limit, replaces that limit and keeps its id.
- **Notifications have no response,** including ones for methods that would normally answer (`{"method":"tracker.status"}` with no id). Lines that aren't JSON objects with a `method` are ignored.
- **`event.status`** is sent by `tracker.pause`, a `tracker.resume` that un-pauses, and every `focus.start` and `focus.stop`, even a `focus.stop` when focus mode is off. It arrives before the response.
- **Ordering is fully defined.** Ties are broken explicitly, comparing strings bytewise (SQLite's `BINARY` collation; in Go, plain `<` on strings, not a locale-aware collator):
  - `usage.summary` and `usage.web`: by `ms` descending, then `key`.
  - `usage.timeline`: by start, then session id.
  - `apps.list`: by display name (`app_id` if there is none), then `app_id`. `data.wipe {everything: true}` empties it.
  - `categories.list` and `limits.list`: by id.
  - `data.export`: by start time, with desktop rows before web rows on a tie.
