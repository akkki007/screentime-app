<script lang="ts">
import type { Snippet } from 'svelte';

/** A label on the left and its value on the right, like "Plan … Superstar". */
const {
  label,
  children,
  dashed = false,
}: { label: string; children: Snippet; dashed?: boolean } = $props();
</script>

<div class="flex min-h-11 items-center justify-between gap-4 py-2 {dashed ? 'border-t border-dashed border-line-dashed first:border-t-0' : ''}">
	<span class="shrink-0 text-[13px] text-muted">{label}</span>
	<span class="flex min-w-0 items-center justify-end gap-2 text-right text-[13px] font-medium tabular-nums">{@render children()}</span>
</div>
