<script lang="ts">
const {
  checked,
  onchange,
  label,
  disabled = false,
}: {
  checked: boolean;
  onchange: (value: boolean) => void;
  label: string;
  disabled?: boolean;
} = $props();
</script>

<button
	type="button"
	role="switch"
	aria-checked={checked}
	aria-label={label}
	{disabled}
	class="relative h-6 w-11 shrink-0 rounded-full transition-colors disabled:opacity-50
		{checked ? 'bg-accent' : 'bg-line'}"
	onclick={() => onchange(!checked)}
>
	<span
		class="absolute top-0.5 left-0.5 size-5 rounded-full bg-white shadow transition-transform
			{checked ? 'translate-x-5' : ''}"
	></span>
</button>
