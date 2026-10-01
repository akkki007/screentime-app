<script lang="ts">
import type { Snippet } from 'svelte';

const {
  title,
  subtitle,
  actions,
  children,
  class: extra = '',
}: {
  title?: string;
  subtitle?: string;
  actions?: Snippet;
  children: Snippet;
  class?: string;
} = $props();
</script>

<section class="rounded-2xl border border-line bg-surface p-5 shadow-[var(--shadow)] {extra}">
	{#if title || actions}
		<header class="mb-4 flex items-start justify-between gap-3">
			<div>
				{#if title}<h2 class="text-[15px] font-semibold">{title}</h2>{/if}
				{#if subtitle}<p class="mt-0.5 text-xs text-muted">{subtitle}</p>{/if}
			</div>
			{@render actions?.()}
		</header>
	{/if}
	{@render children()}
</section>
