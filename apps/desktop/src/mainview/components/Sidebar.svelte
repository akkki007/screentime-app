<script lang="ts">
import { type ViewId, store } from '../lib/store.svelte';
import Icon from './Icon.svelte';

const items: {
  id: ViewId;
  label: string;
  icon: 'today' | 'week' | 'apps' | 'limits' | 'wellbeing' | 'settings';
}[] = [
  { id: 'today', label: 'Today', icon: 'today' },
  { id: 'week', label: 'Trends', icon: 'week' },
  { id: 'apps', label: 'Apps', icon: 'apps' },
  { id: 'limits', label: 'Limits', icon: 'limits' },
  { id: 'wellbeing', label: 'Wellbeing', icon: 'wellbeing' },
  { id: 'settings', label: 'Settings', icon: 'settings' },
];

const state = $derived(
  !store.connected
    ? { text: 'Daemon offline', dot: 'bg-bad' }
    : store.status?.paused
      ? { text: 'Paused', dot: 'bg-warn' }
      : store.status?.idle
        ? { text: 'Idle', dot: 'bg-muted' }
        : { text: 'Tracking', dot: 'bg-good animate-pulse' },
);
</script>

<nav class="flex h-full w-52 shrink-0 flex-col border-r border-line bg-surface px-3 py-4" aria-label="Main">
	<div class="mb-6 flex items-center gap-2.5 px-2">
		<span class="grid size-8 place-items-center rounded-xl bg-accent text-white"><Icon name="today" size={18} /></span>
		<span class="text-[15px] font-semibold tracking-tight">Screentime</span>
	</div>

	<ul class="flex flex-1 flex-col gap-1">
		{#each items as item (item.id)}
			<li>
				<button
					class="flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left text-[13px] font-medium transition
						{store.view === item.id ? 'bg-accent-soft text-accent' : 'text-muted hover:bg-surface-2 hover:text-ink'}"
					aria-current={store.view === item.id ? "page" : undefined}
					onclick={() => (store.view = item.id)}
				>
					<Icon name={item.icon} size={17} />
					{item.label}
				</button>
			</li>
		{/each}
	</ul>

	<div class="flex items-center gap-2 rounded-lg bg-surface-2 px-3 py-2 text-xs text-muted" role="status">
		<span class="size-2 rounded-full {state.dot}"></span>
		{state.text}
	</div>
</nav>
