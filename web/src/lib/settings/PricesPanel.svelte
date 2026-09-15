<script lang="ts">
	import { n, when, usd } from '../format';
	import { tokensOf, type Row } from '../usage';
	import { priceFor, PRICE_FIELDS, FALLBACK_AS_OF } from '../pricing';
	import { prices, loadPrices } from '../prices.svelte';

	let { rows }: { rows: Row[] } = $props();

	const catalog = $derived(prices.catalog);
	const fetched = $derived(Boolean(catalog?.fetched_at));
	const catalogSize = $derived(catalog ? Object.keys(catalog.models).length : 0);

	// One line per model that carried tokens, priced the way the Cost view prices it.
	const models = $derived(
		[...Map.groupBy(rows.filter((r) => r.model), (r) => r.model)]
			.filter(([, rs]) => rs.some((r) => tokensOf(r) > 0))
			.map(([model, rs]) => ({ model, tokens: rs.reduce((s, r) => s + tokensOf(r), 0), resolved: priceFor(model, catalog) }))
			.sort((a, b) => b.tokens - a.tokens)
	);
	const unpriced = $derived(models.filter((m) => m.resolved.source === 'none'));
</script>

<div class="card">
	<div class="kv">
		<div><small>Source</small>{#if catalog?.source}<a href={catalog.source} target="_blank" rel="noopener">OpenRouter public model list</a>{:else}<span>OpenRouter public model list</span>{/if}</div>
		<div><small>Last fetched</small><span>{fetched ? when(catalog!.fetched_at) : 'never'}</span>{#if fetched}<span class="sub">{n(catalogSize)} models, refreshed daily while the dashboard is open</span>{/if}</div>
		<div>
			<small>Status</small>
			{#if prices.problem}<span class="error">{prices.problem}</span>{#if !fetched}<span class="sub">Using the built-in list as of {FALLBACK_AS_OF}</span>{/if}
			{:else if catalog?.error}<span class="error">{catalog.error}</span><span class="sub">at {when(catalog.error_at ?? 0)}{fetched ? '; using the last good catalogue' : `; using the built-in list as of ${FALLBACK_AS_OF}`}</span>
			{:else if fetched}<span>Up to date</span>
			{:else}<span>Not fetched yet; using the built-in list as of {FALLBACK_AS_OF}</span>{/if}
		</div>
	</div>
	<div class="actions" style="margin-top: 14px; margin-bottom: 0">
		<button class="primary" onclick={() => loadPrices(true)} disabled={prices.loading}>{prices.loading ? 'Fetching…' : 'Refresh now'}</button>
		<span class="msg">A public, unauthenticated request from the proxy to openrouter.ai; nothing about your usage is sent.</span>
	</div>
</div>
<p class="caption">
	Prices are pay-as-you-go API list rates in USD per million tokens, as OpenRouter publishes them for each vendor's models. Subscriptions
	(Claude Max, ChatGPT Pro) bill differently, so the Cost view reads as value consumed, not a bill. Anthropic cache writes are the 5-minute
	rate; OpenAI cache writes cost plain input. Rates are not editable: to price a model differently, the estimate would stop meaning "list price".
</p>

<h2 class="spaced">Rates in use</h2>
<div class="scroll">
	<table>
		<thead>
			<tr>
				<th>Model</th>
				<th>Catalogue entry</th>
				{#each PRICE_FIELDS as f (f.key)}<th class="num">{f.label}</th>{/each}
				<th class="num">Tokens on record</th>
			</tr>
		</thead>
		<tbody>
			{#each models as m (m.model)}
				{@const r = m.resolved}
				<tr>
					<td><code>{m.model}</code></td>
					<td>
						{#if r.source === 'catalog'}<code>{r.id}</code>
						{:else if r.source === 'list'}<span class="badge">built-in</span> <span class="sub">{r.rule.label}, as of {r.rule.asOf}</span>
						{:else}<span class="badge warn">unpriced</span>{/if}
					</td>
					{#each PRICE_FIELDS as f (f.key)}<td class="num">{r.price ? usd(r.price[f.key]) : '—'}</td>{/each}
					<td class="num">{n(m.tokens)}</td>
				</tr>
			{:else}
				<tr><td colspan="7">No priced usage yet.</td></tr>
			{/each}
		</tbody>
	</table>
</div>
{#if unpriced.length}
	<p class="caption">
		{unpriced.map((m) => m.model).join(', ')}: not in the catalogue and not in the built-in list, so their tokens count toward nothing on the Cost view.
	</p>
{/if}
