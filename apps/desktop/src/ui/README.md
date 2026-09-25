# UI (Svelte 5 + Tailwind)

Not started yet — this is Phase 2 ("Dashboard") in [`docs/architecture.md`](../../../../docs/architecture.md#roadmap), gated on Phase 0/1 (the Electrobun spike and the tracker daemon) landing first.

Planned contents once started:

- `App.svelte` — shell: today / week / per-app views, tray menu, pause control
- charts via uPlot (time series) and ECharts (breakdowns)
- talks to `../ipc-client.ts` (bridged from the Electrobun main process), never touches SQLite directly
