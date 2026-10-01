<script lang="ts">
import { daemon, getBridge } from '../lib/api';
import { addDays, appLabel, dateKey, formatDuration, formatShort, startOfDay } from '../lib/format';
import { store } from '../lib/store.svelte';
import AppAvatar from './AppAvatar.svelte';
import Icon from './Icon.svelte';

let now = $state(Date.now());
$effect(() => {
  const id = setInterval(() => {
    now = Date.now();
  }, 1000);
  return () => clearInterval(id);
});

let days = $state<{ key: string; ms: number; letter: string }[]>([]);
let previousWeekMs = $state(0);
let yesterdayMs = $state(0);

async function load() {
  const today = startOfDay(Date.now());
  const from = addDays(today, -6);
  const [byDay, prior] = await Promise.all([
    daemon('usage.summary', { from, to: addDays(today, 1), groupBy: 'day' }),
    daemon('usage.summary', { from: addDays(from, -7), to: from, groupBy: 'day' }),
  ]);
  const byKey = new Map(byDay.map((r) => [r.key, r.ms]));
  days = Array.from({ length: 7 }, (_, i) => {
    const ts = addDays(from, i);
    return {
      key: dateKey(ts),
      ms: byKey.get(dateKey(ts)) ?? 0,
      letter: new Date(ts).toLocaleDateString(undefined, { weekday: 'narrow' }),
    };
  });
  previousWeekMs = prior.reduce((n, r) => n + r.ms, 0);
  yesterdayMs = byKey.get(dateKey(addDays(today, -1))) ?? 0;
}

$effect(() => {
  void store.tick;
  if (store.connected) void load().catch(() => {});
});

const status = $derived(store.status);
const todayMs = $derived(days.at(-1)?.ms ?? 0);
const weekMs = $derived(days.reduce((n, d) => n + d.ms, 0));
const activeDays = $derived(days.filter((d) => d.ms > 0).length);
const average = $derived(activeDays ? weekMs / activeDays : 0);
const weekChange = $derived(
  previousWeekMs > 0 ? Math.round(((weekMs - previousWeekMs) / previousWeekMs) * 100) : undefined,
);

// bar chart geometry (SVG units)
const W = 300;
const H = 84;
const max = $derived(Math.max(...days.map((d) => d.ms), average, 30 * 60_000));
const barW = 26;
const gap = $derived((W - 7 * barW) / 6);
const y = (ms: number) => H - (ms / max) * (H - 6);

const clock = $derived(
  new Date(now).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false }),
);
const dateText = $derived(
  new Date(now).toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric' }),
);

const running = $derived(Boolean(status?.currentAppId) && !status?.paused && !status?.idle);
const tracking = $derived(running && status?.since ? formatDuration(now - status.since) : '');

async function togglePause() {
  if (status?.paused) await store.attempt(() => daemon('tracker.resume'));
  else await store.attempt(() => daemon('tracker.pause', { minutes: 15 }));
}
async function toggleFocus() {
  if (status?.focusMode.active) await store.attempt(() => daemon('focus.stop'));
  else await store.attempt(() => daemon('focus.start', { minutes: 25 }));
}
const openDashboard = () => getBridge().openDashboard();
const quit = () => getBridge().quitApp();
</script>

