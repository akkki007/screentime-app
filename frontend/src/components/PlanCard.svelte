<script lang="ts">
/**
 * "Plan my day". Lazy-loaded by TodayView only when the planner is on, and it
 * renders nothing unless planner.status says the planner is usable. Suggestions
 * are never applied automatically: Accept goes through the same APIs as the
 * Limits and Wellbeing screens.
 */
import { onDestroy, onMount } from 'svelte';
import { daemon } from '../lib/api';
import { appLabel } from '../lib/format';
import {
  LIMITS,
  type Plan,
  type PlannerStatus,
  type Suggestion,
  describeAction,
  disclosureHost,
  elapsedLabel,
  isActionable,
  notReadyReason,
  planCardVisible,
  planner,
  plannerSettings,
  suggestionFocusMinutes,
  suggestionToLimit,
} from '../lib/planner';
import { store } from '../lib/store.svelte';
import Button from './Button.svelte';
import Card from './Card.svelte';
import Icon from './Icon.svelte';

let status = $state<PlannerStatus>();
let goal = $state('');
let plan = $state<Plan>();
let busy = $state(false);
let seconds = $state(0);

// Rows are tracked by position. `hidden` rows were dismissed.
let accepted = $state(new Set<number>());
let hidden = $state(new Set<number>());
let working = $state<number>();

let run = 0;
let timer: ReturnType<typeof setInterval> | undefined;

onMount(async () => {
  try {
    status = await planner.status();
  } catch {
    // Not usable right now; the card simply doesn't appear.
  }
});
onDestroy(() => clearInterval(timer));

const settings = $derived(plannerSettings(store.settings));
const visible = $derived(planCardVisible(status));
const blocked = $derived(status ? notReadyReason(status, settings.plannerEndpoint) : undefined);
const host = $derived(disclosureHost(status, settings.plannerEndpoint));

async function start() {
  if (busy) return;
  const id = ++run;
  busy = true;
  plan = undefined;
  accepted = new Set();
  hidden = new Set();
  seconds = 0;
  const began = Date.now();
  clearInterval(timer);
  timer = setInterval(() => {
    seconds = (Date.now() - began) / 1000;
  }, 500);
  try {
    const result = await planner.suggest(goal.trim() || undefined);
    if (id === run) plan = result;
  } catch (err) {
    if (id === run) store.notify('error', err instanceof Error ? err.message : String(err));
  } finally {
    if (id === run) {
      busy = false;
      clearInterval(timer);
    }
  }
}

/** Cancel only stops waiting: the result of the request in flight is ignored. */
function cancel() {
  run++;
  busy = false;
  clearInterval(timer);
}

const labels = {
  app: (appId: string) => appLabel(appId, store.appName(appId)),
  category: (id: number) => store.categories.find((c) => c.id === id)?.name ?? 'a category',
};

function limitFor(s: Suggestion) {
  return suggestionToLimit(s, store.categories, store.limits);
}
/** Why a row can't be accepted, if it can't. */
function cannotAccept(s: Suggestion): string | undefined {
  if (s.kind === 'limit' && !limitFor(s))
    return "This limit can't be set from here. Add it in Limits.";
  return undefined;
}

async function accept(i: number, s: Suggestion) {
  working = i;
  let ok: unknown;
  if (s.kind === 'limit') {
    const limit = limitFor(s);
    if (limit) {
      ok = await store.attempt(() => daemon('limits.set', limit), 'Limit saved');
      if (ok) await store.refreshLimits();
    }
  } else if (s.kind === 'focus') {
    const minutes = suggestionFocusMinutes(s);
    ok = await store.attempt(
      () => daemon('focus.start', { minutes }),
      `Focus mode on for ${minutes} minutes`,
    );
  }
  working = undefined;
  if (ok) {
    accepted = new Set(accepted).add(i);
    store.tick++;
  }
}

function dismiss(i: number) {
  hidden = new Set(hidden).add(i);
}

const KIND = {
  limit: { icon: 'limits', label: 'Limit' },
  focus: { icon: 'bolt', label: 'Focus' },
  break: { icon: 'wellbeing', label: 'Break' },
  note: { icon: 'edit', label: 'Note' },
} as const;

const rows = $derived(
  (plan?.suggestions ?? []).map((s, i) => ({ s, i })).filter(({ i }) => !hidden.has(i)),
);
</script>

