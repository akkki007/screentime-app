<script lang="ts">
import { appLabel } from '../lib/format';
import { store } from '../lib/store.svelte';

const { appId, size = 32 }: { appId: string; size?: number } = $props();

const label = $derived(appLabel(appId, store.appName(appId)));
const color = $derived(store.categoryOf(appId)?.color ?? '#64748b');
</script>

<span
	class="grid shrink-0 place-items-center rounded-lg font-semibold text-white"
	style="width: {size}px; height: {size}px; background: {color}; font-size: {size * 0.42}px"
	aria-hidden="true"
>
	{label.charAt(0).toUpperCase()}
</span>
