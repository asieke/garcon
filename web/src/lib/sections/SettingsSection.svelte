<script lang="ts">
	import StatTile from '../charts/StatTile.svelte';
	import { n, when, duration, usd } from '../format';
	import { HARNESSES, harnessLabel, harnessVar, providerOf, sortHarnesses, type Row } from '../usage';
	import { PROVIDERS, HARNESS_PROVIDERS, baseUrl, snippetsFor } from '../connect';
	import { loadOverrides, saveOverrides, PRICE_FIELDS, PRICE_RULES, type Price } from '../pricing';

	let { rows, pollMs, now }: { rows: Row[]; pollMs: number; now: number } = $props();

	type Config = {
		listen: string;
		data: string;
		rows: number;
		bytes: number;
		started: number;
		providers: Record<string, string>;
		implicit_harnesses: Record<string, string>;
	};
	let config = $state<Config | null>(null);
	let configError = $state(false);
	$effect(() => {
		fetch('/api/config')
			.then((r) => (r.ok ? r.json() : Promise.reject(r.status)))
			.then((c: Config) => (config = c))
			.catch(() => (configError = true));
	});

	const base = $derived(config ? `http://${config.listen}` : 'http://127.0.0.1:4141');

	// Everything seen in the log, unfiltered: settings describe the installation, not the current window.
	const byProvider = $derived(
		new Map(
			PROVIDERS.map((p) => {
				const rs = rows.filter((r) => providerOf(r) === p.key);
				return [p.key, { n: rs.length, last: rs.length ? Math.max(...rs.slice(-500).map((r) => r.time)) : null }];
			})
		)
	);
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

	// Browser-side state.
	let overrides = $state<Record<string, Price>>(loadOverrides());
	function clearOverrides() {
		overrides = {};
		saveOverrides(overrides);
	}
	function clearOne(model: string) {
		const next = { ...overrides };
		delete next[model];
		overrides = next;
		saveOverrides(overrides);
	}
	const overrideList = $derived(Object.entries(overrides).sort(([a], [b]) => a.localeCompare(b)));
</script>

