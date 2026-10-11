<script lang="ts">
import { formatDuration, percent } from '../lib/format';

type Slice = { name: string; value: number; color: string };

const {
  items,
  centerTitle,
  centerSub,
  size = 190,
}: { items: Slice[]; centerTitle: string; centerSub: string; size?: number } = $props();

// A ring drawn as stroked circle arcs: one dash per slice, a small gap between.
const RADIUS = 42;
const CIRCUMFERENCE = 2 * Math.PI * RADIUS;
const GAP = 1.6;

const total = $derived(items.reduce((n, i) => n + i.value, 0));
const arcs = $derived.by(() => {
  let offset = 0;
  return items.map((item) => {
    const length = total > 0 ? (item.value / total) * CIRCUMFERENCE : 0;
    const arc = {
      ...item,
      dash: `${Math.max(length - (items.length > 1 ? GAP : 0), 0)} ${CIRCUMFERENCE}`,
      offset: -offset,
      label: `${item.name}: ${formatDuration(item.value)} (${percent(item.value, total)}%)`,
    };
    offset += length;
    return arc;
  });
});
</script>

{#if items.length}
  <div class="relative shrink-0" style="width: {size}px; height: {size}px" role="img" aria-label="Time by category">
    <svg viewBox="0 0 100 100" width={size} height={size} class="-rotate-90">
      {#each arcs as arc (arc.name)}
        <circle
          cx="50"
          cy="50"
          r={RADIUS}
          fill="none"
          stroke={arc.color}
          stroke-width="11"
          stroke-dasharray={arc.dash}
          stroke-dashoffset={arc.offset}
          class="transition-opacity hover:opacity-75"
        >
          <title>{arc.label}</title>
        </circle>
      {/each}
    </svg>
    <div class="pointer-events-none absolute inset-0 grid place-content-center text-center">
      <p class="text-[17px] leading-tight font-medium">{centerTitle}</p>
      <p class="text-[11px] text-muted">{centerSub}</p>
    </div>
  </div>
{:else}
  <div class="grid place-items-center text-sm text-muted" style="width: {size}px; height: {size}px">No data</div>
{/if}