{#if visible && status}
	<Card title="Plan my day" subtitle="Suggestions from your usage so far. Nothing changes until you accept it.">
		<div class="flex flex-col gap-3">
			<div class="flex flex-col gap-1.5">
				<label for="plan-goal" class="text-[13px] font-medium">What would you like from today? <span class="font-normal text-muted">(optional)</span></label>
				<textarea
					id="plan-goal"
					class="min-h-[4.5rem] w-full resize-y field"
					rows="2"
					maxlength={LIMITS.goal}
					placeholder="e.g. Finish the report by 4pm and spend less time in chat"
					bind:value={goal}
					disabled={busy}
					aria-describedby="plan-goal-count"
				></textarea>
				<p id="plan-goal-count" class="self-end text-[11px] text-muted tabular-nums" aria-live="off">{goal.length}/{LIMITS.goal}</p>
			</div>

			<div class="flex flex-wrap items-center gap-3">
				{#if busy}
					<Button variant="secondary" onclick={cancel}><Icon name="x" size={14} /> Cancel</Button>
					<p class="text-xs text-muted" role="status">
						Thinking… {elapsedLabel(seconds)}{#if status.provider === "local" && seconds > 15}. A local model on a CPU can take a minute or two.{/if}
					</p>
				{:else}
					<Button variant="primary" disabled={!store.connected || Boolean(blocked)} onclick={start}>
						<Icon name="bolt" size={14} /> {plan ? "Plan again" : "Plan my day"}
					</Button>
					{#if blocked}
						<p class="text-xs text-muted">{blocked} Set it up in Settings.</p>
					{:else if status.provider === "remote" && host}
						<p class="text-xs text-muted">Sends daily totals per app and category to {host}.</p>
					{:else if status.state === "error" && status.lastError}
						<p class="text-xs text-bad">Last attempt failed: {status.lastError}</p>
					{/if}
				{/if}
			</div>
		</div>

		{#if busy}
			<div class="mt-5 flex flex-col gap-3" aria-hidden="true">
				<div class="h-4 w-3/4 animate-pulse rounded-full bg-line"></div>
				{#each [0, 1, 2] as n (n)}
					<div class="flex items-center gap-3 rounded-2xl bg-surface-2 p-3">
						<div class="size-9 shrink-0 animate-pulse rounded-xl bg-line"></div>
						<div class="flex flex-1 flex-col gap-2">
							<div class="h-3 w-1/2 animate-pulse rounded-full bg-line"></div>
							<div class="h-3 w-5/6 animate-pulse rounded-full bg-line"></div>
						</div>
					</div>
				{/each}
			</div>
		{:else if plan}
			<div class="mt-5 flex flex-col gap-3" aria-live="polite">
				<p class="text-[13px] leading-relaxed">{plan.summary}</p>
				{#if rows.length === 0}
					<p class="text-xs text-muted">{plan.suggestions.length === 0 ? "No suggestions this time." : "All done. Plan again whenever you like."}</p>
				{/if}
				<ul class="flex flex-col gap-2">
					{#each rows as { s, i } (i)}
						{@const done = accepted.has(i)}
						{@const why = cannotAccept(s)}
						<li class="flex flex-wrap items-center gap-3 rounded-2xl bg-surface-2 p-3 {done ? 'opacity-70' : ''}">
							<span class="grid size-9 shrink-0 place-items-center rounded-xl {done ? 'bg-good/15 text-good' : 'bg-accent-soft text-accent'}">
								<Icon name={done ? "check" : KIND[s.kind].icon} size={16} />
							</span>
							<div class="min-w-0 flex-1 basis-56">
								<p class="text-[13px] font-medium">{describeAction(s, labels)}</p>
								<p class="mt-0.5 text-xs leading-relaxed text-muted">{s.reason}</p>
								{#if why}<p class="mt-0.5 text-xs text-warn">{why}</p>{/if}
							</div>
							<div class="flex shrink-0 items-center gap-1.5">
								{#if done}
									<span class="inline-flex items-center gap-1 px-2 text-[13px] font-medium text-good"><Icon name="check" size={14} /> Done</span>
								{:else}
									{#if isActionable(s)}
										<Button variant="secondary" disabled={Boolean(why) || working !== undefined || !store.connected} onclick={() => accept(i, s)} title={why}>
											{working === i ? "Applying…" : "Accept"}
										</Button>
									{/if}
									<Button variant="ghost" onclick={() => dismiss(i)}>Dismiss</Button>
								{/if}
							</div>
						</li>
					{/each}
				</ul>
			</div>
		{/if}
	</Card>
{/if}
