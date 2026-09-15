<script lang="ts">
	import { n, when } from '../format';
	import { HARNESSES, harnessLabel, harnessVar, providerOf, sortHarnesses, type Row } from '../usage';
	import { PROVIDERS, HARNESS_PROVIDERS, baseUrl, snippetsFor } from '../connect';
	import type { Config } from './sections';

	let { rows, config }: { rows: Row[]; config: Config | null } = $props();
	const base = $derived(config ? `http://${config.listen}` : 'http://127.0.0.1:4141');

	const connections = $derived.by(() => {
		const map = new Map<string, { harness: string; account: string; providers: Set<string>; n: number; last: number; errors: number }>();
		for (const r of rows) {
			const key = `${r.harness}|${r.account}`;
			let c = map.get(key);
			if (!c) {
				c = { harness: r.harness, account: r.account, providers: new Set(), n: 0, last: 0, errors: 0 };
				map.set(key, c);
			}
			c.providers.add(providerOf(r));
			c.n++;
			if (r.time > c.last) c.last = r.time;
			if (r.status >= 400) c.errors++;
		}
		const order = new Map(sortHarnesses([...map.values()].map((c) => c.harness)).map((h, i) => [h, i]));
		return [...map.values()].sort((a, b) => (order.get(a.harness) ?? 99) - (order.get(b.harness) ?? 99) || a.account.localeCompare(b.account));
	});

	// Connect-a-harness recipe builder.
	let harness = $state('claude');
	let customHarness = $state('');
	let provider = $state('anthropic');
	let account = $state('');
	const harnessKey = $derived(harness === 'other' ? customHarness.trim().toLowerCase().replace(/[^a-z0-9_-]/g, '') || 'myagent' : harness);
	const allowedProviders = $derived(HARNESS_PROVIDERS[harness] ?? PROVIDERS.map((p) => p.key));
	$effect(() => {
		if (!allowedProviders.includes(provider)) provider = allowedProviders[0];
	});
	const snippets = $derived(snippetsFor(base, harnessKey, provider, account.trim()));
	let copied = $state<string | null>(null);
	async function copy(text: string, id: string) {
		try {
			await navigator.clipboard.writeText(text);
			copied = id;
			setTimeout(() => (copied = null), 1500);
		} catch {
			copied = null;
		}
	}
</script>

<h2>Connected</h2>
<div class="scroll">
	<table>
		<thead>
			<tr>
				<th>Harness</th>
				<th>Account</th>
				<th>Providers</th>
				<th>Base URL</th>
				<th class="num">Requests</th>
				<th class="num">Errors</th>
				<th>Last seen</th>
			</tr>
		</thead>
		<tbody>
			{#each connections as c (c.harness + c.account)}
				<tr>
					<td><i class="dot" style="background: {harnessVar(c.harness)}"></i>{harnessLabel(c.harness)}</td>
					<td>{c.account}</td>
					<td>{[...c.providers].sort().join(', ')}</td>
					<td><code>{baseUrl(base, c.harness, c.account, [...c.providers][0])}</code></td>
					<td class="num">{n(c.n)}</td>
					<td class="num" class:error={c.errors > 0}>{n(c.errors)}</td>
					<td>{when(c.last)}</td>
				</tr>
			{:else}
				<tr><td colspan="7">Nothing has come through the proxy yet. Connect one below.</td></tr>
			{/each}
		</tbody>
	</table>
</div>
<p class="caption">A connection is a harness and account pair, identified by the path the tool was pointed at. Known harnesses:
	{#each HARNESSES as h, i (h.key)}{i ? ', ' : ''}<span><i class="dot" style="background: {harnessVar(h.key)}"></i>{h.label} ({(HARNESS_PROVIDERS[h.key] ?? []).join(', ')})</span>{/each}; any other name works with any provider.
</p>

<h2 class="spaced">Connect a harness</h2>
<div class="card">
	<div class="fields">
		<label>Harness
			<select bind:value={harness}>
				{#each HARNESSES as h (h.key)}<option value={h.key}>{h.label}</option>{/each}
				<option value="other">Other…</option>
			</select>
		</label>
		{#if harness === 'other'}
			<label>Name <input type="text" placeholder="myagent" bind:value={customHarness} /></label>
		{/if}
		<label>Provider
			<select bind:value={provider} disabled={allowedProviders.length === 1}>
				{#each allowedProviders as p (p)}<option value={p}>{PROVIDERS.find((x) => x.key === p)?.label ?? p}</option>{/each}
			</select>
		</label>
		<label>Account <input type="text" placeholder="me@example.com" bind:value={account} /></label>
	</div>
	{#each snippets as s, i (s.title)}
		<div class="snippet">
			<div class="snippet-head">
				<span>{s.title}</span>
				<button onclick={() => copy(s.text, `${harnessKey}-${i}`)}>{copied === `${harnessKey}-${i}` ? 'Copied' : 'Copy'}</button>
			</div>
			<pre>{s.text}</pre>
			{#if s.note}<p class="caption">{s.note}</p>{/if}
		</div>
	{/each}
	<p class="caption">Use the login the harness signs in with. Verify with one call and a new row in Logs.</p>
</div>

<style>
	.snippet {
		border: 1px solid var(--border);
		border-radius: 8px;
		background: var(--page);
		padding: 10px 12px;
		margin-top: 10px;
	}
	.snippet-head {
		display: flex;
		justify-content: space-between;
		align-items: center;
		font-size: 12px;
		color: var(--text-secondary);
		margin-bottom: 6px;
	}
	pre {
		margin: 0;
		font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
		font-size: 12px;
		white-space: pre-wrap;
		word-break: break-all;
	}
</style>
