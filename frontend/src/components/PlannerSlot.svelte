<script lang="ts">
import { type Component, mount, unmount } from 'svelte';
import { store } from '../lib/store.svelte';

/**
 * Loads a planner component (a separate chunk) only while the planner is
 * switched on, and unloads it when it is switched off. Mounted with `mount`
 * rather than rendered as `<C />` to keep the main bundle's footprint minimal.
 */
const { load }: { load: () => Promise<{ default: Component }> } = $props();
let host: HTMLElement;
// A derived boolean, so saving other settings doesn't remount the component.
const on = $derived((store.settings as { plannerEnabled?: boolean } | undefined)?.plannerEnabled);

$effect(() => {
  if (!on) return;
  let live = true;
  let instance: ReturnType<typeof mount> | undefined;
  void load().then((m) => {
    if (live) instance = mount(m.default, { target: host });
  });
  return () => {
    live = false;
    if (instance) void unmount(instance);
  };
});
</script>

<div bind:this={host} class="contents"></div>
