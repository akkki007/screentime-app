<script lang="ts">
import type { Snippet } from 'svelte';

const {
  variant = 'secondary',
  size = 'md',
  onclick,
  children,
  disabled = false,
  type = 'button',
  title,
}: {
  /**
   * primary: the one blue action. secondary: white and bordered. danger:
   * bordered, red text, for destructive actions in a list. destructive: solid
   * red, for the confirm button of a destructive dialog. ghost: no chrome.
   */
  variant?: 'primary' | 'secondary' | 'danger' | 'destructive' | 'ghost';
  /** icon: a square button that holds only an icon. */
  size?: 'md' | 'sm' | 'icon';
  onclick?: () => void;
  children: Snippet;
  disabled?: boolean;
  type?: 'button' | 'submit';
  title?: string;
} = $props();

const styles = {
  primary: 'bg-accent text-white shadow-[var(--shadow)] hover:brightness-105 active:brightness-95',
  secondary:
    'border border-line bg-surface text-ink shadow-[var(--shadow)] hover:bg-surface-2 active:bg-surface-2',
  danger: 'border border-line bg-surface text-bad shadow-[var(--shadow)] hover:bg-bad/5',
  destructive: 'bg-bad text-white shadow-[var(--shadow)] hover:brightness-105',
  ghost: 'text-muted hover:bg-surface-2 hover:text-ink',
};
const sizes = {
  md: 'h-9 px-3.5 text-[13px]',
  sm: 'h-8 px-3 text-[13px]',
  icon: 'size-9',
};
</script>

<button
	{type}
	{title}
	{disabled}
	{onclick}
	aria-label={size === "icon" ? title : undefined}
	class="inline-flex shrink-0 items-center justify-center gap-1.5 rounded-[10px] font-medium whitespace-nowrap transition disabled:cursor-not-allowed disabled:opacity-50 {styles[variant]} {sizes[size]}"
>
	{@render children()}
</button>
