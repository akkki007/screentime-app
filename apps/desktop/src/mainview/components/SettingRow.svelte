<script lang="ts">
import type { Snippet } from 'svelte';

const { title, description, children }: { title: string; description?: string; children: Snippet } =
  $props();
</script>

<div class="flex items-center justify-between gap-6 border-t border-line py-4 first:border-t-0 first:pt-0 last:pb-0">
	<div class="min-w-0">
		<p class="text-[13px] font-medium">{title}</p>
		{#if description}<p class="mt-0.5 max-w-xl text-xs leading-relaxed text-muted">{description}</p>{/if}
	</div>
	<div class="shrink-0">{@render children()}</div>
</div>
