<script lang="ts">
	import { n, when } from '../format';
	import { providerOf, harnessLabel, harnessVar, sortHarnesses, tokensOf, type Row } from '../usage';
	import { PROVIDERS } from '../connect';
	import type { Config } from './sections';

	let { rows, config }: { rows: Row[]; config: Config | null } = $props();

	// Everything seen in the log, unfiltered: settings describe the installation, not the current window.
	const cards = $derived(
		PROVIDERS.map((p) => {
			const rs = rows.filter((r) => providerOf(r) === p.key);
			return {
				...p,
				upstream: config?.providers[p.key] ?? `https://${p.host}`,
				n: rs.length,
				tokens: rs.reduce((s, r) => s + tokensOf(r), 0),
				errors: rs.filter((r) => r.status >= 400).length,
				last: rs.length ? Math.max(...rs.slice(-500).map((r) => r.time)) : null,
				harnesses: sortHarnesses(rs.map((r) => r.harness)),
				accounts: new Set(rs.map((r) => r.account)).size,
				models: new Set(rs.map((r) => r.model).filter(Boolean)).size
			};
		})
	);
	const inUse = $derived(cards.filter((c) => c.n > 0).sort((a, b) => b.n - a.n));
	const idle = $derived(cards.filter((c) => c.n === 0));
</script>

<h2>In use</h2>
<div class="grid">
	{#each inUse as p (p.key)}
		<div class="card provider">
			<div class="head">
				<span class="name">{p.label}</span>
				<code class="path">/{p.key}/</code>
			</div>
			<a class="upstream" href={p.upstream} target="_blank" rel="noopener">{p.upstream}</a>
			<p class="login">{p.login}.</p>
			<div class="stats">
				<div><small>Requests</small><b>{n(p.n)}</b></div>
				<div><small>Tokens</small><b>{n(p.tokens)}</b></div>
				<div><small>Errors</small><b class:error={p.errors > 0}>{n(p.errors)}</b></div>
				<div><small>Last seen</small><b>{p.last ? when(p.last) : '—'}</b></div>
			</div>
			<div class="meta">
				<span class="badges">{#each p.harnesses as h (h)}<span class="badge"><i class="dot" style="background: {harnessVar(h)}"></i>{harnessLabel(h)}</span>{/each}</span>
				<span class="sub">{p.accounts === 1 ? '1 account' : `${p.accounts} accounts`}, {p.models === 1 ? '1 model' : `${p.models} models`}</span>
			</div>
			<p class="detail"><span>{p.api}</span><span class="sub">{p.path}</span><span class="sub">Records {p.usage}</span></p>
		</div>
	{:else}
		<p class="empty">Nothing has come through the proxy yet. Connect a harness and each provider it reaches moves up here.</p>
	{/each}
</div>

{#if idle.length}
	<h2 class="spaced">Not in use</h2>
	<div class="grid">
		{#each idle as p (p.key)}
			<div class="card provider idle">
				<div class="head">
					<span class="name">{p.label}</span>
					<code class="path">/{p.key}/</code>
				</div>
				<a class="upstream" href={p.upstream} target="_blank" rel="noopener">{p.upstream}</a>
				<p class="login">{p.login}.</p>
				<p class="detail"><span>{p.api}</span><span class="sub">{p.path}</span><span class="sub">Records {p.usage}</span></p>
			</div>
		{/each}
	</div>
{/if}

<p class="caption">
	Requests are forwarded verbatim; only the model and token counts in the reply are recorded. Paths are
	<code>/&lt;harness&gt;/&lt;account&gt;/&lt;provider&gt;/…</code>{#if config}; {#each Object.entries(config.implicit_harnesses) as [h, p], i}{i ? ', ' : ''}<code>/{h}/</code> implies {p}{/each}{/if}.
	Point a tool at one from the <a href="?view=settings&section=harnesses">Harnesses</a> tab. Adding a provider is a code change; see the add-provider skill in the repository.
</p>

<style>
	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
		gap: 12px;
	}
	.grid :global(.empty) {
		grid-column: 1 / -1;
	}
	/* Cards in a row share a height (grid stretch); the API detail sits on the bottom edge so the
	   rows line up whatever wrapped above. The shared card rule adds a top margin between stacked
	   cards, which has no place in a grid. */
	.grid :global(.card) {
		margin-top: 0;
	}
	.provider {
		display: flex;
		flex-direction: column;
		gap: 10px;
	}
	.detail {
		margin-top: auto;
	}
	.provider.idle {
		opacity: 0.75;
	}
	.head {
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		gap: 10px;
	}
	.name {
		font-weight: 600;
		font-size: 14px;
	}
	.path {
		color: var(--text-muted);
	}
	.upstream {
		color: var(--text-secondary);
		font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
		font-size: 12px;
		text-decoration: none;
		word-break: break-all;
	}
	.upstream:hover {
		color: var(--accent);
		text-decoration: underline;
	}
	.login {
		margin: 0;
		font-size: 12px;
		color: var(--text-secondary);
	}
	.stats {
		display: grid;
		grid-template-columns: repeat(4, 1fr);
		gap: 8px;
		padding: 10px 0;
		border-top: 1px solid var(--border);
		border-bottom: 1px solid var(--border);
	}
	.stats small {
		display: block;
		color: var(--text-secondary);
		font-size: 10px;
	}
	.stats b {
		font-size: 15px;
		font-weight: 600;
		white-space: nowrap;
	}
	.meta {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
	}
	.badges {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}
	.badges :global(.dot) {
		margin-right: 4px;
	}
	.meta :global(.sub) {
		display: inline;
	}
	.detail {
		margin-bottom: 0;
		font-size: 12px;
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.detail :global(.sub) {
		display: block;
	}
</style>
