<script lang="ts">
import { daemon } from '../lib/api';
import { appLabel, clockTime, formatDuration } from '../lib/format';
import { store } from '../lib/store.svelte';
import AppAvatar from './AppAvatar.svelte';
import Button from './Button.svelte';
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

<div class="flex items-center gap-4 rounded-2xl border border-line bg-surface p-4 shadow-[var(--shadow)]">
	{#if !status}
		<div class="grid size-10 place-items-center rounded-xl bg-surface-2 text-muted"><Icon name="alert" /></div>
		<div class="flex-1">
			<p class="font-medium">Not connected</p>
			<p class="text-xs text-muted">Waiting for the tracker daemon.</p>
		</div>
	{:else if status.paused}
		<div class="grid size-10 place-items-center rounded-xl bg-warn/15 text-warn"><Icon name="pause" /></div>
		<div class="flex-1">
			<p class="font-medium">Tracking paused</p>
			<p class="text-xs text-muted">
				{status.resumeAt ? `Resumes at ${clockTime(status.resumeAt)}` : "Paused"}
			</p>
		</div>
		<Button variant="primary" onclick={resume}><Icon name="play" size={14} /> Resume</Button>
	{:else if status.idle}
		<div class="grid size-10 place-items-center rounded-xl bg-surface-2 text-muted"><Icon name="today" /></div>
		<div class="flex-1">
			<p class="font-medium">Idle</p>
			<p class="text-xs text-muted">No input recently, so time isn't being counted.</p>
		</div>
	{:else if status.currentAppId}
		<AppAvatar appId={status.currentAppId} size={40} />
		<div class="min-w-0 flex-1">
			<p class="truncate font-medium">{appLabel(status.currentAppId, store.appName(status.currentAppId))}</p>
			<p class="text-xs text-muted">
				<span class="mr-1.5 inline-block size-1.5 animate-pulse rounded-full bg-good align-middle"></span>
				Tracking{status.since ? ` · ${formatDuration(now - status.since)}` : ""}
			</p>
		</div>
		<div class="flex gap-2">
			<Button onclick={() => pause(15)} title="Pause tracking for 15 minutes"><Icon name="pause" size={14} /> 15 min</Button>
			<Button onclick={() => pause(60)} title="Pause tracking for an hour"><Icon name="pause" size={14} /> 1 hour</Button>
		</div>
	{:else}
		<div class="grid size-10 place-items-center rounded-xl bg-surface-2 text-muted"><Icon name="today" /></div>
		<div class="flex-1">
			<p class="font-medium">Waiting for a focused window</p>
			<p class="text-xs text-muted">If this stays empty, the GNOME extension may not be enabled.</p>
		</div>
	{/if}
</div>
