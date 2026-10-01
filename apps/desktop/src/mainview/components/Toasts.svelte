<script lang="ts">
import { store } from '../lib/store.svelte';
import Icon from './Icon.svelte';

const tone = {
  info: 'border-accent/40',
  success: 'border-good/50',
  error: 'border-bad/60',
};
</script>

<div class="pointer-events-none fixed right-4 bottom-4 z-50 flex w-80 flex-col gap-2" aria-live="polite">
	{#each store.toasts as toast (toast.id)}
		<div
			class="pointer-events-auto flex items-start gap-2 rounded-xl border bg-surface p-3 text-[13px] shadow-lg {tone[toast.kind]}"
			role={toast.kind === "error" ? "alert" : "status"}
		>
			<span class="mt-0.5 shrink-0 {toast.kind === 'error' ? 'text-bad' : toast.kind === 'success' ? 'text-good' : 'text-accent'}">
				<Icon name={toast.kind === "error" ? "alert" : toast.kind === "success" ? "check" : "bolt"} size={16} />
			</span>
			<p class="flex-1 break-words">{toast.message}</p>
			<button class="text-muted hover:text-ink" aria-label="Dismiss" onclick={() => store.dismissToast(toast.id)}>
				<Icon name="x" size={14} />
			</button>
		</div>
	{/each}
</div>
