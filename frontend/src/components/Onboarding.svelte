<script lang="ts">
import { daemon } from '../lib/api';
import { store } from '../lib/store.svelte';
import Button from './Button.svelte';
import Icon from './Icon.svelte';
import Modal from './Modal.svelte';

let step = $state(0);
const detected = $derived(Boolean(store.status?.currentAppId) || store.mode === 'mock');

async function finish() {
  const updated = await store.attempt(() => daemon('settings.set', { onboardingDone: true }));
  if (updated) store.settings = updated;
}
</script>

<Modal title={step === 0 ? "Welcome to Screentime" : step === 1 ? "Is tracking working?" : "You're set"} onclose={finish} width="max-w-lg">
	{#if step === 0}
		<div class="flex flex-col gap-3 text-[13px] leading-relaxed">
			<p>Screentime shows where your time goes on this computer, nudges you to take breaks, and lets you set daily limits.</p>
			<ul class="flex flex-col gap-2">
				<li class="flex gap-2"><span class="mt-0.5 text-good"><Icon name="shield" size={16} /></span><span><b>Local only.</b> No account, no telemetry, no network access.</span></li>
				<li class="flex gap-2"><span class="mt-0.5 text-good"><Icon name="shield" size={16} /></span><span><b>Titles off.</b> Window titles are never recorded unless you turn that on.</span></li>
				<li class="flex gap-2"><span class="mt-0.5 text-good"><Icon name="shield" size={16} /></span><span><b>You're in control.</b> Pause any time, export or delete everything in Settings.</span></li>
			</ul>
		</div>
	{:else if step === 1}
		<div class="flex flex-col gap-3 text-[13px] leading-relaxed">
			{#if detected}
				<p class="flex items-center gap-2 rounded-xl bg-good/10 p-3 text-good"><Icon name="check" size={16} /> Tracking is working: your focused window is being detected.</p>
			{:else}
				<p class="flex items-start gap-2 rounded-xl bg-warn/10 p-3 text-warn">
					<span class="mt-0.5"><Icon name="alert" size={16} /></span>
					No focused window detected yet. Click into another app and come back; this updates automatically.
				</p>
				<p class="text-muted">
					On GNOME, Screentime needs its small Shell extension. Enable it with
					<code class="rounded bg-surface-2 px-1">gnome-extensions enable screentime-focus@akkki007.github.io</code>, then log out and back in.
				</p>
			{/if}
			<p class="text-muted">The browser extension is optional and adds per-site time. See the README to install it.</p>
		</div>
	{:else}
		<p class="text-[13px] leading-relaxed">
			Give it a day of normal use, then check <b>Today</b> and <b>Trends</b>. Tracking continues in the background even when this window is closed.
		</p>
	{/if}

	{#snippet footer()}
		{#if step > 0}<Button onclick={() => step--}>Back</Button>{/if}
		{#if step < 2}
			<Button variant="primary" onclick={() => step++}>Continue</Button>
		{:else}
			<Button variant="primary" onclick={finish}>Get started</Button>
		{/if}
	{/snippet}
</Modal>
