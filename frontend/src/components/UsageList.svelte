<script lang="ts">
import type { UsageSummaryRow } from '@screentime/shared';
import { appLabel, formatDuration, percent } from '../lib/format';
import { store } from '../lib/store.svelte';
import AppAvatar from './AppAvatar.svelte';
import Icon from './Icon.svelte';

const {
  rows,
  limit = 8,
  kind = 'app',
  empty = 'Nothing recorded yet.',
}: { rows: UsageSummaryRow[]; limit?: number; kind?: 'app' | 'domain'; empty?: string } = $props();

const shown = $derived(rows.slice(0, limit));
const total = $derived(rows.reduce((n, r) => n + r.ms, 0));
</script>

{#if shown.length === 0}
	<p class="py-6 text-center text-[13px] text-muted">{empty}</p>
{:else}
	<ul class="-mx-2 flex flex-col">
		{#each shown as row (row.key)}
			<li class="flex items-center gap-3 rounded-[10px] px-2 py-2 transition-colors hover:bg-surface-2">
				{#if kind === "app"}
					<AppAvatar appId={row.key} size={28} />
				{:else}
					<span class="grid size-7 shrink-0 place-items-center rounded-lg text-muted"><Icon name="globe" size={18} /></span>
				{/if}
				<p class="flex min-w-0 flex-1 items-baseline gap-2">
					<span class="truncate text-[14px]">{kind === "app" ? appLabel(row.key, store.appName(row.key)) : row.key}</span>
					{#if kind === "app"}
						<span class="truncate text-[13px] text-muted">{store.categoryOf(row.key)?.name ?? "Uncategorized"}</span>
					{/if}
				</p>
				<span class="shrink-0 text-[13px] text-muted tabular-nums" title="{percent(row.ms, total)}% of the total">{formatDuration(row.ms)}</span>
			</li>
		{/each}
	</ul>
{/if}
