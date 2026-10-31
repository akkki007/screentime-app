<script lang="ts">
import AppAvatar from '../components/AppAvatar.svelte';
import Card from '../components/Card.svelte';
import Icon from '../components/Icon.svelte';
import PlannerSlot from '../components/PlannerSlot.svelte';
import { daemon } from '../lib/api';
import { addDays, appLabel, formatDuration, startOfDay } from '../lib/format';
import { store } from '../lib/store.svelte';
import { latestOnly } from '../lib/usage';

let totals = $state(new Map<string, number>());
let query = $state('');
const guard = latestOnly();

async function load() {
  const ticket = guard.next();
  const today = startOfDay(Date.now());
  try {
    const rows = await daemon('usage.summary', {
      from: addDays(today, -6),
      to: addDays(today, 1),
      groupBy: 'app',
    });
    if (guard.isCurrent(ticket)) totals = new Map(rows.map((r) => [r.key, r.ms]));
  } catch (err) {
    store.notify('error', err instanceof Error ? err.message : String(err));
  }
}

$effect(() => {
  void store.tick;
  if (store.connected) void load();
});

const rows = $derived(
  store.apps
    .map((a) => ({ ...a, label: appLabel(a.appId, a.name), ms: totals.get(a.appId) ?? 0 }))
    .filter((a) => {
      const q = query.trim().toLowerCase();
      return !q || a.label.toLowerCase().includes(q) || a.appId.toLowerCase().includes(q);
    })
    .sort((a, b) => b.ms - a.ms || a.label.localeCompare(b.label)),
);

async function setCategory(appId: string, value: string) {
  const categoryId = value === '' ? null : Number(value);
  const result = await store.attempt(() => daemon('apps.setCategory', { appId, categoryId }));
  if (result) {
    await store.refreshApps();
    store.tick++;
  }
}
</script>

<div class="flex flex-col gap-5">
	<header>
		<h1 class="text-2xl font-medium tracking-tight">Apps</h1>
		<p class="mt-1 text-[13px] text-muted">Usage over the last 7 days. Categories drive the charts, focus mode and category limits.</p>
	</header>

	<PlannerSlot load={() => import("../components/CategorizeAction.svelte")} />

	<Card flush>
		<label class="flex items-center gap-3 border-b border-line px-5 py-4 text-muted">
			<Icon name="search" size={18} />
			<input
				type="search"
				bind:value={query}
				placeholder="Search apps…"
				aria-label="Search apps"
				class="min-w-0 flex-1 bg-transparent text-[15px] text-ink outline-none placeholder:text-muted"
			/>
			<span class="shrink-0 text-xs tabular-nums">{rows.length} app{rows.length === 1 ? "" : "s"}</span>
		</label>
		<div class="flex items-center justify-between px-5 pt-4 pb-1 text-[13px] text-muted">
			<span>Records</span>
			<span>Last 7 days</span>
		</div>
		{#if rows.length === 0}
			<p class="px-5 py-10 text-center text-[13px] text-muted">
				{store.apps.length === 0 ? "No apps tracked yet." : "No apps match your search."}
			</p>
		{:else}
			<ul class="flex flex-col px-3 pb-3">
				{#each rows as app (app.appId)}
					<li class="flex items-center gap-3 rounded-[10px] px-2 py-2 transition-colors hover:bg-surface-2">
						<AppAvatar appId={app.appId} size={30} />
						<p class="flex min-w-0 flex-1 items-baseline gap-2">
							<span class="truncate text-[14px]">{app.label}</span>
							<span class="truncate text-[13px] text-muted max-sm:hidden">{app.appId}</span>
						</p>
						<select
							class="field w-40 py-1"
							aria-label="Category for {app.label}"
							value={app.categoryId ?? ""}
							onchange={(e) => setCategory(app.appId, e.currentTarget.value)}
						>
							<option value="">Uncategorized</option>
							{#each store.categories as c (c.id)}
								<option value={c.id}>{c.name}</option>
							{/each}
						</select>
						<span class="w-20 shrink-0 text-right text-[13px] text-muted tabular-nums">{app.ms ? formatDuration(app.ms) : "—"}</span>
					</li>
				{/each}
			</ul>
		{/if}
	</Card>
</div>
