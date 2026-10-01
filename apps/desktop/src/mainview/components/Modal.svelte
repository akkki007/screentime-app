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
	class="fixed inset-0 z-40 grid place-items-center bg-black/40 p-4 backdrop-blur-[2px]"
	role="presentation"
	onclick={(e) => e.target === e.currentTarget && onclose()}
>
	<div
		class="w-full {width} rounded-2xl border border-line bg-surface p-6 shadow-2xl"
		role="dialog"
		aria-modal="true"
		aria-label={title}
	>
		<h2 class="mb-4 text-lg font-semibold">{title}</h2>
		{@render children()}
		{#if footer}
			<div class="mt-6 flex justify-end gap-2">{@render footer()}</div>
		{/if}
	</div>
</div>
