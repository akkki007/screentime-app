<script lang="ts">
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

// Plot-area insets: the y-axis labels live in the left gutter, the x labels below.
const LEFT = 44;
const RIGHT = 4;
const TOP = 8;
const BOTTOM = 22;

let width = $state(0);
let hover = $state(-1);
const hasData = $derived(values.some((v) => v > 0));

/** Y-axis tick spacing in whole minutes/hours rather than arbitrary decimals. */
function splits(max: number): number[] {
  const steps = [15, 30, 60, 120, 180, 240, 360, 480].map((m) => m * 60_000);
  const step = steps.find((s) => max / s <= 4) ?? steps[steps.length - 1] ?? 3_600_000;
  const out: number[] = [];
  for (let v = 0; v <= max; v += step) out.push(v);
  return out;
}

const n = $derived(labels.length);
const plotW = $derived(Math.max(0, width - LEFT - RIGHT));
const plotH = $derived(height - TOP - BOTTOM);
const top = $derived(Math.max(...values, 15 * 60_000) * 1.12);
const ticks = $derived(splits(top));
// The x scale spans [-0.6, n - 0.4], so each bar sits in a slot of plotW / n.
const slot = $derived(plotW / Math.max(n, 1));
const barW = $derived(Math.min(slot * 0.62, 48));
const cx = (i: number) => LEFT + (i + 0.6) * slot;
const y = (v: number) => TOP + plotH * (1 - v / top);

function move(e: MouseEvent) {
  const box = (e.currentTarget as SVGElement).getBoundingClientRect();
  const i = Math.round(((e.clientX - box.left - LEFT) / plotW) * n - 0.6);
  hover = i >= 0 && i < n && values[i] ? i : -1;
}
</script>

{#if hasData}
	<div class="relative" style="height: {height}px" bind:clientWidth={width}>
		<svg
			{width}
			{height}
			role="img"
			aria-label="Usage chart"
			style="font: 11px var(--font-sans)"
			onmousemove={move}
			onmouseleave={() => (hover = -1)}
		>
			{#each ticks as t}
				<line x1={LEFT} x2={LEFT + plotW} y1={y(t)} y2={y(t)} stroke="var(--line)" />
				<text x={LEFT - 6} y={y(t)} text-anchor="end" dominant-baseline="middle" fill="var(--muted)">{formatShort(t)}</text>
			{/each}
			{#each values as v, i}
				{#if v > 0}
					<rect
						x={cx(i) - barW / 2}
						y={y(v)}
						width={barW}
						height={y(0) - y(v)}
						rx={barW * 0.28}
						fill="var(--accent)"
						fill-opacity={i === highlight ? 1 : 0.6}
					/>
				{/if}
			{/each}
			{#each labels as l, i}
				{#if l}
					<text x={cx(i)} y={height - 6} text-anchor="middle" fill="var(--muted)">{l}</text>
				{/if}
			{/each}
		</svg>
		{#if hover >= 0}
			<div
				class="pointer-events-none absolute z-10 -translate-x-1/2 -translate-y-full rounded-md bg-ink px-2 py-1 text-[11px] whitespace-nowrap text-bg shadow"
				style="left: {cx(hover)}px; top: {y(values[hover] as number) - 4}px"
			>
				{labels[hover]} · {formatDuration(values[hover] as number)}
			</div>
		{/if}
	</div>
{:else}
	<div class="grid place-items-center text-sm text-muted" style="height: {height}px">{empty}</div>
{/if}
