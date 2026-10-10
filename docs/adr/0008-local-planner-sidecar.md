# 8. Daily planner and activity categorisation through a `llama-server` sidecar

Date: 2026-10-08

## Status

Proposed. Design only: nothing in this ADR is implemented. It is the design ADR that step 7 of [the migration plan](../migration-to-go.md) asks for before planner work starts (issue #28).

## Context

Two features are planned that need a language model:

- **Categorisation.** Apps are assigned a category by substring rules (`internal/categories`) on first sight. Anything the rules miss lands in *Other*, and the user fixes it by hand.
- **Daily planner.** A short plan for the day, built from recent usage and a goal the user types, proposing limits, focus sessions and breaks.

Both must follow the product's promises: no telemetry, no network calls from the daemon ([ADR 5](0005-privacy-defaults.md), README), data stays on the machine, and the daemon stays light (10 MB RSS measured for the Go daemon, against a 60 MB budget). A model runtime is the opposite of light, so the design is mostly about containing it.

[ADR 7](0007-migrate-to-go.md) and the migration notes already name the runtime: `llama-server` from llama.cpp, as a sidecar over localhost HTTP, without cgo bindings. This ADR records why, and fills in the lifecycle, privacy, memory and failure design that choice leaves open.

## Decision

### Scope

- **Categorisation.** Only for apps that no rule matches and the user has not categorised. The model picks one of the *existing* categories; it cannot create categories. User choices are never overwritten (the invariant the daemon has today).
- **Planner.** Produces *suggestions*. It never changes limits, focus mode or settings itself; the user accepts a suggestion in the UI, and that goes through the existing `limits.set` and similar methods.
- **Inputs are aggregates only:** per-app and per-category totals for the last 7 days, today so far, the current limits, the app's `.desktop` `Name`, `Comment` and `Categories`, and the user's goal text. Window titles and browser hostnames are **never** sent to the model, even when title capture is on. If a later feature wants them it needs its own opt-in and an amendment here.
- **Off by default.** `plannerEnabled` defaults to `false`, in the same way as `captureTitles`. While it is off the daemon never looks for `llama-server`, never reads a model file and never starts a process.

### Runtime: `llama-server` as a sidecar, not cgo

| Option | Verdict |
| --- | --- |
| cgo bindings to llama.cpp in `screentimed` | Rejected. It ends the static, cross-compilable binary (the reason SQLite is `modernc.org/sqlite`), makes Windows and Linux builds need a C++ toolchain, keeps the model's memory inside the always-on process (a model cannot be reliably unloaded from a long-lived Go process), and lets a crash in C++ code take down the tracker. llama.cpp's API also changes quickly, so bindings lag it. |
| Ollama | Rejected. A second always-on daemon with its own model store and its own network pulls; the opposite of what ADR 5 needs. |
| `llama-server` as a child process | Chosen. Its failures are contained to a process we can kill. It speaks an OpenAI-compatible HTTP API with constrained JSON output (`response_format` / grammars) and embeddings, which covers both features. The daemon stays pure Go and gains only `net/http` and `os/exec`, both of which it already links. |

Costs we accept: an extra binary the user has to have, an HTTP surface to secure, and process supervision code.

`llama-server` is **not bundled** in the `.deb` or the release binaries: it is large, it is CPU-feature-specific, and bundling it would make the "lightweight" claim false for everyone to serve a feature most people leave off. The package `Suggests:` it where a distribution packages it; otherwise the user installs it or sets `plannerServerPath`. We do not redistribute models either.

### Lifecycle

The sidecar belongs to a small supervisor in `internal/planner`. States: `off`, `starting`, `ready`, `stopping`, `error`.

1. **Start on demand.** A planner or categorisation request when the state is `off` starts it; nothing starts at daemon boot. Requests wait for `ready` (bounded by a start timeout, default 60 s, since loading a model from a cold disk takes seconds) and then run one at a time (`--parallel 1`).
2. **Command line.** Roughly `llama-server --model <file> --host 127.0.0.1 --port <ephemeral> --ctx-size 4096 --parallel 1 --threads <min(4, NumCPU/2)> --n-gpu-layers 0 --no-webui --offline`. Each flag must be checked against the oldest `llama-server` build we accept; the supervisor runs `--version` at first use and refuses builds below a pinned minimum rather than guessing.
3. **Readiness.** Poll `GET /health` until it returns 200.
4. **Idle shutdown.** After the last request finishes an idle timer starts (default 5 minutes, setting `plannerIdleMinutes`). When it fires: `SIGTERM`, then `SIGKILL` after 5 s. The next request starts it again. The planner runs a few times a day, so the common case is a start, a burst of requests and a stop.
5. **Dies with the daemon.** On Linux the child is started with `Pdeathsig: SIGKILL`, and the daemon stops it in its shutdown path. A pid file in `$XDG_RUNTIME_DIR/screentime/` lets the next daemon reap an orphan (checking `/proc/<pid>/exe` first so a reused pid is not killed). Windows would use a Job Object with kill-on-close; macOS has no equivalent, so the pid-file reap is the only guard there.
6. **Lower priority.** `nice 19` and idle I/O class, so it yields to what the user is doing. Requests run when asked; a request does not wait for the user to go idle, because the planner is something the user asks for.
7. **Suspend/resume.** After a resume the supervisor re-checks `/health` before the next request and restarts the child if it is gone.

### Preserving ADR 5 (no network from the daemon)

The promise in the README and ADR 5 is that the daemon makes no network calls. A localhost HTTP client is not that, but it is close enough to the line that the design must not weaken the wording of the promise in practice:

- **Loopback only.** Preferred transport is a **Unix socket** in `$XDG_RUNTIME_DIR/screentime/` (directory `0700`, checked as in S2), if the pinned `llama-server` supports binding one (recent builds accept a `--host` ending in `.sock`; to be verified). That removes TCP from the picture altogether. If it is not supported, the fallback is `127.0.0.1` on an ephemeral port, with a random per-start key passed through `--api-key-file` (a `0600` file in the runtime directory, never on the command line) so another local user or process cannot use the model. If neither is possible on the installed version, the planner refuses to start.
- **No network for the child.** `llama-server` is started with `--offline`, never with `-hf`, `--model-url` or any option that fetches. On Linux, when unprivileged user namespaces are available and the Unix-socket transport is in use, the child additionally gets `CLONE_NEWUSER|CLONE_NEWNET`, so it has no network at all, whatever it does. This is defence in depth and best-effort: if namespaces are unavailable it is logged, not fatal.
- **No download code in the daemon.** The daemon contains no code that fetches a model or a binary. In this ADR's v1, model files arrive by the user's own action:
  1. The user downloads a `.gguf` file themselves (the docs name a specific file, its source and its SHA-256).
  2. `screentimed planner import <file>` (and the matching UI action) hashes the file and compares it to the SHA-256 values in a manifest embedded in the daemon binary. A match is copied into `$XDG_DATA_HOME/screentime/models/` (`0600`, directory `0700`). A mismatch is refused unless the user passes `--allow-unverified`, which records the hash and shows it in the UI, since a model file is trusted input to a process running as the user.
  3. The daemon records hash, size and mtime in settings and re-hashes only when size or mtime change, so each start does not read gigabytes.
- **A one-click "download model" button is deliberately not part of v1.** It would be the first network request anywhere in the product, and the README promise would need rewording ("the daemon never makes a network call" would become "the only network request is a model download you start"). That is a product decision, not an implementation detail, and it needs its own amendment. If it is added, the request is made by the UI shell, never the daemon, from a pinned URL with the pinned hash, and only on a click that states the size and host.
- **Wording to adopt when this ships:** "The daemon never makes a network call. With the planner enabled it talks to a local `llama-server` process over a Unix socket on your machine; that process is started with networking disabled."
- **No prompt logging.** The sidecar's stdout/stderr are captured, truncated and kept only in memory for the `planner.status` error field. Prompts and answers are not written to disk or logs.

### Memory and CPU budget

- **Daemon:** unchanged, about 10 MB, plus the supervisor and an idle HTTP client. Nothing planner-related is allocated while the feature is off.
- **Sidecar:** separate, transient, and outside the 60 MB budget. Expected rough figures for a small instruction-tuned model (1 to 3 billion parameters, 4-bit): a 1 to 2 GB model file, mapped into memory, plus a KV cache for a 4096 token context in the low hundreds of MB. Plan for a peak of about 2.5 GB RSS. **These are estimates to be measured, not measurements**; step 7 records real figures per candidate model.
- **Guard rails:** `--ctx-size 4096`, one slot, CPU-only (`--n-gpu-layers 0`, avoiding driver and VRAM surprises; GPU is a later opt-in), and the supervisor refuses to start if `MemAvailable` in `/proc/meminfo` is below the model file size plus 512 MB, telling the user why.
- **Prompt size:** inputs are aggregates, built to fit well under the context: top 20 apps for the week, not raw events.

### RPC surface (sketch)

All additive; the frozen contract methods do not change. Parameters are size-capped and validated like every other method (S7).

| Method | Params | Result |
| --- | --- | --- |
| `planner.status` | none | `{enabled, state, serverFound, serverVersion, modelInstalled, modelSha256, modelVerified, lastError}` |
| `planner.importModel` | `{path}` (and `allowUnverified`) | `{sha256, verified}` |
| `planner.categorize` | `{appIds?: string[]}`, capped at 50 | `[{appId, categoryId, confidence}]`, only for apps that were uncategorised |
| `planner.suggest` | `{date, goal?}` (goal capped at 500 chars) | `{summary, suggestions: [{kind: "limit"\|"focus"\|"break"\|"note", target?, minutes?, reason}]}` |
| `planner.stop` | none | stops the sidecar now |

Settings added through the existing `settings.set`: `plannerEnabled` (default `false`), `plannerServerPath`, `plannerIdleMinutes`. An `event.planner` notification carries state changes so the UI can show "loading model...".

**Structured output.** Both calls use a JSON schema (or grammar) so the model can only answer in the expected shape, and the daemon validates the answer anyway. `categorize` answers are checked against the real category list; `suggest` targets must name an existing app or category, and minutes are bounded. Anything invalid is dropped, not repaired.

**Untrusted input.** App names and `.desktop` fields come from other programs, so a hostile one could contain instructions for the model. The structure above is the defence: the model has no tools, its output is constrained and validated, and it only ever proposes things the user must accept.

**Storage.** A model-assigned category is stored with its source (a new column on `apps`, via a migration) so the UI can mark it as automatic and the user can override it, and so turning the planner off can offer to clear model-assigned categories.

### Failure modes

| Failure | Behaviour |
| --- | --- |
| `llama-server` not found, or too old | `planner.status` explains; the feature stays unavailable; tracking is unaffected |
| No model, or hash mismatch | Same; an unverified model needs the explicit flag above |
| Not enough free memory | Refuse to start, say how much is needed |
| Start timeout | Kill the child, `state=error`, retry on the next request, not in a loop |
| Child crashes mid-request | Retry once after restart, then return an error to the caller |
| Invalid or schema-violating output | Drop it; for `categorize`, fall back to the rule result (*Other*) |
| Slow CPU | Per-request deadline (120 s); the UI shows progress from `event.planner` |
| Daemon killed with `SIGKILL` | `Pdeathsig` ends the child; if that fails, the pid file lets the next daemon reap it |
| Socket path or port in use | Choose a fresh one; never connect to something that is not our child (key and pid check) |
| User turns the planner off while running | Stop the sidecar at once; discard the pending requests |

None of these may affect time tracking, rules or the RPC server: the planner runs behind its own goroutines and a bounded queue, and a request that cannot be served returns an error instead of blocking a tick.

### Package layout

`internal/planner` holds the supervisor, the HTTP client, the prompt builders and the schemas, and nothing else imports `llama-server` concepts. The daemon holds a `*planner.Supervisor` that is nil while the feature is off.

### Acceptance before this moves to Accepted

- A fixture set of real app IDs and the categories a person would give them; the model must beat the rule-only baseline by a margin we state in advance, on at least one candidate model with a permissive licence. If it does not, categorisation is dropped and only the planner remains.
- A measured peak RSS and time-to-first-answer on a modest laptop CPU for each candidate, recorded in the migration notes.
- The `llama-server` flags above checked against the oldest accepted build.

## Consequences

- Lightness holds for everyone who leaves the feature off: no new dependencies, no new processes, no new allocations.
- Anyone who turns it on needs a separately installed `llama-server` and about 1 to 2 GB of disk and a few GB of RAM while it runs. That is stated in the UI before they enable it.
- The README's network wording gets one more sentence, and the model download question is left explicitly open instead of being slipped in.
- Model quality for planning is unproven. The acceptance criteria make failing it a legitimate outcome.
- The Windows and macOS providers are unaffected, but the same sidecar can run there later, with a Job Object on Windows and a pid-file reap on macOS.

## Open questions

- [ ] Which candidate models, and which licence terms are acceptable to point users at (Apache 2.0 and MIT models only?)
- [ ] Does the pinned `llama-server` support a Unix-socket `--host` and `--api-key-file`? If not, is TCP plus a key acceptable, or should the planner wait for a build that does?
- [ ] One-click model download: wanted at all, and if so, in the UI shell with what wording?
- [ ] Should model-assigned categories be written directly or kept as suggestions the user confirms? This ADR writes them with their source marked.
