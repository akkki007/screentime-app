<script lang="ts">
import { appLabel, formatDuration } from '../lib/format';
import { store } from '../lib/store.svelte';
import Button from './Button.svelte';
import Icon from './Icon.svelte';

const limit = $derived(store.limits.find((l) => l.id === store.limitOverlay?.limitId));
const target = $derived(
  limit
    ? limit.targetType === 'app'
      ? appLabel(limit.target, store.appName(limit.target))
      : limit.target
    : 'this limit',
);

function dismiss() {
  store.limitOverlay = undefined;
}
function manage() {
  store.view = 'limits';
  dismiss();
}
</script>

<svelte:window onkeydown={(e) => e.key === "Escape" && dismiss()} />

<div class="fixed inset-0 z-50 grid place-items-center bg-bg/95 p-6" role="alertdialog" aria-modal="true" aria-label="Daily limit reached">
	<div class="w-full max-w-md overflow-hidden rounded-2xl border border-line bg-surface text-center shadow-[var(--shadow-pop)]">
		<div class="p-8">
		<div class="mx-auto mb-5 grid size-12 place-items-center rounded-xl bg-bad/10 text-bad"><Icon name="limits" size={22} /></div>
		<h2 class="text-xl font-medium tracking-tight">Daily limit reached</h2>
		<p class="mt-2 text-[14px] leading-relaxed text-muted">
			You've used your {limit ? formatDuration(limit.dailyMs) : ""} for <span class="font-medium text-ink">{target}</span> today. Maybe it's a good moment to do something else.
		</p>
		<p class="mt-4 text-xs text-muted">Screentime only nudges. You can keep going, and this will come back in a few minutes.</p>
		</div>
		<div class="flex justify-center gap-2 border-t border-line bg-surface-2/70 px-6 py-3">
			<Button onclick={manage}>Manage limits</Button>
			<Button variant="primary" onclick={dismiss}>Got it</Button>
		</div>
	</div>
</div>
