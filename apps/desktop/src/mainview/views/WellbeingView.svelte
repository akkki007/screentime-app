<script lang="ts">
import type { Settings } from '@screentime/shared';
import Button from '../components/Button.svelte';
import Card from '../components/Card.svelte';
import Icon from '../components/Icon.svelte';
import SettingRow from '../components/SettingRow.svelte';
import Toggle from '../components/Toggle.svelte';
import { daemon } from '../lib/api';
import { clockTime } from '../lib/format';
import { store } from '../lib/store.svelte';

const settings = $derived(store.settings);

async function save(patch: Partial<Settings>) {
  const updated = await store.attempt(() => daemon('settings.set', patch));
  if (updated) store.settings = updated;
}

function number(e: Event, min: number, max: number): number | undefined {
  const value = Number((e.currentTarget as HTMLInputElement).value);
  if (!Number.isInteger(value) || value < min || value > max) {
    store.notify('error', `Enter a whole number between ${min} and ${max}.`);
    (e.currentTarget as HTMLInputElement).value = '';
    return undefined;
  }
  return value;
}

// --- focus mode ---------------------------------------------------------
let now = $state(Date.now());
$effect(() => {
  const id = setInterval(() => {
    now = Date.now();
  }, 1000);
  return () => clearInterval(id);
});

const focus = $derived(store.status?.focusMode);
const remaining = $derived(focus?.until ? Math.max(0, focus.until - now) : 0);

async function startFocus(minutes: number) {
  await store.attempt(
    () => daemon('focus.start', { minutes }),
    `Focus mode on for ${minutes} minutes`,
  );
}
async function stopFocus() {
  await store.attempt(() => daemon('focus.stop'));
}
</script>

<div class="flex flex-col gap-5">
	<header>
		<h1 class="text-2xl font-semibold tracking-tight">Wellbeing</h1>
		<p class="text-sm text-muted">Gentle nudges to take breaks, wind down and stay focused.</p>
	</header>

	<Card title="Focus mode" subtitle="Get a nudge whenever a distracting app takes focus">
		{#if focus?.active}
			<div class="flex items-center justify-between gap-4">
				<div>
					<p class="text-3xl font-semibold tracking-tight tabular-nums">
						{Math.floor(remaining / 60_000)}:{String(Math.floor((remaining % 60_000) / 1000)).padStart(2, "0")}
					</p>
					<p class="text-xs text-muted">{focus.until ? `Ends at ${clockTime(focus.until)}` : ""}</p>
				</div>
				<Button variant="danger" onclick={stopFocus}><Icon name="x" size={14} /> End focus mode</Button>
			</div>
		{:else}
			<div class="flex flex-wrap items-center gap-2">
				{#each [25, 50, 90] as minutes}
					<Button variant="secondary" disabled={!store.connected} onclick={() => startFocus(minutes)}>
						<Icon name="bolt" size={14} /> {minutes} minutes
					</Button>
				{/each}
			</div>
		{/if}
		<p class="mt-3 text-xs text-muted">
			Distracting apps are those in a category marked unproductive (Social and Entertainment by default). Change categories in Apps.
		</p>
	</Card>

	{#if settings}
		<Card title="Break reminders">
			<SettingRow title="Remind me to take breaks" description="After a stretch of continuous activity you'll get a notification. Stepping away for a few minutes counts as a break.">
				<Toggle label="Break reminders" checked={settings.breakRemindersEnabled} onchange={(v) => save({ breakRemindersEnabled: v })} />
			</SettingRow>
			<SettingRow title="Remind after" description="Minutes of continuous activity.">
				<label class="flex items-center gap-2 text-[13px]">
					<input
						type="number"
						min="5"
						max="240"
						class="w-20 rounded-lg border border-line bg-surface-2 px-2 py-1.5 text-right"
						aria-label="Minutes of activity before a break reminder"
						value={settings.breakEveryMinutes}
						disabled={!settings.breakRemindersEnabled}
						onchange={(e) => {
							const v = number(e, 5, 240);
							if (v !== undefined) void save({ breakEveryMinutes: v });
						}}
					/>
					min
				</label>
			</SettingRow>
			<SettingRow title="A break is" description="How long you need to be away for it to count.">
				<label class="flex items-center gap-2 text-[13px]">
					<input
						type="number"
						min="1"
						max="60"
						class="w-20 rounded-lg border border-line bg-surface-2 px-2 py-1.5 text-right"
						aria-label="Minutes away that count as a break"
						value={settings.breakLengthMinutes}
						disabled={!settings.breakRemindersEnabled}
						onchange={(e) => {
							const v = number(e, 1, 60);
							if (v !== undefined) void save({ breakLengthMinutes: v });
						}}
					/>
					min
				</label>
			</SettingRow>
		</Card>

		<Card title="Downtime" subtitle="A reminder to wind down when you're still on the computer late">
			<SettingRow title="Enable downtime" description="You'll get a nudge every 15 minutes while the computer is in use during this window.">
				<Toggle label="Downtime" checked={settings.downtimeEnabled} onchange={(v) => save({ downtimeEnabled: v })} />
			</SettingRow>
			<SettingRow title="Schedule" description={`Active from ${settings.downtimeStart} until ${settings.downtimeEnd}; it can run past midnight.`}>
				<div class="flex items-center gap-2 text-[13px]">
					<input
						type="time"
						class="rounded-lg border border-line bg-surface-2 px-2 py-1.5"
						aria-label="Downtime start"
						value={settings.downtimeStart}
						disabled={!settings.downtimeEnabled}
						onchange={(e) => e.currentTarget.value && save({ downtimeStart: e.currentTarget.value })}
					/>
					<span class="text-muted">to</span>
					<input
						type="time"
						class="rounded-lg border border-line bg-surface-2 px-2 py-1.5"
						aria-label="Downtime end"
						value={settings.downtimeEnd}
						disabled={!settings.downtimeEnabled}
						onchange={(e) => e.currentTarget.value && save({ downtimeEnd: e.currentTarget.value })}
					/>
				</div>
			</SettingRow>
		</Card>

		<p class="text-xs text-muted">
			Reminders are nudges, not locks: Screentime never closes apps or blocks sites in v1.
		</p>
	{/if}
</div>
