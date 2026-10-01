<script lang="ts">
import type { Limit } from '@screentime/shared';
import AppAvatar from '../components/AppAvatar.svelte';
import Button from '../components/Button.svelte';
import Card from '../components/Card.svelte';
import Icon from '../components/Icon.svelte';
import Modal from '../components/Modal.svelte';
import { daemon } from '../lib/api';
import {
  addDays,
  appLabel,
  formatDuration,
  parseDurationInput,
  percent,
  startOfDay,
} from '../lib/format';
import { store } from '../lib/store.svelte';
import { type RangeUsage, latestOnly, loadRange } from '../lib/usage';

let usage = $state<RangeUsage>();
const guard = latestOnly();

async function load() {
  const ticket = guard.next();
  const today = startOfDay(Date.now());
  try {
    const range = await loadRange(today, addDays(today, 1));
    if (guard.isCurrent(ticket)) usage = range;
  } catch (err) {
    store.notify('error', err instanceof Error ? err.message : String(err));
  }
}

$effect(() => {
  void store.tick;
  if (store.connected) void load();
});

function usedToday(limit: Limit): number {
  const rows =
    limit.targetType === 'app'
      ? usage?.byApp
      : limit.targetType === 'category'
        ? usage?.byCategory
        : usage?.web;
  return rows?.find((r) => r.key === limit.target)?.ms ?? 0;
}

function targetLabel(limit: Limit): string {
  return limit.targetType === 'app'
    ? appLabel(limit.target, store.appName(limit.target))
    : limit.target;
}

const ACTIONS = {
  notify: 'Notify me',
  overlay: 'Show a reminder overlay',
  block: 'Overlay (nudge)',
} as const;

// --- editor ---------------------------------------------------------------

type Draft = {
  id?: number;
  targetType: Limit['targetType'];
  target: string;
  duration: string;
  action: Limit['action'];
  scheduled: boolean;
  from: string;
  to: string;
};

let draft = $state<Draft>();
let formError = $state<string>();

function newDraft(): Draft {
  return {
    targetType: 'app',
    target: store.apps[0]?.appId ?? '',
    duration: '1h',
    action: 'notify',
    scheduled: false,
    from: '09:00',
    to: '18:00',
  };
}

function edit(limit: Limit) {
  const [from, to] = limit.schedule?.split('-') ?? ['09:00', '18:00'];
  const minutes = Math.round(limit.dailyMs / 60_000);
  draft = {
    id: limit.id,
    targetType: limit.targetType,
    target: limit.target,
    duration: minutes % 60 === 0 ? `${minutes / 60}h` : `${minutes}m`,
    action: limit.action,
    scheduled: Boolean(limit.schedule),
    from: from ?? '09:00',
    to: to ?? '18:00',
  };
  formError = undefined;
}

function changeType(type: Limit['targetType']) {
  if (!draft) return;
  draft.targetType = type;
  draft.target =
    type === 'app'
      ? (store.apps[0]?.appId ?? '')
      : type === 'category'
        ? (store.categories[0]?.name ?? '')
        : '';
}

