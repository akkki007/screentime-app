<script lang="ts">
import type { Snippet } from 'svelte';

const {
  title,
  onclose,
  children,
  footer,
  width = 'max-w-md',
}: {
  title: string;
  onclose: () => void;
  children: Snippet;
  footer?: Snippet;
  width?: string;
} = $props();

function onkeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') onclose();
}
</script>

<svelte:window {onkeydown} />

<div
	class="fixed inset-0 z-40 grid place-items-center bg-black/30 p-4"
	role="presentation"
	onclick={(e) => e.target === e.currentTarget && onclose()}
>
	<div
		class="w-full {width} overflow-hidden rounded-2xl border border-line bg-surface-raised shadow-[var(--shadow-pop)]"
		role="dialog"
		aria-modal="true"
		aria-label={title}
	>
		<div class="p-6">
			<h2 class="mb-4 text-[17px] font-medium tracking-tight">{title}</h2>
			{@render children()}
		</div>
		{#if footer}
			<div class="flex justify-end gap-2 border-t border-line bg-surface-2/70 px-6 py-3">{@render footer()}</div>
		{/if}
	</div>
</div>
