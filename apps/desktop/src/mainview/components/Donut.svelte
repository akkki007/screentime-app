<script lang="ts">
import { PieChart } from 'echarts/charts';
import { TitleComponent, TooltipComponent } from 'echarts/components';
import * as echarts from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import { formatDuration } from '../lib/format';

echarts.use([PieChart, TooltipComponent, TitleComponent, CanvasRenderer]);

export type Slice = { name: string; value: number; color: string };

const {
  items,
  centerTitle,
  centerSub,
  size = 190,
}: { items: Slice[]; centerTitle: string; centerSub: string; size?: number } = $props();

let host: HTMLDivElement | undefined = $state();
let dark = $state(matchMedia('(prefers-color-scheme: dark)').matches);

const css = (name: string) =>
  getComputedStyle(document.documentElement).getPropertyValue(name).trim();

$effect(() => {
  const mq = matchMedia('(prefers-color-scheme: dark)');
  const on = () => {
    dark = mq.matches;
  };
  mq.addEventListener('change', on);
  return () => mq.removeEventListener('change', on);
});

$effect(() => {
  if (!host) return;
  void dark;
  const chart = echarts.init(host, undefined, { renderer: 'canvas' });
  chart.setOption({
    animationDuration: 400,
    tooltip: {
      trigger: 'item',
      backgroundColor: css('--surface'),
      borderColor: css('--line'),
      textStyle: { color: css('--text'), fontSize: 12 },
      formatter: (p: { name: string; value: number; percent: number }) =>
        `${p.name}<br/><b>${formatDuration(p.value)}</b> · ${Math.round(p.percent)}%`,
    },
    title: {
      text: centerTitle,
      subtext: centerSub,
      left: 'center',
      top: 'center',
      itemGap: 2,
      textStyle: { color: css('--text'), fontSize: 17, fontWeight: 600 },
      subtextStyle: { color: css('--muted'), fontSize: 11 },
    },
    series: [
      {
        type: 'pie',
        radius: ['64%', '90%'],
        avoidLabelOverlap: true,
        label: { show: false },
        emphasis: { scaleSize: 4 },
        itemStyle: { borderRadius: 5, borderColor: css('--surface'), borderWidth: 3 },
        data: items.map((i) => ({ name: i.name, value: i.value, itemStyle: { color: i.color } })),
      },
    ],
  });
  const ro = new ResizeObserver(() => chart.resize());
  ro.observe(host);
  return () => {
    ro.disconnect();
    chart.dispose();
  };
});
</script>

{#if items.length}
	<div bind:this={host} style="width: {size}px; height: {size}px" role="img" aria-label="Time by category"></div>
{:else}
	<div class="grid place-items-center text-sm text-muted" style="width: {size}px; height: {size}px">No data</div>
{/if}