/** "https://www.YouTube.com/watch?v=1" -> "www.youtube.com" */
function cleanDomain(input: string): string {
  return (
    input
      .trim()
      .toLowerCase()
      .replace(/^[a-z]+:\/\//, '')
      .split(/[/?#]/)[0] ?? ''
  );
}

function fail(message: string) {
  formError = message;
}

async function save() {
  if (!draft) return;
  const dailyMs = parseDurationInput(draft.duration);
  const target = draft.targetType === 'domain' ? cleanDomain(draft.target) : draft.target;

  if (!target) return fail('Choose what to limit.');
  if (!dailyMs) return fail('Enter a duration like "45m", "1h 30m" or "1:30".');
  if (dailyMs > 24 * 3_600_000) return fail("A daily limit can't be more than 24 hours.");
  if (draft.scheduled && (!draft.from || !draft.to || draft.from === draft.to)) {
    return fail('Pick different start and end times.');
  }

  const limit: Limit = {
    ...(draft.id !== undefined ? { id: draft.id } : {}),
    targetType: draft.targetType,
    target,
    dailyMs,
    action: draft.action,
    ...(draft.scheduled ? { schedule: `${draft.from}-${draft.to}` } : {}),
  };
  const saved = await store.attempt(() => daemon('limits.set', limit), 'Limit saved');
  if (saved) {
    draft = undefined;
    await store.refreshLimits();
    store.tick++;
  }
}

let confirmDelete = $state<Limit>();
async function remove() {
  const limit = confirmDelete;
  if (!limit?.id) return;
  confirmDelete = undefined;
  const result = await store.attempt(
    () => daemon('limits.delete', { id: limit.id as number }),
    'Limit removed',
  );
  if (result) await store.refreshLimits();
}
</script>

<div class="flex flex-col gap-5">
	<header class="flex items-end justify-between gap-4">
		<div>
			<h1 class="text-2xl font-semibold tracking-tight">Limits</h1>
			<p class="text-sm text-muted">Daily limits for an app, a category or a website. They nudge you; they don't lock anything.</p>
		</div>
		<Button variant="primary" onclick={() => ((draft = newDraft()), (formError = undefined))}>
			<Icon name="plus" size={14} /> New limit
		</Button>
	</header>

	{#if store.limits.length === 0}
		<Card>
			<div class="py-10 text-center">
				<p class="font-medium">No limits yet</p>
				<p class="mx-auto mt-1 max-w-sm text-sm text-muted">
					Set one for the app or site you lose the most time to. You'll get a notification when you hit it.
				</p>
			</div>
		</Card>
	{:else}
		<div class="grid gap-4 lg:grid-cols-2">
			{#each store.limits as limit (limit.id)}
				{@const used = usedToday(limit)}
				{@const pct = percent(used, limit.dailyMs)}
				<Card>
					<div class="flex items-start gap-3">
						{#if limit.targetType === "app"}
							<AppAvatar appId={limit.target} size={36} />
						{:else}
							<span class="grid size-9 shrink-0 place-items-center rounded-lg bg-surface-2 text-muted">
								<Icon name={limit.targetType === "category" ? "apps" : "week"} size={16} />
							</span>
						{/if}
						<div class="min-w-0 flex-1">
							<p class="truncate font-medium">{targetLabel(limit)}</p>
							<p class="text-xs text-muted">
								<span class="capitalize">{limit.targetType}</span> · {ACTIONS[limit.action]}{limit.schedule ? ` · ${limit.schedule.replace("-", "–")}` : ""}
							</p>
						</div>
						<Button variant="ghost" title="Edit limit" onclick={() => edit(limit)}><Icon name="edit" size={15} /></Button>
						<Button variant="ghost" title="Delete limit" onclick={() => (confirmDelete = limit)}><Icon name="trash" size={15} /></Button>
					</div>
					<div class="mt-4">
						<div class="mb-1.5 flex items-baseline justify-between text-xs">
							<span class="font-medium tabular-nums">{formatDuration(used)} <span class="font-normal text-muted">of {formatDuration(limit.dailyMs)}</span></span>
							<span class="tabular-nums {pct >= 100 ? 'text-bad' : 'text-muted'}">{pct}%</span>
						</div>
						<div class="h-2 overflow-hidden rounded-full bg-surface-2" role="progressbar" aria-valuenow={Math.min(pct, 100)} aria-valuemin={0} aria-valuemax={100} aria-label="Used today">
							<div class="h-full rounded-full transition-all {pct >= 100 ? 'bg-bad' : pct >= 80 ? 'bg-warn' : 'bg-accent'}" style="width: {Math.min(pct, 100)}%"></div>
						</div>
					</div>
				</Card>
			{/each}
		</div>
	{/if}
</div>

{#if draft}
	{@const d = draft}
	<Modal title={d.id !== undefined ? "Edit limit" : "New limit"} onclose={() => (draft = undefined)}>
		<form
			class="flex flex-col gap-4"
			onsubmit={(e) => {
				e.preventDefault();
				void save();
			}}
		>
			<label class="flex flex-col gap-1.5 text-[13px] font-medium">
				Limit a
				<select class="rounded-lg border border-line bg-surface-2 px-3 py-2 font-normal" value={d.targetType} onchange={(e) => changeType(e.currentTarget.value as Limit["targetType"])}>
					<option value="app">App</option>
					<option value="category">Category</option>
					<option value="domain">Website</option>
				</select>
			</label>

			<label class="flex flex-col gap-1.5 text-[13px] font-medium">
				{d.targetType === "app" ? "App" : d.targetType === "category" ? "Category" : "Website"}
				{#if d.targetType === "app"}
					<select class="rounded-lg border border-line bg-surface-2 px-3 py-2 font-normal" bind:value={d.target}>
						{#each store.apps as a (a.appId)}<option value={a.appId}>{appLabel(a.appId, a.name)}</option>{/each}
					</select>
				{:else if d.targetType === "category"}
					<select class="rounded-lg border border-line bg-surface-2 px-3 py-2 font-normal" bind:value={d.target}>
						{#each store.categories as c (c.id)}<option value={c.name}>{c.name}</option>{/each}
					</select>
				{:else}
					<input class="rounded-lg border border-line bg-surface-2 px-3 py-2 font-normal" placeholder="youtube.com" bind:value={d.target} />
				{/if}
			</label>

			<div class="grid grid-cols-2 gap-3">
				<label class="flex flex-col gap-1.5 text-[13px] font-medium">
					Per day
					<input class="rounded-lg border border-line bg-surface-2 px-3 py-2 font-normal" placeholder="1h 30m" bind:value={d.duration} />
				</label>
				<label class="flex flex-col gap-1.5 text-[13px] font-medium">
					When reached
					<select class="rounded-lg border border-line bg-surface-2 px-3 py-2 font-normal" bind:value={d.action}>
						{#each Object.entries(ACTIONS) as [value, label]}<option {value}>{label}</option>{/each}
					</select>
				</label>
			</div>

			<div class="flex flex-col gap-2 text-[13px]">
				<label class="flex items-center gap-2 font-medium">
					<input type="checkbox" bind:checked={d.scheduled} />
					Only enforce during certain hours
				</label>
				{#if d.scheduled}
					<div class="flex items-center gap-2">
						<input type="time" class="rounded-lg border border-line bg-surface-2 px-3 py-1.5" aria-label="From" bind:value={d.from} />
						<span class="text-muted">to</span>
						<input type="time" class="rounded-lg border border-line bg-surface-2 px-3 py-1.5" aria-label="To" bind:value={d.to} />
					</div>
				{/if}
			</div>

			{#if formError}<p class="text-[13px] text-bad" role="alert">{formError}</p>{/if}

			<div class="mt-2 flex justify-end gap-2">
				<Button onclick={() => (draft = undefined)}>Cancel</Button>
				<Button variant="primary" type="submit">Save limit</Button>
			</div>
		</form>
	</Modal>
{/if}

{#if confirmDelete}
	<Modal title="Delete this limit?" onclose={() => (confirmDelete = undefined)}>
		<p class="text-[13px] text-muted">The limit for <b class="text-ink">{targetLabel(confirmDelete)}</b> will stop applying. Your usage history is not affected.</p>
		{#snippet footer()}
			<Button onclick={() => (confirmDelete = undefined)}>Cancel</Button>
			<Button variant="danger" onclick={remove}>Delete</Button>
		{/snippet}
	</Modal>
{/if}