{#snippet circle(label: string, active: boolean, onclick: () => void, icon: 'today' | 'pause' | 'play' | 'bolt')}
  <button
    class="grid size-12 place-items-center rounded-full transition {active ? 'bg-accent text-[#1a0d03]' : 'bg-white/12 text-on-dark hover:bg-white/20'}"
    aria-label={label}
    title={label}
    {onclick}
  >
    <Icon name={icon} size={22} />
  </button>
{/snippet}

<main class="flex h-full flex-col gap-3 p-3 text-on-dark">
  <!-- header panel -->
  <section class="rounded-[32px] bg-[#2a231e]/95 p-4 shadow-2xl ring-1 ring-white/10">
    <div class="flex items-start justify-between">
      <div>
        <p class="text-[13px] text-white/55">{dateText}</p>
        <p class="text-[40px] leading-none font-light tracking-tight tabular-nums">{clock}</p>
      </div>
      <span class="flex items-center gap-2 rounded-full bg-white/10 px-3 py-1.5 text-xs">
        <span class="size-2 rounded-full {!store.connected ? 'bg-bad' : status?.paused ? 'bg-warn' : status?.idle ? 'bg-white/40' : 'bg-good animate-pulse'}"></span>
        {!store.connected ? 'Offline' : status?.paused ? 'Paused' : status?.idle ? 'Idle' : 'Tracking'}
      </span>
    </div>

    <div class="mt-4 flex items-center justify-between px-2">
      {@render circle('Open dashboard', false, openDashboard, 'today')}
      {@render circle(status?.paused ? 'Resume tracking' : 'Pause for 15 minutes', Boolean(status?.paused), togglePause, status?.paused ? 'play' : 'pause')}
      {@render circle(status?.focusMode.active ? 'Stop focus mode' : 'Start 25-minute focus', Boolean(status?.focusMode.active), toggleFocus, 'bolt')}
    </div>

    {#if running && status?.currentAppId}
      <div class="mt-4 flex items-center gap-3 rounded-2xl bg-black/25 p-2.5">
        <AppAvatar appId={status.currentAppId} size={36} />
        <div class="min-w-0 flex-1">
          <p class="truncate text-[14px]">{appLabel(status.currentAppId, store.appName(status.currentAppId))}</p>
          <p class="text-xs text-white/55">For {tracking}</p>
        </div>
      </div>
    {/if}
  </section>

  <!-- activity panel -->
  <section class="flex min-h-0 flex-1 flex-col rounded-[32px] bg-[#2a231e]/95 p-4 shadow-2xl ring-1 ring-white/10">
    <h2 class="text-center text-base font-medium">Screen time</h2>

    <div class="mt-3 rounded-3xl bg-black/30 p-3.5">
      <p class="text-[13px] text-white/55">Today</p>
      <div class="flex items-baseline gap-3">
        <p class="text-[30px] leading-tight font-light tracking-tight">{formatShort(todayMs)}</p>
        {#if todayMs > 0 || yesterdayMs > 0}
          <span class="rounded-full bg-white/10 px-2.5 py-1 text-[11px] text-white/70">
            {todayMs >= yesterdayMs ? '↑' : '↓'} {formatShort(Math.abs(todayMs - yesterdayMs))} vs yesterday
          </span>
        {/if}
      </div>

      <svg viewBox="0 0 {W} {H + 18}" class="mt-3 w-full" role="img" aria-label="Screen time for the last 7 days">
        {#each [0.5, 1] as f}
          <line x1="0" x2={W} y1={y(max * f)} y2={y(max * f)} stroke="white" stroke-opacity="0.07" />
        {/each}
        {#each days as d, i (d.key)}
          {@const x = i * (barW + gap)}
          <rect {x} y={y(d.ms)} width={barW} height={Math.max(H - y(d.ms), d.ms > 0 ? 3 : 0)} rx="7"
            fill={i === days.length - 1 ? 'var(--accent)' : 'var(--accent)'} opacity={i === days.length - 1 ? 1 : 0.55}>
            <title>{d.key}: {formatDuration(d.ms)}</title>
          </rect>
          <text x={x + barW / 2} y={H + 14} text-anchor="middle" font-size="11" fill="white" fill-opacity="0.5">{d.letter}</text>
        {/each}
        {#if average > 0}
          <line x1="0" x2={W} y1={y(average)} y2={y(average)} stroke="var(--sun)" stroke-width="1.5" stroke-dasharray="4 4" />
        {/if}
      </svg>

      <p class="mt-1 flex items-center justify-between text-xs text-white/55">
        <span><span class="mr-1.5 inline-block h-0.5 w-3 bg-sun align-middle"></span>Daily average {formatShort(average)}</span>
        {#if weekChange !== undefined}<span>{weekChange >= 0 ? '↑' : '↓'} {Math.abs(weekChange)}% from last week</span>{/if}
      </p>
    </div>

    <button class="mt-2 flex items-center gap-3 rounded-2xl px-2 py-2 text-left text-[14px] hover:bg-white/8" onclick={openDashboard}>
      <span class="grid size-9 place-items-center rounded-full bg-white/12"><Icon name="apps" size={18} /></span>
      <span class="flex-1">See all activity</span>
      <span class="text-white/50">›</span>
    </button>

    <button class="mt-auto pt-1 text-center text-[13px] text-white/60 hover:text-white" onclick={quit}>Exit Screentime</button>
  </section>
</main>
