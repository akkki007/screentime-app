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
  variant?: 'primary' | 'secondary' | 'danger' | 'ghost' | 'onDark';
  onclick?: () => void;
  children: Snippet;
  disabled?: boolean;
  type?: 'button' | 'submit';
  title?: string;
} = $props();

const styles = {
  primary: 'bg-dark text-on-dark hover:opacity-90',
  onDark: 'bg-white/15 text-on-dark hover:bg-white/25',
  secondary: 'bg-surface-2 text-ink hover:bg-white/80 border border-line',
  danger: 'bg-bad text-white hover:brightness-110',
  ghost: 'text-muted hover:text-ink hover:bg-surface-2',
};
</script>

<button
	{type}
	{title}
	{disabled}
	{onclick}
	class="inline-flex items-center justify-center gap-1.5 whitespace-nowrap rounded-full px-4 py-2 text-[13px] font-medium transition disabled:cursor-not-allowed disabled:opacity-50 {styles[variant]}"
>
	{@render children()}
</button>
