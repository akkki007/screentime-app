# 4. Wellbeing rules are derived from the database on each tick

Date: 2026-10-01

## Status

Accepted

## Context

Limits, break reminders, downtime and focus mode must keep working across suspend/resume (a Phase 3 exit criterion). Timers drift or never fire while the machine sleeps, and a "fire at T" timer can double-fire or miss after wake.

## Decision

`RulesEngine.tick()` runs every heartbeat (5 s) and recomputes everything from the database and the clock:

- A limit fires when today's clipped usage first reaches `daily_ms`; per-limit progress (`day`, `lastEmit`) only prevents repeats.
- Break accounting tracks *time away*. A gap between ticks longer than 60 s is treated as the machine having been asleep, and counts as away time.
- Pause expiry is checked from the heartbeat, not a `setTimeout`.
- Overlay/block limits re-appear every 5 minutes while the user keeps using the target; notify-only limits fire once per day.

State is held in memory only: after a daemon restart a limit already exceeded notifies once more, which is preferable to persisting more state.

## Consequences

- Rules are deterministic given a clock, so they are unit-tested with a fake one (`rules.test.ts`), including suspend.
- Resolution is the tick interval (5 s), which is fine for nudges.
- v1 limits remain nudges. Tamper-resistant enforcement stays with the v2 privileged helper.
