<script lang="ts">
import LimitOverlay from './components/LimitOverlay.svelte';
import Onboarding from './components/Onboarding.svelte';
import QuickPanel from './components/QuickPanel.svelte';
import Toasts from './components/Toasts.svelte';
import TopNav from './components/TopNav.svelte';
import { setBridge } from './lib/api';
import { createBridge } from './lib/bridge';
import { type ViewId, errorMessage, store } from './lib/store.svelte';
import AppsView from './views/AppsView.svelte';
import LimitsView from './views/LimitsView.svelte';
import SettingsView from './views/SettingsView.svelte';
import TodayView from './views/TodayView.svelte';
import WeekView from './views/WeekView.svelte';
import WellbeingView from './views/WellbeingView.svelte';

let fatal = $state<string>();
const widget = location.hash === '#widget';
if (widget) {
  document.documentElement.dataset.view = 'widget';
  document.documentElement.dataset.theme = 'dark';
}

(async () => {
  try {
    const bridge = await createBridge();
    setBridge(bridge);

    // Browser-preview conveniences, e.g. `?view=limits&theme=dark` for screenshots.
    if (bridge.mode === 'mock') {
      const q = new URLSearchParams(location.search);
      const theme = q.get('theme');
      if (theme === 'dark' || theme === 'light') document.documentElement.dataset.theme = theme;
      const view = q.get('view');
      if (view) store.view = view as ViewId;
    }

    await store.init();

    if (bridge.mode === 'mock') {
      const q = new URLSearchParams(location.search);
      const w = window as unknown as { __mock?: { limitHit(id?: number): void; reminder(): void } };
      if (q.get('limitHit') === '1') setTimeout(() => w.__mock?.limitHit(2), 300);
      if (q.get('toast') === '1') setTimeout(() => w.__mock?.reminder(), 300);
    }
  } catch (err) {
    fatal = errorMessage(err);
  }
})();

const views = {
  today: TodayView,
  week: WeekView,
  apps: AppsView,
  limits: LimitsView,
  wellbeing: WellbeingView,
  settings: SettingsView,
} as const;

const View = $derived(views[store.view]);
const needsOnboarding = $derived(
  store.connected && store.settings !== undefined && !store.settings.onboardingDone,
);
</script>

{#if fatal}
	<main class="grid h-full place-items-center p-8">
		<div class="max-w-md text-center">
			<h1 class="text-lg font-semibold">Screentime couldn't start</h1>
			<p class="mt-2 text-sm text-muted">{fatal}</p>
		</div>
	</main>
{:else if !store.ready}
	<main class="grid h-full place-items-center text-sm text-muted">Loading…</main>
{:else if widget}
  <QuickPanel />
{:else}
	<div class="flex h-full flex-col">
		<TopNav />
		<main class="min-h-0 flex-1 overflow-y-auto">
			{#if !store.connected}
				<div class="m-6 mb-0 flex items-start gap-3 rounded-xl border border-warn/40 bg-warn/10 p-4 text-[13px]" role="alert">
					<span class="mt-0.5 text-warn">!</span>
					<div>
						<p class="font-medium">The Screentime daemon isn't running</p>
						<p class="mt-0.5 text-muted">
							Tracking happens in a background service, so nothing is being recorded right now. Start it with
							<code class="rounded bg-surface px-1">bun run dev:daemon</code> or <code class="rounded bg-surface px-1">systemctl --user start screentime-daemon</code>.
							This window reconnects automatically.
						</p>
						{#if store.connectionError}<p class="mt-1 text-xs text-muted">Details: {store.connectionError}</p>{/if}
					</div>
				</div>
			{/if}
			<div class="mx-auto max-w-6xl px-4 pt-6 pb-10 sm:px-8">
				{#if store.connected || store.view === "settings"}
					<View />
				{:else}
					<div class="grid place-items-center py-24 text-center">
						<p class="text-lg font-medium">No data to show</p>
						<p class="mt-1 max-w-sm text-sm text-muted">Your usage appears here as soon as the daemon is running and this window has reconnected.</p>
					</div>
				{/if}
			</div>
		</main>
	</div>

	{#if needsOnboarding}<Onboarding />{/if}
	{#if store.limitOverlay}<LimitOverlay />{/if}
{/if}

<Toasts />
