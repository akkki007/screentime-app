<script lang="ts">
const {
  label,
  value,
  hint,
  tone = 'neutral',
}: { label: string; value: string; hint?: string; tone?: 'neutral' | 'good' | 'bad' } = $props();
</script>

<div class="rounded-2xl border border-line bg-surface p-4 shadow-[var(--shadow)]">
	<p class="text-xs font-medium text-muted">{label}</p>
	<p class="mt-1.5 text-2xl font-semibold tracking-tight">{value}</p>
	{#if hint}
		<p class="mt-1 text-xs {tone === 'good' ? 'text-good' : tone === 'bad' ? 'text-bad' : 'text-muted'}">{hint}</p>
	{/if}
</div>
