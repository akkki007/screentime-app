<script lang="ts">
import { type ViewId, store } from '../lib/store.svelte';
import Icon from './Icon.svelte';

const items: {
  id: ViewId;
  label: string;
  icon: 'today' | 'week' | 'apps' | 'limits' | 'wellbeing' | 'settings';
}[] = [
  { id: 'today', label: 'Dashboard', icon: 'today' },
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

<header class="flex items-center justify-between gap-4 px-8 pt-6">
  <div class="flex items-center gap-2.5">
    <span class="grid size-9 place-items-center rounded-full bg-dark text-on-dark"><Icon name="today" size={18} /></span>
    <span class="text-[17px] font-medium tracking-tight">Screentime</span>
  </div>

  <nav class="flex items-center gap-2" aria-label="Main">
    {#each items as item (item.id)}
      {@const active = store.view === item.id}
      <button
        class="flex h-11 items-center gap-2 rounded-full transition
          {active ? 'bg-dark px-5 text-on-dark shadow-lg' : 'w-11 justify-center bg-surface text-muted backdrop-blur-xl hover:text-ink'}"
        aria-current={active ? 'page' : undefined}
        aria-label={item.label}
        title={item.label}
        onclick={() => (store.view = item.id)}
      >
        <Icon name={item.icon} size={17} />
        {#if active}<span class="text-[13px] font-medium">{item.label}</span>{/if}
      </button>
    {/each}
  </nav>

  <div class="flex items-center gap-2 rounded-full bg-surface px-4 py-2.5 text-xs backdrop-blur-xl" role="status">
    <span class="size-2 rounded-full {state.dot}"></span>
    {state.text}
  </div>
</header>
