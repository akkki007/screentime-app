<script lang="ts">
import { daemon } from '../lib/api';
import { appLabel, clockTime, formatDuration } from '../lib/format';
import { store } from '../lib/store.svelte';
import AppAvatar from './AppAvatar.svelte';
import Button from './Button.svelte';
import Card from './Card.svelte';
import Icon from './Icon.svelte';

let now = $state(Date.now());
$effect(() => {
  const id = setInterval(() => {
    now = Date.now();
  }, 1000);
  return () => clearInterval(id);
});

const status = $derived(store.status);

async function pause(minutes: number) {
  await store.attempt(() => daemon('tracker.pause', { minutes }));
}
async function resume() {
  await store.attempt(() => daemon('tracker.resume'));
}
</script>

{#snippet row(title: string, sub: string)}
	<div class="min-w-0 flex-1">
		<p class="truncate text-[15px] font-medium">{title}</p>
		<p class="mt-0.5 truncate text-[13px] text-muted">{sub}</p>
	</div>
{/snippet}

<Card title="Now">
	<div class="flex min-h-12 items-center gap-3">
		{#if !status}
			<span class="grid size-11 place-items-center rounded-xl bg-surface-2 text-muted"><Icon name="alert" /></span>
			{@render row("Not connected", "Waiting for the tracker daemon.")}
		{:else if status.paused}
			<span class="grid size-11 place-items-center rounded-xl bg-warn/10 text-warn"><Icon name="pause" /></span>
			{@render row("Tracking paused", status.resumeAt ? `Resumes at ${clockTime(status.resumeAt)}` : "Paused")}
		{:else if status.idle}
			<span class="grid size-11 place-items-center rounded-xl bg-surface-2 text-muted"><Icon name="today" /></span>
			{@render row("Idle", "No input recently, so time isn't being counted.")}
		{:else if status.currentAppId}
			<AppAvatar appId={status.currentAppId} size={44} />
			<div class="min-w-0 flex-1">
				<p class="truncate text-[15px] font-medium">{appLabel(status.currentAppId, store.appName(status.currentAppId))}</p>
				<p class="mt-0.5 flex items-center gap-1.5 text-[13px] text-muted">
					<span class="size-1.5 rounded-full bg-good"></span>
					Tracking{status.since ? ` · ${formatDuration(now - status.since)}` : ""}
				</p>
			</div>
		{:else}
			<span class="grid size-11 place-items-center rounded-xl bg-surface-2 text-muted"><Icon name="today" /></span>
			{@render row("Waiting for a focused window", "If this stays empty, the GNOME extension may not be enabled.")}
		{/if}
	</div>

	{#snippet footer()}
		{#if status?.paused}
			<span class="flex-1 text-[13px] text-muted">Paused</span>
			<Button size="sm" onclick={resume}><Icon name="play" size={13} /> Resume</Button>
		{:else}
			<span class="flex-1 text-[13px] text-muted">Pause tracking</span>
			<Button size="sm" disabled={!status} onclick={() => pause(15)} title="Pause tracking for 15 minutes">15 min</Button>
			<Button size="sm" disabled={!status} onclick={() => pause(60)} title="Pause tracking for an hour">1 hour</Button>
		{/if}
	{/snippet}
</Card>
