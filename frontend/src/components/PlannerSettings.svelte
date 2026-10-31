<script lang="ts">
/**
 * The planner's provider, paths, key and status. Lazy-loaded by SettingsView,
 * and only when the planner is switched on.
 */
import { onMount } from 'svelte';
import {
  LIMITS,
  type PlannerProvider,
  type PlannerSettings,
  type PlannerStatus,
  disclosureHost,
  endpointError,
  idleMinutesError,
  notReadyReason,
  pathError,
  planner,
  plannerSettings,
} from '../lib/planner';
import { store } from '../lib/store.svelte';
import Button from './Button.svelte';
import Icon from './Icon.svelte';

const settings = $derived(plannerSettings(store.settings));

let status = $state<PlannerStatus>();
let statusError = $state<string>();
let checking = $state(false);

async function refresh() {
  checking = true;
  try {
    status = await planner.status();
    statusError = undefined;
  } catch (err) {
    statusError = err instanceof Error ? err.message : String(err);
  } finally {
    checking = false;
  }
}

onMount(() => {
  void refresh();
});

// A local server takes a moment to start; follow it until it settles.
$effect(() => {
  if (status?.state !== 'starting') return;
  const id = setInterval(() => void refresh(), 2000);
  return () => clearInterval(id);
});

let saving = $state(false);

async function save(patch: Partial<PlannerSettings>): Promise<boolean> {
  saving = true;
  const updated = await store.attempt(() => planner.setSettings(patch));
  saving = false;
  if (!updated) return false;
  store.settings = updated;
  void refresh();
  return true;
}

// --- text fields: saved when they lose focus and pass validation -----------

const initial = plannerSettings(store.settings);
let endpoint = $state(initial.plannerEndpoint);
let model = $state(initial.plannerModel);
let serverPath = $state(initial.plannerServerPath);
let modelPath = $state(initial.plannerModelPath);
let idle = $state(String(initial.plannerIdleMinutes));
let touched = $state({ endpoint: false });

const errors = $derived({
  endpoint: touched.endpoint || endpoint ? endpointError(endpoint) : undefined,
  model: pathError(model, LIMITS.model, 'The model name'),
  serverPath: pathError(serverPath, LIMITS.serverPath, 'The path'),
  modelPath: pathError(modelPath, LIMITS.modelPath, 'The path'),
  idle: idleMinutesError(Number(idle)),
});

function commitEndpoint() {
  touched.endpoint = true;
  const value = endpoint.trim();
  endpoint = value;
  if (value !== settings.plannerEndpoint && !endpointError(value)) {
    void save({ plannerEndpoint: value });
  }
}
function commitText(
  key: 'plannerModel' | 'plannerServerPath' | 'plannerModelPath',
  value: string,
  error: string | undefined,
) {
  const trimmed = value.trim();
  if (!error && trimmed !== settings[key]) void save({ [key]: trimmed });
}
function commitIdle() {
  if (!errors.idle && Number(idle) !== settings.plannerIdleMinutes) {
    void save({ plannerIdleMinutes: Number(idle) });
  }
}

function chooseProvider(provider: PlannerProvider) {
  if (provider !== settings.plannerProvider) void save({ plannerProvider: provider });
}

// --- key (write-only) -------------------------------------------------------

let key = $state('');
let keyBusy = $state(false);

async function saveKey() {
  if (!key) return;
  keyBusy = true;
  const done = await store.attempt(() => planner.setKey(key), 'API key saved');
  keyBusy = false;
  if (done) {
    key = '';
    void refresh();
  }
}
async function removeKey() {
  keyBusy = true;
  const done = await store.attempt(() => planner.setKey(''), 'API key removed');
  keyBusy = false;
  if (done) {
    key = '';
    void refresh();
  }
}

const host = $derived(disclosureHost(status, settings.plannerEndpoint));
const problem = $derived(
  status ? notReadyReason({ ...status, enabled: true }, settings.plannerEndpoint) : undefined,
);

const STATE_TEXT = {
  off: 'Off until you use it',
  starting: 'Starting…',
  ready: 'Ready',
  error: 'Error',
} as const;

const input = 'w-full field aria-[invalid=true]:border-bad';
</script>

