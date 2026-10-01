<script lang="ts">
import type { UsageSummaryRow } from '@screentime/shared';
import { appLabel, formatDuration, percent } from '../lib/format';
import { store } from '../lib/store.svelte';
import AppAvatar from './AppAvatar.svelte';

const {
  rows,
  limit = 8,
  kind = 'app',
  empty = 'Nothing recorded yet.',
}: { rows: UsageSummaryRow[]; limit?: number; kind?: 'app' | 'domain'; empty?: string } = $props();

const shown = $derived(rows.slice(0, limit));
const top = $derived(shown[0]?.ms ?? 0);
</script>

{#if shown.length === 0}
	<p class="py-6 text-center text-sm text-muted">{empty}</p>
{:else}
	<ul class="flex flex-col gap-3">
		{#each shown as row (row.key)}
			{@const color = kind === "app" ? (store.categoryOf(row.key)?.color ?? "#64748b") : "var(--accent)"}
			<li class="flex items-center gap-3">
				{#if kind === "app"}
					<AppAvatar appId={row.key} />
				{:else}
					<span class="grid size-8 shrink-0 place-items-center rounded-lg bg-surface-2 text-xs font-semibold text-muted">
						{row.key.charAt(0).toUpperCase()}
					</span>
				{/if}
				<div class="min-w-0 flex-1">
					<div class="flex items-baseline justify-between gap-2">
						<span class="truncate text-[13px] font-medium">
							{kind === "app" ? appLabel(row.key, store.appName(row.key)) : row.key}
						</span>
						<span class="shrink-0 text-xs text-muted tabular-nums">{formatDuration(row.ms)}</span>
					</div>
					<div class="mt-1.5 h-1.5 overflow-hidden rounded-full bg-surface-2" title="{percent(row.ms, top)}% of the top item">
						<div class="h-full rounded-full" style="width: {percent(row.ms, top)}%; background: {color}"></div>
					</div>
				</div>
			</li>
		{/each}
	</ul>
{/if}
