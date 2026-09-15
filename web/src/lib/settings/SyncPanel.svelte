<script lang="ts">
	import { n, when } from '../format';
	import { keyProblem, DOCS_URL, type SettingsResponse } from '../sync';

	let { pollMs }: { pollMs: number } = $props();

	// The master switch, this device's name, and the project it talks to. The key is write-only:
	// the proxy reports whether one is stored and never sends it back.
	let sync = $state<SettingsResponse | null>(null);
	let syncError = $state(false);
	let form = $state({ sync_enabled: false, device_name: '', url: '', key: '' });
	let dirty = $state(false);
	let saving = $state(false);
	let saveMessage = $state<{ ok: boolean; text: string } | null>(null);
	function fill(data: SettingsResponse) {
		form = { sync_enabled: data.settings.sync_enabled, device_name: data.settings.device_name, url: data.settings.url, key: '' };
	}
	async function loadSync() {
		try {
			const r = await fetch('/api/settings');
			if (!r.ok) throw new Error(String(r.status));
			const data: SettingsResponse = await r.json();
			sync = data;
			if (!dirty) fill(data);
		} catch {
			syncError = true;
		}
	}
	$effect(() => {
		loadSync();
		const timer = setInterval(loadSync, pollMs);
		return () => clearInterval(timer);
	});
	async function saveSync(overrides: Record<string, unknown> = {}) {
		const body: Record<string, unknown> = { sync_enabled: form.sync_enabled, device_name: form.device_name, url: form.url, ...overrides };
		if (form.key.trim() && !('key' in overrides)) body.key = form.key.trim();
		saving = true;
		saveMessage = null;
		try {
			const r = await fetch('/api/settings', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
			const text = await r.text();
			if (!r.ok) {
				saveMessage = { ok: false, text: text.trim() };
				return;
			}
			sync = JSON.parse(text);
			dirty = false;
			fill(sync!);
			saveMessage = { ok: true, text: sync!.settings.sync_enabled ? 'Saved. Sync is on.' : 'Saved. Sync is off.' };
		} catch {
			saveMessage = { ok: false, text: 'Could not reach the proxy.' };
		} finally {
			saving = false;
		}
	}
	const keyHint = $derived(keyProblem(form.key));
	const touch = () => (dirty = true);
</script>

{#if syncError}
	<p class="empty">Could not read settings. Run <code>garcon doctor</code> to check your installation.</p>
{:else if !sync}
	<p class="caption">Loading…</p>
{:else}
	<div class="card">
		<label class="switch"><input type="checkbox" bind:checked={form.sync_enabled} onchange={touch} /> Enable Supabase sync</label>
		<p class="caption">
			Off: no network calls. On: rows are exchanged with your Supabase project, and the Machines view and Device filter light up. First machine:
			<code>garcon connect-supabase --create-project --name "work laptop"</code> (needs the Supabase CLI). Already have a project? Paste its URL and secret key below and give this machine a distinct name. The project table must exist before saving.
			<a href={DOCS_URL} target="_blank" rel="noopener">Setup guide</a>
		</p>
		<div class="fields">
			<label>Device name <input type="text" placeholder="work laptop" bind:value={form.device_name} oninput={touch} /></label>
			<label>Project URL <input type="text" placeholder="https://abcdefghijklmnopqrst.supabase.co" bind:value={form.url} oninput={touch} /></label>
			<label>Secret key <input type="password" placeholder={sync.settings.key_set ? 'stored; leave blank to keep (needed again if the URL changes)' : 'sb_secret_…'} bind:value={form.key} oninput={touch} autocomplete="off" /></label>
		</div>
		{#if keyHint}<p class="caption error">{keyHint}</p>{/if}
		<div class="actions">
			<button class="primary" onclick={() => saveSync()} disabled={saving || !!keyHint}>{saving ? 'Saving…' : 'Save'}</button>
			{#if sync.settings.key_set}<button class="link" onclick={() => saveSync({ sync_enabled: false, key: '' })} disabled={saving}>Forget key</button>{/if}
			{#if saveMessage}<span class="msg" class:error={!saveMessage.ok}>{saveMessage.text}</span>{/if}
		</div>
	</div>

	<h2 class="spaced">Status</h2>
	<div class="card">
		<div class="kv">
			<div><small>Device id</small><code>{sync.status.device_id}</code></div>
			{#if sync.settings.sync_enabled}
				<div><small>Pushed</small><span>{n(sync.status.pushed)} rows{sync.status.pending ? `, ${n(sync.status.pending)} pending` : ''}</span></div>
				<div>
					<small>Last push</small>
					<span>{sync.status.last_push_ok ? when(sync.status.last_push_ok) : 'never'}</span>
					{#if sync.status.last_push_error}<span class="sub error">{sync.status.last_push_error}</span>{/if}
				</div>
				<div>
					<small>Last pull</small>
					<span>{sync.status.last_pull_ok ? when(sync.status.last_pull_ok) : 'never'}</span>
					{#if sync.status.last_pull_error}<span class="sub error">{sync.status.last_pull_error}</span>{/if}
				</div>
				<div><small>Rows from other devices</small><span>{n(sync.status.remote_rows)}</span></div>
			{:else}
				<div><small>Status</small><span>Off</span></div>
			{/if}
		</div>
	</div>
	{#if sync.settings.sync_enabled && sync.status.devices.length}
		<h2 class="spaced">Devices in this project</h2>
		<div class="scroll">
			<table>
				<thead><tr><th>Device</th><th class="num">Rows</th><th>Last request</th></tr></thead>
				<tbody>
					<tr><td>{sync.settings.device_name} <span class="sub">this device</span></td><td class="num">{n(sync.status.pushed + sync.status.pending)}</td><td>—</td></tr>
					{#each sync.status.devices as d (d.device_id)}
						<tr><td>{d.device}</td><td class="num">{n(d.rows)}</td><td>{when(d.last_time)}</td></tr>
					{/each}
				</tbody>
			</table>
		</div>
		<p class="caption">Explore usage per device on the <a href="?view=machines">Machines</a> view.</p>
	{/if}
{/if}

<style>
	.switch {
		display: inline-flex;
		align-items: center;
		gap: 8px;
		font-size: 14px;
		font-weight: 600;
		cursor: pointer;
	}
	.switch input {
		width: 16px;
		height: 16px;
		margin: 0;
		accent-color: var(--accent);
	}
	.switch + :global(.caption) {
		margin-bottom: 14px;
	}
</style>
