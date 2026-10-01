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
let loadedAt = $state(Date.now());

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
    loadedAt = Date.now();
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

let nowTs = $state(Date.now());
$effect(() => {
  const id = setInterval(() => {
    nowTs = Date.now();
  }, 1000);
  return () => clearInterval(id);
});

/** Today's total as H:MM:SS, ticking while a window is being tracked. */
const clock = $derived.by(() => {
  const live =
    store.status?.currentAppId && !store.status.paused && !store.status.idle ? nowTs - loadedAt : 0;
  const secs = Math.floor((total + Math.max(0, live)) / 1000);
  const p = (n: number) => String(n).padStart(2, '0');
  return {
    h: String(Math.floor(secs / 3600)),
    m: p(Math.floor((secs % 3600) / 60)),
    s: p(secs % 60),
  };
});

const weekdayDay = new Date().toLocaleDateString(undefined, {
  weekday: 'short',
  month: 'short',
  day: 'numeric',
});
const weekNumber = (() => {
  const d = new Date();
  const jan1 = new Date(d.getFullYear(), 0, 1);
  return Math.ceil(((d.getTime() - jan1.getTime()) / 86400000 + jan1.getDay() + 1) / 7);
})();

const dateText = new Date().toLocaleDateString(undefined, {
  weekday: 'long',
  month: 'long',
  day: 'numeric',
});
</script>

<div class="flex flex-col gap-6">
  <section class="grid items-end gap-6 lg:grid-cols-2">
    <div>
      <h1 class="text-[40px] leading-[1.05] font-normal tracking-tight">
        Keep track of<br /><span class="text-muted/70">where your time goes</span>
      </h1>
      <p class="mt-3 text-sm text-muted">{dateText}</p>
    </div>
    <div class="text-right" aria-label="Screen time today">
      <p class="text-[13px] text-muted">Screen time today</p>
      <p class="text-[88px] leading-none font-extralight tracking-tighter tabular-nums">
        {clock.h}<span class="text-muted/60">:</span>{clock.m}<span class="text-muted/50">:{clock.s}</span>
      </p>
    </div>
  </section>

  {#if loadError}
    <p class="rounded-2xl bg-bad/10 p-3 text-sm text-bad" role="alert">{loadError}</p>
  {/if}

  <div class="grid gap-5 lg:grid-cols-5">
    <div class="flex flex-col gap-5 lg:col-span-2">
      <NowCard />
      <div class="grid grid-cols-2 gap-5">
        <StatTile
          label="Vs. yesterday"
          value={`${delta >= 0 ? '+' : '−'}${formatShort(Math.abs(delta))}`}
          hint={`${formatShort(yesterdaySoFar)} by this time`}
          tone={delta > 0 ? 'bad' : delta < 0 ? 'good' : 'neutral'}
        />
        <StatTile
          label="Most used"
          value={topApp ? appLabel(topApp.key, store.appName(topApp.key)) : '—'}
          hint={topApp ? formatDuration(topApp.ms) : undefined}
        />
      </div>
      <div class="grid grid-cols-2 gap-5">
        <StatTile look="orange" label="Today" value={weekdayDay} hint={`Week ${weekNumber}`} />
        <StatTile look="sun" label="Productive" value={`${percent(productiveMs, total)}%`} hint={formatDuration(productiveMs)} />
      </div>
    </div>

    <div class="flex flex-col gap-5 lg:col-span-3">
      <Card title="By hour">
        <BarChart labels={hourLabels} values={hours} highlight={new Date().getHours()} height={200} />
      </Card>
      <Card title="By category">
        <div class="flex items-center gap-6">
          <Donut items={slices} centerTitle={formatShort(total)} centerSub="today" size={160} />
          <ul class="grid min-w-0 flex-1 grid-cols-1 gap-2 text-[13px] sm:grid-cols-2">
            {#each slices.slice(0, 6) as s (s.name)}
              <li class="flex items-center gap-2">
                <span class="size-2.5 shrink-0 rounded-full" style="background: {s.color}"></span>
                <span class="flex-1 truncate">{s.name}</span>
                <span class="rounded-full bg-accent/20 px-2 py-0.5 text-[11px] font-medium tabular-nums">{formatShort(s.value)}</span>
              </li>
            {/each}
          </ul>
        </div>
      </Card>
    </div>
  </div>

  <Card title="Timeline" subtitle="When you were active, coloured by category">
    <Timeline {sessions} {dayStart} />
  </Card>

  <div class="grid gap-5 lg:grid-cols-2">
    <Card title="Top apps">
      <UsageList rows={usage?.byApp ?? []} />
    </Card>
    <Card title="Top sites" subtitle="From the browser extension">
      <UsageList rows={usage?.web ?? []} kind="domain" empty="No sites yet. Install the browser extension to see per-site time." />
    </Card>
  </div>
</div>
