<script lang="ts">
import type { Session } from '@screentime/shared';
import BarChart from '../components/BarChart.svelte';
import Card from '../components/Card.svelte';
import Donut from '../components/Donut.svelte';
import NowCard from '../components/NowCard.svelte';
import StatTile from '../components/StatTile.svelte';
import Timeline from '../components/Timeline.svelte';
import UsageList from '../components/UsageList.svelte';
import {
  addDays,
  appLabel,
  dateKey,
  formatDuration,
  formatShort,
  percent,
  startOfDay,
} from '../lib/format';
import { store } from '../lib/store.svelte';
import {
  type RangeUsage,
  fillHours,
  latestOnly,
  loadRange,
  loadTimeline,
  loadTotal,
} from '../lib/usage';

let usage = $state<RangeUsage>();
let sessions = $state<Session[]>([]);
let yesterdaySoFar = $state(0);
let dayStart = $state(startOfDay(Date.now()));
let loadError = $state<string>();

const guard = latestOnly();

async function load() {
  const ticket = guard.next();
  const now = Date.now();
  const start = startOfDay(now);
  const yStart = addDays(start, -1);
  try {
    const [range, timeline, yesterday] = await Promise.all([
      loadRange(start, addDays(start, 1)),
      loadTimeline(dateKey(start)),
      // Compare like with like: yesterday up to this time of day.
      loadTotal(yStart, yStart + (now - start)),
    ]);
    if (!guard.isCurrent(ticket)) return;
    usage = range;
    sessions = timeline;
    yesterdaySoFar = yesterday;
    dayStart = start;
    loadError = undefined;
  } catch (err) {
    if (guard.isCurrent(ticket)) loadError = err instanceof Error ? err.message : String(err);
  }
}

$effect(() => {
  void store.tick;
  if (store.connected) void load();
});

const total = $derived(usage?.total ?? 0);
const delta = $derived(total - yesterdaySoFar);
const productiveMs = $derived(
  (usage?.byCategory ?? [])
    .filter((r) => store.categories.find((c) => c.name === r.key)?.productive === 1)
    .reduce((n, r) => n + r.ms, 0),
);
const topApp = $derived(usage?.byApp[0]);
const hours = $derived(fillHours(usage?.byHour ?? []));
const hourLabels = Array.from({ length: 24 }, (_, h) =>
  h % 3 === 0 ? String(h).padStart(2, '0') : '',
);
const slices = $derived(
  (usage?.byCategory ?? []).map((r) => ({
    name: r.key,
    value: r.ms,
    color: store.categoryColor(r.key),
  })),
);

const dateText = new Date().toLocaleDateString(undefined, {
  weekday: 'long',
  month: 'long',
  day: 'numeric',
});
</script>

<div class="flex flex-col gap-5">
	<header>
		<h1 class="text-2xl font-semibold tracking-tight">Today</h1>
		<p class="text-sm text-muted">{dateText}</p>
	</header>

	<NowCard />

	{#if loadError}
		<p class="rounded-xl border border-bad/40 bg-bad/10 p-3 text-sm text-bad" role="alert">{loadError}</p>
	{/if}

	<div class="grid grid-cols-2 gap-4 lg:grid-cols-4">
		<StatTile label="Screen time" value={formatDuration(total)} hint={total === 0 ? "Nothing yet today" : undefined} />
		<StatTile
			label="Vs. yesterday"
			value={`${delta >= 0 ? "+" : "−"}${formatDuration(Math.abs(delta))}`}
			hint={`${formatDuration(yesterdaySoFar)} by this time`}
			tone={delta > 0 ? "bad" : delta < 0 ? "good" : "neutral"}
		/>
		<StatTile label="Productive" value={`${percent(productiveMs, total)}%`} hint={formatDuration(productiveMs)} />
		<StatTile
			label="Most used"
			value={topApp ? appLabel(topApp.key, store.appName(topApp.key)) : "—"}
			hint={topApp ? formatDuration(topApp.ms) : undefined}
		/>
	</div>

	<Card title="Timeline" subtitle="When you were active, coloured by category">
		<Timeline {sessions} {dayStart} />
	</Card>

	<div class="grid gap-5 lg:grid-cols-5">
		<Card title="By hour" class="lg:col-span-3">
			<BarChart labels={hourLabels} values={hours} highlight={new Date().getHours()} />
		</Card>
		<Card title="By category" class="lg:col-span-2">
			<div class="flex items-center gap-4">
				<Donut items={slices} centerTitle={formatShort(total)} centerSub="today" size={150} />
				<ul class="flex min-w-0 flex-1 flex-col gap-2 text-[13px]">
					{#each slices.slice(0, 6) as s (s.name)}
						<li class="flex items-center gap-2">
							<span class="size-2.5 shrink-0 rounded-full" style="background: {s.color}"></span>
							<span class="flex-1 truncate">{s.name}</span>
							<span class="text-xs text-muted tabular-nums">{formatDuration(s.value)}</span>
						</li>
					{/each}
				</ul>
			</div>
		</Card>
	</div>

	<div class="grid gap-5 lg:grid-cols-2">
		<Card title="Top apps">
			<UsageList rows={usage?.byApp ?? []} />
		</Card>
		<Card title="Top sites" subtitle="From the browser extension">
			<UsageList rows={usage?.web ?? []} kind="domain" empty="No sites yet. Install the browser extension to see per-site time." />
		</Card>
	</div>
</div>