{#snippet dot(ok: boolean)}
	<span class="mr-1.5 inline-block size-1.5 rounded-full align-middle {ok ? 'bg-good' : 'bg-warn'}"></span>
{/snippet}

<div class="mt-4 flex flex-col gap-5 border-t border-line pt-5">
	<fieldset class="flex flex-col gap-2" disabled={!store.connected || saving}>
		<legend class="mb-1.5 text-[13px] font-medium">Where the model runs</legend>
		<div class="inline-flex w-fit max-w-full flex-wrap gap-0.5 rounded-[10px] border border-line bg-surface-2 p-0.5">
			{#each [{ id: "local", label: "Local model" }, { id: "remote", label: "Your own API key" }] as opt (opt.id)}
				<label
					class="cursor-pointer rounded-lg px-3.5 py-1 text-[13px] text-muted transition has-[:checked]:bg-surface has-[:checked]:font-medium has-[:checked]:text-ink has-[:checked]:shadow-[var(--shadow)] has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-offset-2 has-[:focus-visible]:outline-accent"
				>
					<input
						type="radio"
						name="planner-provider"
						class="sr-only"
						value={opt.id}
						checked={settings.plannerProvider === opt.id}
						onchange={() => chooseProvider(opt.id as PlannerProvider)}
					/>
					{opt.label}
				</label>
			{/each}
		</div>
	</fieldset>

	{#if settings.plannerProvider === "local"}
		<div class="flex flex-col gap-4">
			<label class="flex flex-col gap-1.5 text-[13px] font-medium">
				llama-server path
				<input
					class={input}
					bind:value={serverPath}
					maxlength={LIMITS.serverPath}
					placeholder="found automatically if it's on your PATH"
					autocomplete="off"
					spellcheck="false"
					aria-invalid={errors.serverPath ? "true" : "false"}
					onchange={() => commitText("plannerServerPath", serverPath, errors.serverPath)}
				/>
				{#if errors.serverPath}<span class="text-xs font-normal text-bad" role="alert">{errors.serverPath}</span>{/if}
			</label>
			<label class="flex flex-col gap-1.5 text-[13px] font-medium">
				Model file (.gguf)
				<input
					class={input}
					bind:value={modelPath}
					maxlength={LIMITS.modelPath}
					placeholder="~/models/qwen2.5-1.5b-instruct-q4_k_m.gguf"
					autocomplete="off"
					spellcheck="false"
					aria-invalid={errors.modelPath ? "true" : "false"}
					onchange={() => commitText("plannerModelPath", modelPath, errors.modelPath)}
				/>
				{#if errors.modelPath}<span class="text-xs font-normal text-bad" role="alert">{errors.modelPath}</span>{/if}
			</label>

			<p class="text-xs leading-relaxed text-muted">
				A tiny model is plenty: about 1 to 1.5 billion parameters at 4-bit, such as Qwen2.5-1.5B-Instruct or
				Llama-3.2-1B-Instruct (roughly 1 GB). You download it yourself; Screentime never fetches anything.
				Everything stays on this computer.
			</p>

			<div class="flex flex-wrap items-center gap-x-2 gap-y-1 text-[13px]">
				<label class="flex items-center gap-2">
					<span class="text-muted">It runs only while you use it, and stops after</span>
					<input
						type="number"
						min={LIMITS.idleMin}
						max={LIMITS.idleMax}
						step="1"
						class="w-16 field text-right aria-[invalid=true]:border-bad"
						aria-label="Minutes idle before the local model stops"
						aria-invalid={errors.idle ? "true" : "false"}
						bind:value={idle}
						onchange={commitIdle}
					/>
					<span class="text-muted">minutes idle.</span>
				</label>
				{#if errors.idle}<span class="w-full text-xs text-bad" role="alert">{errors.idle}</span>{/if}
			</div>
		</div>
	{:else}
		<div class="flex flex-col gap-4">
			<label class="flex flex-col gap-1.5 text-[13px] font-medium">
				Endpoint URL
				<input
					class={input}
					type="url"
					bind:value={endpoint}
					maxlength={LIMITS.endpoint}
					placeholder="https://api.example.com/v1"
					autocomplete="off"
					spellcheck="false"
					aria-invalid={errors.endpoint ? "true" : "false"}
					aria-describedby="planner-endpoint-help"
					onchange={commitEndpoint}
					onblur={() => (touched.endpoint = true)}
				/>
				<span id="planner-endpoint-help" class="text-xs font-normal {errors.endpoint ? 'text-bad' : 'text-muted'}" role={errors.endpoint ? "alert" : undefined}>
					{errors.endpoint ?? "The base URL of an OpenAI-compatible service."}
				</span>
			</label>
			<label class="flex flex-col gap-1.5 text-[13px] font-medium">
				Model name
				<input
					class={input}
					bind:value={model}
					maxlength={LIMITS.model}
					placeholder="e.g. gpt-4o-mini"
					autocomplete="off"
					spellcheck="false"
					aria-invalid={errors.model ? "true" : "false"}
					onchange={() => commitText("plannerModel", model, errors.model)}
				/>
				{#if errors.model}<span class="text-xs font-normal text-bad" role="alert">{errors.model}</span>{/if}
			</label>

			<div class="flex flex-col gap-1.5">
				<label for="planner-key" class="text-[13px] font-medium">API key</label>
				<div class="flex flex-wrap items-center gap-2">
					<input
						id="planner-key"
						class="{input} min-w-0 flex-1 basis-56"
						type="password"
						bind:value={key}
						autocomplete="off"
						spellcheck="false"
						placeholder={status?.keySet ? "Paste a new key to replace the saved one" : "Paste your API key"}
						onkeydown={(e) => e.key === "Enter" && saveKey()}
					/>
					<Button variant="primary" disabled={!key || keyBusy || !store.connected} onclick={saveKey}>
						{keyBusy ? "Saving…" : "Save key"}
					</Button>
					{#if status?.keySet}
						<Button disabled={keyBusy || !store.connected} onclick={removeKey}>Remove</Button>
					{/if}
				</div>
				<p class="flex items-center gap-1.5 text-xs text-muted" role="status">
					{#if status?.keySet}
						<span class="text-good"><Icon name="check" size={13} /></span> Key saved. It can't be shown again.
					{:else}
						No key saved yet. It is stored on this computer and never shown again.
					{/if}
				</p>
			</div>

			<div class="flex items-start gap-3 rounded-xl border border-warn/40 bg-warn/10 p-3 text-[13px]" role="note">
				<span class="mt-0.5 text-warn"><Icon name="globe" size={16} /></span>
				<div>
					<p class="font-medium">Your usage totals are sent off this computer</p>
					<p class="mt-0.5 leading-relaxed">
						When you use the planner, daily totals per app and category (no window titles, no websites) are sent to
						{#if host}<strong class="break-all">{host}</strong>{:else}<strong>the endpoint you enter above</strong>{/if}.
						Nothing is sent until you press <em>Plan my day</em> or <em>Suggest categories</em>.
					</p>
				</div>
			</div>
		</div>
	{/if}

	<div class="text-[13px]" role="status" aria-live="polite">
		{#if statusError}
			<p class="text-bad">Couldn't read the planner's status: {statusError}</p>
		{:else if !status}
			<p class="text-muted">Checking…</p>
		{:else}
			<ul class="flex flex-wrap items-center gap-x-4 gap-y-1">
				{#if status.provider === "local"}
					<li>{@render dot(status.serverFound)} llama-server {status.serverFound ? "found" : "not found"}</li>
					<li>{@render dot(status.modelFound)} Model file {status.modelFound ? "found" : "not found"}</li>
				{:else}
					<li>{@render dot(!endpointError(settings.plannerEndpoint))} Endpoint {endpointError(settings.plannerEndpoint) ? "needed" : "set"}</li>
					<li>{@render dot(status.keySet)} Key {status.keySet ? "saved" : "needed"}</li>
				{/if}
				<li>
					<span class="mr-1.5 inline-block size-1.5 rounded-full align-middle {status.state === 'ready' ? 'bg-good' : status.state === 'error' ? 'bg-bad' : status.state === 'starting' ? 'bg-warn animate-pulse' : 'bg-muted'}"></span>
					{STATE_TEXT[status.state]}
				</li>
				<li>
					<button type="button" class="text-xs text-muted underline-offset-2 hover:text-ink hover:underline disabled:opacity-50" disabled={checking} onclick={refresh}>
						{checking ? "Checking…" : "Check again"}
					</button>
				</li>
			</ul>
			{#if problem}
				<p class="mt-1.5 text-xs text-muted">Not usable yet: {problem}</p>
			{/if}
			{#if status.state === "error" && status.lastError}
				<p class="mt-1.5 text-xs text-bad">{status.lastError}</p>
			{/if}
		{/if}
	</div>
</div>
