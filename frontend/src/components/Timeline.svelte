<script lang="ts">
import type { Session } from '@screentime/shared';
import { appLabel, clockTime, formatDuration } from '../lib/format';
import { store } from '../lib/store.svelte';

const { sessions, dayStart }: { sessions: Session[]; dayStart: number } = $props();

const DAY = 24 * 60 * 60_000;
const hours = [0, 3, 6, 9, 12, 15, 18, 21];

const blocks = $derived(
  sessions.map((s) => {
    const start = Math.max(s.startTs, dayStart);
    const end = Math.min(s.endTs, dayStart + DAY);
    const category = store.categoryOf(s.appId);
    return {
      id: s.id,
      left: ((start - dayStart) / DAY) * 100,
      width: Math.max(((end - start) / DAY) * 100, 0.18),
      color: category?.color ?? '#64748b',
      label: `${appLabel(s.appId, store.appName(s.appId))} · ${clockTime(start)}–${clockTime(end)} (${formatDuration(end - start)})`,
    };
  }),
);
</script>

<div>
	<div class="relative h-9 overflow-hidden rounded-lg bg-surface-2">
		{#each hours as h}
			<div class="absolute top-0 h-full w-px bg-line" style="left: {(h / 24) * 100}%"></div>
		{/each}
		{#each blocks as b (b.id)}
			<div
				class="absolute top-1.5 h-6 rounded-[3px] transition-opacity hover:opacity-80"
				style="left: {b.left}%; width: {b.width}%; background: {b.color}"
				title={b.label}
			></div>
		{/each}
	</div>
	<div class="relative mt-1.5 h-4 text-[11px] text-muted">
		{#each hours as h}
			<span class="absolute -translate-x-1/2 first:translate-x-0" style="left: {(h / 24) * 100}%">{String(h).padStart(2, "0")}:00</span>
		{/each}
	</div>
</div>
