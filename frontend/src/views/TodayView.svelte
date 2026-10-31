<script lang="ts">
import type { Session } from '@screentime/shared';
import AppAvatar from '../components/AppAvatar.svelte';
import BarChart from '../components/BarChart.svelte';
import Card from '../components/Card.svelte';
import DetailRow from '../components/DetailRow.svelte';
import Donut from '../components/Donut.svelte';
import NowCard from '../components/NowCard.svelte';
import PlannerSlot from '../components/PlannerSlot.svelte';
import Timeline from '../components/Timeline.svelte';
import UsageList from '../components/UsageList.svelte';
import { addDays, appLabel, dateKey, formatShort, percent, startOfDay } from '../lib/format';
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

const dateText = new Date().toLocaleDateString(undefined, {
  weekday: 'long',
  month: 'long',
  day: 'numeric',
});
</script>

<div class="flex flex-col gap-5">
  <header class="flex items-end justify-between gap-4">
    <div>
      <h1 class="text-2xl font-medium tracking-tight">Today</h1>
      <p class="mt-1 text-[13px] text-muted">{dateText}</p>
    </div>
  </header>

  <PlannerSlot load={() => import('../components/PlanCard.svelte')} />

  {#if loadError}
    <p class="rounded-xl border border-bad/30 bg-bad/5 p-3 text-[13px] text-bad" role="alert">{loadError}</p>
  {/if}

  <div class="grid gap-5 lg:grid-cols-5">
    <div class="flex flex-col gap-5 lg:col-span-2">
      <Card title="Screen time today">
        <p class="font-display text-[44px] leading-none font-medium tracking-tight tabular-nums" aria-label="Screen time today">
          {clock.h}<span class="text-muted/50">:</span>{clock.m}<span class="text-muted/50">:{clock.s}</span>
        </p>
        <div class="mt-4">
          <DetailRow label="Vs. yesterday" dashed>
            <span class={delta > 0 ? 'text-bad' : delta < 0 ? 'text-good' : ''}>{delta >= 0 ? '+' : '−'}{formatShort(Math.abs(delta))}</span>
            <span class="font-normal text-muted">({formatShort(yesterdaySoFar)} by now)</span>
          </DetailRow>
          <DetailRow label="Most used" dashed>
            {#if topApp}
              <AppAvatar appId={topApp.key} size={20} />
              <span class="truncate">{appLabel(topApp.key, store.appName(topApp.key))}</span>
            {:else}
              <span class="text-muted">—</span>
            {/if}
          </DetailRow>
          <DetailRow label="Productive" dashed>
            <span class="size-2 rounded-full bg-good"></span>
            {percent(productiveMs, total)}%
            <span class="font-normal text-muted">({formatShort(productiveMs)})</span>
          </DetailRow>
        </div>
      </Card>
      <NowCard />
    </div>

    <div class="flex flex-col gap-5 lg:col-span-3">
      <Card title="By hour">
        <BarChart labels={hourLabels} values={hours} highlight={new Date().getHours()} height={190} />
      </Card>
      <Card title="By category">
        <div class="flex items-center gap-6">
          <Donut items={slices} centerTitle={formatShort(total)} centerSub="today" size={140} />
          <ul class="flex min-w-0 flex-1 flex-col">
            {#each slices.slice(0, 6) as s (s.name)}
              <li class="flex items-center gap-2.5 py-1.5 text-[13px]">
                <span class="size-2 shrink-0 rounded-full" style="background: {s.color}"></span>
                <span class="flex-1 truncate">{s.name}</span>
                <span class="text-muted tabular-nums">{formatShort(s.value)}</span>
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
