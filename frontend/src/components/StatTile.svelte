<script lang="ts">
const {
  label,
  value,
  hint,
  tone = 'neutral',
  look = 'glass',
}: {
  label: string;
  value: string;
  hint?: string;
  tone?: 'neutral' | 'good' | 'bad';
  look?: 'glass' | 'orange' | 'sun' | 'dark';
} = $props();

const looks = {
  glass: 'bg-surface backdrop-blur-xl',
  orange: 'bg-accent text-[#1a0d03]',
  sun: 'bg-sun text-[#1a1a03]',
  dark: 'bg-dark text-on-dark',
};
const hintTone = $derived(
  look !== 'glass'
    ? 'opacity-70'
    : tone === 'good'
      ? 'text-good'
      : tone === 'bad'
        ? 'text-bad'
        : 'text-muted',
);
</script>

<div class="rounded-[28px] p-5 shadow-[var(--shadow)] {looks[look]}">
  <p class="text-[13px] {look === 'glass' ? 'text-muted' : 'opacity-70'}">{label}</p>
  <p class="mt-3 text-4xl font-light tracking-tight">{value}</p>
  {#if hint}<p class="mt-2 text-xs {hintTone}">{hint}</p>{/if}
</div>