<h2>Proxy</h2>
{#if configError}
	<p class="empty">Could not read /api/config. The proxy may be an older build; rebuild with <code>./install.sh</code>.</p>
{:else if !config}
	<p class="caption">Loading…</p>
{:else}
	<section class="tiles">
		<StatTile label="Requests on record" value={n(config.rows)} accent="var(--series-1)" />
		<StatTile label="Usage log size" value={config.bytes >= 1_048_576 ? `${(config.bytes / 1_048_576).toFixed(1)} MB` : `${Math.round(config.bytes / 1024)} KB`} accent="var(--series-3)" />
		<StatTile label="Running for" value={duration(now - config.started)} accent="var(--series-3)" />
		<StatTile label="Dashboard refresh" value={`${Math.round(pollMs / 1000)}s`} accent="var(--series-1)" />
	</section>
	<div class="kv">
		<div><small>Listening on</small><code>{base}</code></div>
		<div><small>Usage log</small><code>{config.data}</code></div>
		<div><small>Raw data</small><code>{base}/api/usage</code></div>
		<div><small>Started</small><span>{when(config.started)}</span></div>
	</div>
	<p class="caption">
		The proxy binds to loopback only, stores no credentials, and forwards every request unchanged. Each line of the
		usage log is one completion call: account, model, status, latency and the token counts the provider reported.
	</p>
{/if}

<h2 class="spaced">Providers</h2>
<div class="scroll">
	<table>
		<thead>
			<tr>
				<th>Provider</th>
				<th>Upstream</th>
				<th>URL segment</th>
				<th>API recorded</th>
				<th>Usage fields</th>
				<th class="num">Requests</th>
				<th>Last seen</th>
			</tr>
		</thead>
		<tbody>
			{#each PROVIDERS as p (p.key)}
				{@const stat = byProvider.get(p.key)}
				<tr>
					<td>{p.label}</td>
					<td><code>{config?.providers[p.key] ?? `https://${p.host}`}</code></td>
					<td><code>/{p.key}/</code></td>
					<td>{p.api}<span class="sub">{p.path}</span></td>
					<td>{p.usage}</td>
					<td class="num">{n(stat?.n ?? 0)}</td>
					<td>{stat?.last ? when(stat.last) : '—'}</td>
				</tr>
			{/each}
		</tbody>
	</table>
</div>
<p class="caption">
	Every request path is <code>/&lt;harness&gt;/&lt;account&gt;/&lt;provider&gt;/…</code>. Claude Code and Codex talk to one
	provider each, so their URLs omit the segment:
	{#if config}{#each Object.entries(config.implicit_harnesses) as [h, p], i}{i ? ', ' : ''}<code>/{h}/</code> implies {p}{/each}.{/if}
</p>

<h2 class="spaced">Harnesses</h2>
<div class="harnesses">
	{#each HARNESSES as h (h.key)}
		<span class="chip"><i style="background: {harnessVar(h.key)}"></i>{h.label}<span class="sub">{(HARNESS_PROVIDERS[h.key] ?? []).join(', ')}</span></span>
	{/each}
	<span class="chip"><i style="background: var(--text-muted)"></i>Anything else<span class="sub">any provider</span></span>
</div>
<p class="caption">Known harnesses get a fixed colour across every chart. Any other name in a base URL is accepted and drawn in grey.</p>

<h2 class="spaced">Connections seen</h2>
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
				<tr><td colspan="7">Nothing has come through the proxy yet.</td></tr>
			{/each}
		</tbody>
	</table>
</div>

<h2 class="spaced">Connect a harness</h2>
<div class="builder">
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
<p class="caption">
	The account is only a label the proxy records; use the login the harness actually signs in with so usage lines up
	with the right subscription. Verify with one short call and a new row in the Logs tab.
</p>

<h2 class="spaced">This browser</h2>
<div class="kv">
	<div>
		<small>Custom prices</small>
		{#if overrideList.length}
			<ul class="plain">
				{#each overrideList as [model, p] (model)}
					<li><code>{model}</code> {PRICE_FIELDS.map((f) => `${f.label} ${usd(p[f.key])}`).join(', ')} per 1M <button class="link" onclick={() => clearOne(model)}>reset</button></li>
				{/each}
			</ul>
			<button onclick={clearOverrides}>Reset all to list prices</button>
		{:else}
			<span>None. {PRICE_RULES.length} list-price rules apply; edit any model on the Cost tab.</span>
		{/if}
	</div>
	<div><small>Theme</small><span>Follows the operating system's light or dark setting.</span></div>
	<div><small>Filters</small><span>Time range, harness and account filters are per page load and scope every tab except this one.</span></div>
</div>

<style>
	h2 {
		font-size: 15px;
		margin: 0 0 12px;
		opacity: 0.85;
	}
	h2.spaced {
		margin-top: 32px;
	}
	.tiles {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
		gap: 12px;
		margin-bottom: 16px;
	}
	.caption {
		color: var(--text-muted);
		font-size: 12px;
		margin: 8px 0 0;
		max-width: 80ch;
	}
	.empty {
		color: var(--text-muted);
		font-size: 13px;
		border: 1px dashed var(--border);
		border-radius: 8px;
		padding: 16px;
	}
	code {
		font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
		font-size: 12px;
		word-break: break-all;
	}
	.kv {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
		gap: 12px 24px;
		font-size: 13px;
	}
	.kv small {
		display: block;
		color: var(--text-secondary);
		font-size: 11px;
		margin-bottom: 2px;
	}
	.scroll {
		overflow-x: auto;
	}
	table {
		border-collapse: collapse;
		width: 100%;
		white-space: nowrap;
	}
	th,
	td {
		text-align: left;
		padding: 6px 10px;
		border-bottom: 1px solid var(--border);
		font-variant-numeric: tabular-nums;
		vertical-align: top;
	}
	th {
		font-weight: 500;
		color: var(--text-secondary);
		font-variant-numeric: normal;
	}
	.num {
		text-align: right;
	}
	.sub {
		display: block;
		color: var(--text-muted);
		font-size: 11px;
	}
	.error {
		color: var(--status-critical);
	}
	.dot,
	.chip i {
		display: inline-block;
		width: 8px;
		height: 8px;
		border-radius: 2px;
		margin-right: 6px;
		vertical-align: middle;
	}
	.harnesses {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
	}
	.chip {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		font-size: 12px;
		border: 1px solid var(--border);
		border-radius: 8px;
		padding: 6px 10px;
		background: var(--surface);
	}
	.chip .sub {
		display: inline;
		margin-left: 2px;
	}
	.builder {
		display: flex;
		flex-wrap: wrap;
		gap: 12px;
		margin-bottom: 12px;
	}
	.builder label {
		display: flex;
		flex-direction: column;
		gap: 4px;
		font-size: 11px;
		color: var(--text-secondary);
	}
	select,
	input[type='text'] {
		font: inherit;
		font-size: 13px;
		padding: 4px 10px;
		border-radius: 6px;
		border: 1px solid var(--border);
		background: var(--surface);
		color: var(--text-primary);
		min-width: 180px;
	}
	.snippet {
		border: 1px solid var(--border);
		border-radius: 8px;
		background: var(--surface);
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
	button {
		font: inherit;
		font-size: 12px;
		padding: 3px 10px;
		border-radius: 6px;
		border: 1px solid var(--border);
		background: none;
		color: inherit;
		cursor: pointer;
	}
	button.link {
		border: none;
		padding: 0 4px;
		color: var(--text-muted);
		text-decoration: underline;
	}
	.plain {
		list-style: none;
		margin: 0 0 8px;
		padding: 0;
	}
	.plain li {
		margin-bottom: 4px;
	}
</style>
