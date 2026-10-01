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

<div class="fixed inset-0 z-50 grid place-items-center bg-bg/92 p-6 backdrop-blur-md" role="alertdialog" aria-modal="true" aria-label="Daily limit reached">
	<div class="max-w-md text-center">
		<div class="mx-auto mb-5 grid size-16 place-items-center rounded-2xl bg-bad/15 text-bad"><Icon name="limits" size={30} /></div>
		<h2 class="text-2xl font-semibold tracking-tight">Daily limit reached</h2>
		<p class="mt-2 text-[15px] text-muted">
			You've used your {limit ? formatDuration(limit.dailyMs) : ""} for <b class="text-ink">{target}</b> today. Maybe it's a good moment to do something else.
		</p>
		<div class="mt-6 flex justify-center gap-2">
			<Button variant="primary" onclick={dismiss}>Got it</Button>
			<Button onclick={manage}>Manage limits</Button>
		</div>
		<p class="mt-4 text-xs text-muted">Screentime only nudges. You can keep going, and this will come back in a few minutes.</p>
	</div>
</div>
