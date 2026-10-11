<script lang="ts">
import uPlot from 'uplot';
import 'uplot/dist/uPlot.min.css';
import { formatDuration, formatShort } from '../lib/format';

const {
  labels,
  values,
  highlight = -1,
  height = 190,
  empty = 'No activity recorded yet.',
}: {
  labels: string[];
  values: number[];
  /** Index drawn in the accent colour (e.g. today, current hour). */
  highlight?: number;
  height?: number;
  empty?: string;
} = $props();

let host: HTMLDivElement | undefined = $state();
let tip = $state<{ x: number; y: number; text: string } | undefined>();
const hasData = $derived(values.some((v) => v > 0));

const css = (name: string) =>
  getComputedStyle(document.documentElement).getPropertyValue(name).trim();

/** Y-axis tick spacing in whole minutes/hours rather than arbitrary decimals. */
function splits(max: number): number[] {
  const steps = [15, 30, 60, 120, 180, 240, 360, 480].map((m) => m * 60_000);
  const step = steps.find((s) => max / s <= 4) ?? steps[steps.length - 1] ?? 3_600_000;
  const out: number[] = [];
  for (let v = 0; v <= max + step; v += step) out.push(v);
  return out;
}

function build(el: HTMLDivElement, dark: boolean): uPlot {
  void dark; // only here so a theme change re-runs the effect
  const xs = labels.map((_, i) => i);
  const normal = values.map((v, i) => (i === highlight ? null : v));
  const hot = values.map((v, i) => (i === highlight ? v : null));
  const max = Math.max(...values, 15 * 60_000);
  const bars = uPlot.paths.bars?.({ size: [0.62, 48], radius: 0.28 });

  const axisCommon = {
    stroke: css('--muted'),
    grid: { stroke: css('--line'), width: 1 },
    ticks: { show: false },
    font: '11px system-ui, sans-serif',
  };

  const plot = new uPlot(
    {
      width: el.clientWidth,
      height,
      padding: [8, 4, 0, 0],
      legend: { show: false },
      cursor: { x: false, y: false, drag: { x: false, y: false }, points: { show: false } },
      scales: {
        x: { time: false, range: [-0.6, labels.length - 0.4] },
        y: { range: [0, max * 1.12] },
      },
      axes: [
        {
          ...axisCommon,
          grid: { show: false },
          values: (_u, ticks) => ticks.map((t) => (Number.isInteger(t) ? (labels[t] ?? '') : '')),
          splits: () => xs,
          space: 18,
        },
        {
          ...axisCommon,
          size: 44,
          values: (_u, ticks) => ticks.map((t) => formatShort(t)),
          splits: () => splits(max * 1.12),
        },
      ],
      series: [
        {},
        {
          fill: `${css('--accent')}99`,
          stroke: `${css('--accent')}99`,
          paths: bars,
          width: 0,
          points: { show: false },
        },
        {
          fill: css('--accent'),
          stroke: css('--accent'),
          paths: bars,
          width: 0,
          points: { show: false },
        },
      ],
      hooks: {
        setCursor: [
          (u) => {
            const idx = u.posToIdx(u.cursor.left ?? -1);
            const inside = (u.cursor.left ?? -1) >= 0 && idx >= 0 && idx < labels.length;
            if (!inside || !values[idx]) {
              tip = undefined;
              return;
            }
            tip = {
              x: u.valToPos(idx, 'x'),
              y: u.valToPos(values[idx] as number, 'y'),
              text: `${labels[idx]} · ${formatDuration(values[idx] as number)}`,
            };
          },
        ],
      },
    },
    [xs, normal, hot] as uPlot.AlignedData,
    el,
  );
  return plot;
}

let dark = $state(matchMedia('(prefers-color-scheme: dark)').matches);
$effect(() => {
  const mq = matchMedia('(prefers-color-scheme: dark)');
  const on = () => {
    dark = mq.matches;
  };
  mq.addEventListener('change', on);
  return () => mq.removeEventListener('change', on);
});

$effect(() => {
  if (!host || !hasData) return;
  const plot = build(host, dark);
  const ro = new ResizeObserver(() => host && plot.setSize({ width: host.clientWidth, height }));
  ro.observe(host);
  return () => {
    ro.disconnect();
    plot.destroy();
    tip = undefined;
  };
});
</script>

{#if hasData}
	<div class="relative" style="height: {height}px">
		<div bind:this={host} class="h-full w-full"></div>
		{#if tip}
			<div
				class="pointer-events-none absolute z-10 -translate-x-1/2 -translate-y-full rounded-md bg-ink px-2 py-1 text-[11px] whitespace-nowrap text-bg shadow"
				style="left: {tip.x + 44}px; top: {tip.y - 4}px"
			>
				{tip.text}
			</div>
		{/if}
	</div>
{:else}
	<div class="grid place-items-center text-sm text-muted" style="height: {height}px">{empty}</div>
{/if}
