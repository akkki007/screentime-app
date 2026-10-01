<script lang="ts">
import type { Snippet } from 'svelte';

const {
  variant = 'secondary',
  onclick,
  children,
  disabled = false,
  type = 'button',
  title,
}: {
  variant?: 'primary' | 'secondary' | 'danger' | 'ghost';
  onclick?: () => void;
  children: Snippet;
  disabled?: boolean;
  type?: 'button' | 'submit';
  title?: string;
} = $props();

const styles = {
  primary: 'bg-accent text-white hover:brightness-110',
  secondary: 'bg-surface-2 text-ink hover:brightness-95 border border-line',
  danger: 'bg-bad text-white hover:brightness-110',
  ghost: 'text-muted hover:text-ink hover:bg-surface-2',
};
</script>

<button
	{type}
	{title}
	{disabled}
	{onclick}
	class="inline-flex items-center justify-center gap-1.5 rounded-lg px-3 py-1.5 text-[13px] font-medium transition disabled:cursor-not-allowed disabled:opacity-50 {styles[variant]}"
>
	{@render children()}
</button>
