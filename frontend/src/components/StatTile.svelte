<script lang="ts">
const {
  label,
  value,
  hint,
  tone = 'neutral',
}: {
  label: string;
  value: string;
  hint?: string;
  tone?: 'neutral' | 'good' | 'bad';
} = $props();

const hintTone = $derived(
  tone === 'good' ? 'text-good' : tone === 'bad' ? 'text-bad' : 'text-muted',
);
</script>

<div class="rounded-2xl border border-line bg-surface p-5 shadow-[var(--shadow)]">
  <p class="text-[13px] text-muted">{label}</p>
  <p class="mt-2 truncate font-display text-[26px] leading-tight font-medium tracking-tight tabular-nums">{value}</p>
  {#if hint}<p class="mt-1 text-xs {hintTone}">{hint}</p>{/if}
</div>
