<script lang="ts">
import BarChart from '../components/BarChart.svelte';
import Card from '../components/Card.svelte';
import Donut from '../components/Donut.svelte';
import StatTile from '../components/StatTile.svelte';
import UsageList from '../components/UsageList.svelte';
import {
  addDays,
  dayMonth,
  formatDuration,
  formatShort,
  percent,
  startOfDay,
  weekdayShort,
} from '../lib/format';
import { store } from '../lib/store.svelte';
import { type RangeUsage, fillDays, latestOnly, loadRange, loadTotal } from '../lib/usage';

const RANGES = [7, 14, 30] as const;
let days = $state<(typeof RANGES)[number]>(7);
let usage = $state<RangeUsage>();
let previousTotal = $state(0);
let start = $state(startOfDay(Date.now()));
let loadError = $state<string>();

const guard = latestOnly();

async function load(range: number) {
  const ticket = guard.next();
  const today = startOfDay(Date.now());
  const from = addDays(today, -(range - 1));
  try {
    const [current, previous] = await Promise.all([
      loadRange(from, addDays(today, 1)),
      loadTotal(addDays(from, -range), from),
    ]);
    if (!guard.isCurrent(ticket)) return;
    usage = current;
    previousTotal = previous;
    start = from;
    loadError = undefined;
  } catch (err) {
    if (guard.isCurrent(ticket)) loadError = err instanceof Error ? err.message : String(err);
  }
}

$effect(() => {
  const range = days;
  void store.tick;
  if (store.connected) void load(range);
});

const perDay = $derived(fillDays(start, days, usage?.byDay ?? []));
const activeDays = $derived(perDay.filter((d) => d.ms > 0).length);
const total = $derived(usage?.total ?? 0);
const average = $derived(activeDays ? total / activeDays : 0);
const change = $derived(
  previousTotal > 0 ? Math.round(((total - previousTotal) / previousTotal) * 100) : undefined,
);
const busiest = $derived([...perDay].sort((a, b) => b.ms - a.ms)[0]);
const labels = $derived(
  perDay.map((d, i) =>
    days <= 7
      ? weekdayShort(d.key)
      : days <= 14
        ? weekdayShort(d.key).charAt(0)
        : i % 5 === 0
          ? dayMonth(d.key)
          : '',
  ),
);
const slices = $derived(
  (usage?.byCategory ?? []).map((r) => ({
    name: r.key,
    value: r.ms,
    color: store.categoryColor(r.key),
  })),
);
</script>

<div class="flex flex-col gap-5">
	<header class="flex items-end justify-between gap-4">
		<div>
			<h1 class="text-2xl font-semibold tracking-tight">Trends</h1>
			<p class="text-sm text-muted">{dayMonth(perDay[0]?.key ?? "2000-01-01")} – {dayMonth(perDay.at(-1)?.key ?? "2000-01-01")}</p>
		</div>
		<div class="inline-flex rounded-lg border border-line bg-surface p-0.5" role="group" aria-label="Range">
			{#each RANGES as r}
				<button
					class="rounded-md px-3 py-1 text-[13px] font-medium transition {days === r ? 'bg-accent text-white' : 'text-muted hover:text-ink'}"
					aria-pressed={days === r}
					onclick={() => (days = r)}
				>
					{r} days
				</button>
			{/each}
		</div>
	</header>

	{#if loadError}
		<p class="rounded-xl border border-bad/40 bg-bad/10 p-3 text-sm text-bad" role="alert">{loadError}</p>
	{/if}

	<div class="grid grid-cols-2 gap-4 lg:grid-cols-4">
		<StatTile label="Total" value={formatDuration(total)} />
		<StatTile label="Daily average" value={formatDuration(average)} hint={`over ${activeDays} active day${activeDays === 1 ? "" : "s"}`} />
		<StatTile
			label={`Vs. previous ${days} days`}
			value={change === undefined ? "—" : `${change >= 0 ? "+" : "−"}${Math.abs(change)}%`}
			hint={previousTotal ? formatDuration(previousTotal) : "No earlier data"}
			tone={change === undefined ? "neutral" : change > 0 ? "bad" : change < 0 ? "good" : "neutral"}
		/>
		<StatTile label="Busiest day" value={busiest && busiest.ms > 0 ? dayMonth(busiest.key) : "—"} hint={busiest && busiest.ms > 0 ? formatDuration(busiest.ms) : undefined} />
	</div>

	<Card title="Daily screen time">
		<BarChart {labels} values={perDay.map((d) => d.ms)} highlight={perDay.length - 1} height={230} />
	</Card>

	<div class="grid gap-5 lg:grid-cols-5">
		<Card title="By category" class="lg:col-span-2">
			<div class="flex items-center gap-4">
				<Donut items={slices} centerTitle={formatShort(total)} centerSub={`${days} days`} size={150} />
				<ul class="flex min-w-0 flex-1 flex-col gap-2 text-[13px]">
					{#each slices.slice(0, 6) as s (s.name)}
						<li class="flex items-center gap-2">
							<span class="size-2.5 shrink-0 rounded-full" style="background: {s.color}"></span>
							<span class="flex-1 truncate">{s.name}</span>
							<span class="text-xs text-muted tabular-nums">{percent(s.value, total)}%</span>
						</li>
					{/each}
				</ul>
			</div>
		</Card>
		<Card title="Top apps" class="lg:col-span-3">
			<UsageList rows={usage?.byApp ?? []} limit={7} />
		</Card>
	</div>

	<Card title="Top sites">
		<UsageList rows={usage?.web ?? []} kind="domain" limit={6} empty="No sites yet. Install the browser extension to see per-site time." />
	</Card>
</div>
