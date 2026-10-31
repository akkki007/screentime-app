<script lang="ts">
import type { Snippet } from 'svelte';

const {
  title,
  subtitle,
  actions,
  footer,
  children,
  flush = false,
  class: extra = '',
}: {
  /** A small muted label above the content, like "Records" or "Tools". */
  title?: string;
  subtitle?: string;
  actions?: Snippet;
  /** A grey strip along the bottom, for buttons or a note. */
  footer?: Snippet;
  children: Snippet;
  /** No padding around the content, for lists that run edge to edge. */
  flush?: boolean;
  class?: string;
} = $props();
</script>

<section class="flex flex-col overflow-hidden rounded-2xl border border-line bg-surface shadow-[var(--shadow)] {extra}">
	<div class="flex-1 {flush ? '' : 'p-5'}">
		{#if title || actions}
			<header class="flex items-start justify-between gap-3 {flush ? 'px-5 pt-5' : ''} mb-3">
				<div class="min-w-0">
					{#if title}<h2 class="font-sans text-[13px] font-normal text-muted">{title}</h2>{/if}
					{#if subtitle}<p class="mt-0.5 text-xs text-muted/80">{subtitle}</p>{/if}
				</div>
				{@render actions?.()}
			</header>
		{/if}
		{@render children()}
	</div>
	{#if footer}
		<footer class="flex flex-wrap items-center gap-2 border-t border-line bg-surface-2/70 px-5 py-3">
			{@render footer()}
		</footer>
	{/if}
</section>
