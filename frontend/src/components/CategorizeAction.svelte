<script lang="ts">
/**
 * "Suggest categories for uncategorized apps". Lazy-loaded by AppsView only
 * when the planner is on. The daemon applies the guesses; this refreshes the
 * list afterwards so they show up (and can be changed with the usual dropdown).
 */
import { onMount } from 'svelte';
import { planCardVisible, planner } from '../lib/planner';
import type { PlannerStatus } from '../lib/planner';
import { store } from '../lib/store.svelte';
import Button from './Button.svelte';
import Icon from './Icon.svelte';

let status = $state<PlannerStatus>();
let busy = $state(false);

onMount(async () => {
  try {
    status = await planner.status();
  } catch {
    // Not usable right now; the action simply doesn't appear.
  }
});

const uncategorized = $derived(store.apps.filter((a) => a.categoryId == null));

async function suggest() {
  const appIds = uncategorized.map((a) => a.appId);
  if (appIds.length === 0) return;
  busy = true;
  const guesses = await store.attempt(() => planner.categorize(appIds));
  busy = false;
  if (!guesses) return;
  await store.refreshApps();
  store.tick++;
  store.notify(
    guesses.length ? 'success' : 'info',
    guesses.length
      ? `Categorized ${guesses.length} app${guesses.length === 1 ? '' : 's'}. Change any of them below.`
      : "Couldn't suggest a category for those apps.",
  );
}
</script>

{#if planCardVisible(status)}
	<div class="flex flex-wrap items-center gap-3">
		<Button
			disabled={busy || uncategorized.length === 0 || !store.connected}
			onclick={suggest}
			title={uncategorized.length === 0 ? "Every app already has a category" : undefined}
		>
			<Icon name="bolt" size={14} />
			{busy ? "Thinking…" : "Suggest categories for uncategorized apps"}
			{#if !busy && uncategorized.length > 0}<span class="rounded-full bg-accent/20 px-1.5 text-[11px] tabular-nums">{uncategorized.length}</span>{/if}
		</Button>
		{#if status?.provider === "remote" && status.host}
			<p class="text-xs text-muted">Sends the names of those apps to {status.host}.</p>
		{/if}
	</div>
{/if}
