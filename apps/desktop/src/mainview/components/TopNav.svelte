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

<!-- Narrow windows (Electrobun has no minimum window size): the wordmark and
     status text collapse first, then the brand mark and active label; below
     that the nav scrolls rather than overflowing the page. -->
<header class="flex items-center justify-between gap-3 px-4 pt-6 sm:gap-4 sm:px-8">
  <div class="flex shrink-0 items-center gap-2.5 max-sm:hidden">
    <span class="grid size-9 place-items-center rounded-full bg-dark text-on-dark"><Icon name="today" size={18} /></span>
    <span class="text-[17px] font-medium tracking-tight max-md:hidden">Screentime</span>
  </div>

  <nav class="-my-3 flex min-w-0 items-center gap-2 overflow-x-auto py-3 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden" aria-label="Main">
    {#each items as item (item.id)}
      {@const active = store.view === item.id}
      <button
        class="flex h-11 items-center gap-2 rounded-full transition
          {active ? 'shrink-0 bg-dark text-on-dark shadow-lg max-sm:w-11 max-sm:justify-center sm:px-5' : 'w-11 shrink-0 justify-center bg-surface text-muted backdrop-blur-xl hover:text-ink'}"
        aria-current={active ? 'page' : undefined}
        aria-label={item.label}
        title={item.label}
        onclick={() => (store.view = item.id)}
      >
        <Icon name={item.icon} size={17} />
        {#if active}<span class="text-[13px] font-medium max-sm:hidden">{item.label}</span>{/if}
      </button>
    {/each}
  </nav>

  <div class="flex shrink-0 items-center gap-2 rounded-full bg-surface px-3.5 py-2.5 text-xs backdrop-blur-xl md:px-4" role="status" title={state.text}>
    <span class="size-2 rounded-full {state.dot}"></span>
    <span class="max-md:sr-only">{state.text}</span>
  </div>
</header>
