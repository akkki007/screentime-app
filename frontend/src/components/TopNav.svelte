<script lang="ts">
import { type ViewId, store } from '../lib/store.svelte';
import Icon from './Icon.svelte';

const items: { id: ViewId; label: string }[] = [
  { id: 'today', label: 'Today' },
  { id: 'week', label: 'Trends' },
  { id: 'apps', label: 'Apps' },
  { id: 'limits', label: 'Limits' },
  { id: 'wellbeing', label: 'Wellbeing' },
  { id: 'settings', label: 'Settings' },
];

const state = $derived(
  !store.connected
    ? { text: 'Daemon offline', dot: 'bg-bad' }
    : store.status?.paused
      ? { text: 'Paused', dot: 'bg-warn' }
      : store.status?.idle
        ? { text: 'Idle', dot: 'bg-muted' }
        : { text: 'Tracking', dot: 'bg-good' },
);
</script>

<!-- One bar: the wordmark, the tabs (an underline marks the current one), and the
     tracker's state. On narrow windows the wordmark and status text collapse
     first; below that the tabs scroll rather than overflowing. -->
<header class="border-b border-line bg-surface">
  <div class="mx-auto flex h-14 max-w-6xl items-center gap-6 px-4 sm:px-8">
    <div class="flex shrink-0 items-center gap-2.5 max-sm:hidden">
      <span class="grid size-7 place-items-center rounded-lg bg-dark text-on-dark"><Icon name="today" size={15} /></span>
      <span class="text-[15px] font-medium tracking-tight max-md:hidden">Screentime</span>
    </div>

    <nav class="flex h-full min-w-0 flex-1 items-stretch gap-1 overflow-x-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden" aria-label="Main">
      {#each items as item (item.id)}
        {@const active = store.view === item.id}
        <button
          class="relative shrink-0 px-3 text-[14px] transition-colors
            {active ? 'text-accent' : 'text-muted hover:text-ink'}"
          aria-current={active ? 'page' : undefined}
          onclick={() => (store.view = item.id)}
        >
          {item.label}
          {#if active}<span class="absolute inset-x-2 -bottom-px h-0.5 rounded-full bg-accent"></span>{/if}
        </button>
      {/each}
    </nav>

    <div class="flex h-8 shrink-0 items-center gap-2 rounded-[10px] border border-line px-3 text-[13px] text-muted shadow-[var(--shadow)]" role="status" title={state.text}>
      <span class="size-2 rounded-full {state.dot}"></span>
      <span class="max-md:sr-only">{state.text}</span>
    </div>
  </div>
</header>
