<script lang="ts">
import AppAvatar from '../components/AppAvatar.svelte';
import Card from '../components/Card.svelte';
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
	<header class="flex items-end justify-between gap-4">
		<div>
			<h1 class="text-2xl font-semibold tracking-tight">Apps</h1>
			<p class="text-sm text-muted">Usage over the last 7 days. Categories drive the charts, focus mode and category limits.</p>
		</div>
		<input
			type="search"
			bind:value={query}
			placeholder="Search apps"
			aria-label="Search apps"
			class="w-56 rounded-lg border border-line bg-surface px-3 py-1.5 text-[13px] placeholder:text-muted"
		/>
	</header>

	<Card>
		{#if rows.length === 0}
			<p class="py-8 text-center text-sm text-muted">
				{store.apps.length === 0 ? "No apps tracked yet." : "No apps match your search."}
			</p>
		{:else}
			<table class="w-full text-[13px]">
				<thead>
					<tr class="text-left text-xs text-muted">
						<th class="pb-3 font-medium">App</th>
						<th class="pb-3 font-medium">Category</th>
						<th class="pb-3 text-right font-medium">Last 7 days</th>
					</tr>
				</thead>
				<tbody>
					{#each rows as app (app.appId)}
						<tr class="border-t border-line">
							<td class="py-2.5 pr-3">
								<div class="flex items-center gap-3">
									<AppAvatar appId={app.appId} size={28} />
									<div class="min-w-0">
										<p class="truncate font-medium">{app.label}</p>
										<p class="truncate text-[11px] text-muted">{app.appId}</p>
									</div>
								</div>
							</td>
							<td class="py-2.5 pr-3">
								<select
									class="rounded-md border border-line bg-surface-2 px-2 py-1 text-[13px]"
									aria-label="Category for {app.label}"
									value={app.categoryId ?? ""}
									onchange={(e) => setCategory(app.appId, e.currentTarget.value)}
								>
									<option value="">Uncategorized</option>
									{#each store.categories as c (c.id)}
										<option value={c.id}>{c.name}</option>
									{/each}
								</select>
							</td>
							<td class="py-2.5 text-right text-muted tabular-nums">{app.ms ? formatDuration(app.ms) : "—"}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		{/if}
	</Card>
</div>
