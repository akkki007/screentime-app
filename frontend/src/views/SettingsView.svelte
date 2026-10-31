<script lang="ts">
import type { Settings } from '@screentime/shared';
import Button from '../components/Button.svelte';
import Card from '../components/Card.svelte';
import DetailRow from '../components/DetailRow.svelte';
import Icon from '../components/Icon.svelte';
import Modal from '../components/Modal.svelte';
import PlannerSlot from '../components/PlannerSlot.svelte';
import SettingRow from '../components/SettingRow.svelte';
import Toggle from '../components/Toggle.svelte';
import { daemon, getBridge } from '../lib/api';
import { store } from '../lib/store.svelte';

const settings = $derived(store.settings);

async function save(patch: Partial<Settings>) {
  const updated = await store.attempt(() => daemon('settings.set', patch));
  if (updated) store.settings = updated;
}

async function exportAs(format: 'csv' | 'json') {
  const result = await store.attempt(() => getBridge().saveExport({ format }));
  if (result) store.notify('success', `Saved to ${result.path}`);
}

let confirm = $state<'usage' | 'everything'>();
let typed = $state('');

async function wipe() {
  const everything = confirm === 'everything';
  confirm = undefined;
  typed = '';
  const done = await store.attempt(
    () => daemon('data.wipe', { everything }),
    everything ? 'Everything was reset' : 'Usage history deleted',
  );
  if (done) await store.loadAll();
}

const IDLE_OPTIONS = [1, 2, 3, 5, 10, 15, 30];
</script>

<div class="flex flex-col gap-5">
	<header>
		<h1 class="text-2xl font-medium tracking-tight">Settings</h1>
		<p class="mt-1 text-[13px] text-muted">Everything stays on this computer.</p>
	</header>

	{#if settings}
		<Card title="Tracking">
			<SettingRow title="Count me as away after" description="With no keyboard or mouse input for this long, the current session ends. The idle time itself is never counted.">
				<select
					class="field"
					aria-label="Idle threshold"
					value={settings.idleThresholdMinutes}
					onchange={(e) => save({ idleThresholdMinutes: Number(e.currentTarget.value) })}
				>
					{#each IDLE_OPTIONS as m}<option value={m}>{m} minute{m === 1 ? "" : "s"}</option>{/each}
				</select>
			</SettingRow>
			<SettingRow title="Show the notch" description="A small black pill at the top of the screen with today's total. It shows your focus timer and speaks up at a limit or a reminder. Click it for today's top apps.">
				<Toggle label="Show the notch" checked={settings.notchEnabled} onchange={(v) => save({ notchEnabled: v })} />
			</SettingRow>
		</Card>

		<Card title="Privacy">
			<SettingRow
				title="Record window titles"
				description="Titles often contain private details such as document names, chat contacts and page titles. Off by default. Turning it off stops new titles being saved; to remove ones already saved, delete your usage history below."
			>
				<Toggle label="Record window titles" checked={settings.captureTitles} onchange={(v) => save({ captureTitles: v })} />
			</SettingRow>
			<div class="mt-4 flex items-start gap-3 rounded-xl border border-line bg-surface-2/70 p-3 text-[13px] text-muted">
				<span class="mt-0.5 text-good"><Icon name="shield" size={16} /></span>
				<p>
					Screentime never connects to the internet unless you turn on the optional planner with your own API key. Your data lives in <code class="rounded bg-surface px-1">~/.local/share/screentime</code> and
					is only ever read by this app.
				</p>
			</div>
		</Card>

		<Card title="Planner (optional)">
			<SettingRow
				title="Daily planner and smarter categories"
				description="Optional and off by default. A small AI model plans your day and sorts uncategorized apps. It needs a model file you download yourself or your own API key. Switching it on or off restarts the tracker once, to grant or remove its network access."
			>
				<Toggle
					label="Daily planner and smarter categories"
					checked={(settings as { plannerEnabled?: boolean }).plannerEnabled === true}
					onchange={(v) => save({ plannerEnabled: v } as Partial<Settings>)}
				/>
			</SettingRow>
			<PlannerSlot load={() => import("../components/PlannerSettings.svelte")} />
		</Card>
	{/if}

	<Card title="Your data">
		<SettingRow title="Export" description="Save all tracked sessions to your Downloads folder.">
			<div class="flex gap-2">
				<Button disabled={!store.connected} onclick={() => exportAs("csv")}><Icon name="download" size={14} /> CSV</Button>
				<Button disabled={!store.connected} onclick={() => exportAs("json")}><Icon name="download" size={14} /> JSON</Button>
			</div>
		</SettingRow>
		<SettingRow title="Delete usage history" description="Removes every recorded session and website visit. Settings and limits are kept.">
			<Button variant="danger" disabled={!store.connected} onclick={() => (confirm = "usage")}><Icon name="trash" size={14} /> Delete history</Button>
		</SettingRow>
		<SettingRow title="Reset everything" description="Also removes your limits, settings and app categories, as if freshly installed.">
			<Button variant="danger" disabled={!store.connected} onclick={() => (confirm = "everything")}>Reset…</Button>
		</SettingRow>
	</Card>

	<Card title="About">
		<DetailRow label="Daemon" dashed>
			<span class="size-2 rounded-full {store.connected ? 'bg-good' : 'bg-bad'}"></span>
			{store.connected ? "Connected" : "Not running"}
		</DetailRow>
		<DetailRow label="Data source" dashed>{store.mode === "mock" ? "Demo data (browser preview)" : "Local tracker daemon"}</DetailRow>
		<DetailRow label="License" dashed>GPL-3.0-or-later</DetailRow>
	</Card>
</div>

{#if confirm}
	<Modal title={confirm === "everything" ? "Reset everything?" : "Delete usage history?"} onclose={() => ((confirm = undefined), (typed = ""))}>
		<p class="text-[13px] text-muted">
			{#if confirm === "everything"}
				This permanently deletes all usage history, limits, settings and categories. It can't be undone.
			{:else}
				This permanently deletes every recorded session and website visit. It can't be undone.
			{/if}
		</p>
		{#if confirm === "everything"}
			<label class="mt-4 flex flex-col gap-1.5 text-[13px] font-medium">
				Type RESET to confirm
				<input class="field font-normal" bind:value={typed} autocomplete="off" />
			</label>
		{/if}
		{#snippet footer()}
			<Button onclick={() => ((confirm = undefined), (typed = ""))}>Cancel</Button>
			<Button variant="destructive" disabled={confirm === "everything" && typed !== "RESET"} onclick={wipe}>
				{confirm === "everything" ? "Reset everything" : "Delete history"}
			</Button>
		{/snippet}
	</Modal>
{/if}
