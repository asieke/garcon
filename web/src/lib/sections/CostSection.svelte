<script lang="ts">
	import StatTile from '../charts/StatTile.svelte';
	import TimeSeriesChart from '../charts/TimeSeriesChart.svelte';
	import CompositionBar from '../charts/CompositionBar.svelte';
	import RankedBarChart from '../charts/RankedBarChart.svelte';
	import { usd, n, pct } from '../format';
	import {
		sum,
		tokensOf,
		groupByBucket,
		modelSlots,
		modelColor,
		shortModel,
		deltaPct,
		topWithOther,
		minTime,
		type Row,
		type Granularity
	} from '../usage';
	import {
		priceFor,
		costOf,
		cacheSavingsOf,
		addCost,
		ZERO_COST,
		loadOverrides,
		saveOverrides,
		PRICE_FIELDS,
		type Price,
		type Cost
	} from '../pricing';

	let {
		rows,
		prevRows,
		buckets,
		granularity,
		days,
		now,
		since
	}: {
		rows: Row[];
		prevRows: Row[] | null;
		buckets: number[];
		granularity: Granularity;
		days: number;
		now: number;
		since: number;
	} = $props();

	let overrides = $state<Record<string, Price>>(loadOverrides());

	function modelOf(r: Row): string {
		return r.model || '(unknown)';
	}

	// Models that actually carried tokens. Error replies (rate limits, bad requests) come back with no
	// model and no usage, and there is nothing to price about them.
	const models = $derived(
		[...Map.groupBy(rows, modelOf)]
			.filter(([, rs]) => rs.some((r) => tokensOf(r) > 0))
			.map(([m]) => m)
			.sort()
	);
	const resolved = $derived(new Map(models.map((m) => [m, priceFor(m, overrides)])));
	const slots = $derived(modelSlots(models));

	/** Cost of a set of rows: grouped by model so each group is priced at its own rate. Unpriced
	 * models contribute nothing, which is reported separately rather than hidden. */
	function costRows(rs: Row[]): Cost {
		let total = ZERO_COST;
		for (const [model, group] of Map.groupBy(rs, modelOf)) {
			const p = priceFor(model, overrides).price;
			if (p) total = addCost(total, costOf(sum(group), p));
		}
		return total;
	}

	const modelGroups = $derived(
		[...Map.groupBy(rows, modelOf)]
			.map(([model, rs]) => {
				const totals = sum(rs);
				const p = resolved.get(model)?.price ?? null;
				return {
					model,
					totals,
					price: p,
					source: resolved.get(model)?.source ?? 'none',
					cost: p ? costOf(totals, p) : null,
					savings: p ? cacheSavingsOf(totals, p) : 0
				};
			})
			.sort((a, b) => (b.cost?.total ?? 0) - (a.cost?.total ?? 0) || tokensOf(b.totals) - tokensOf(a.totals))
	);

	const totalCost = $derived(modelGroups.reduce((c, g) => (g.cost ? addCost(c, g.cost) : c), ZERO_COST));
	const savings = $derived(modelGroups.reduce((s, g) => s + g.savings, 0));
	const totals = $derived(sum(rows));
	const pricedRows = $derived(modelGroups.filter((g) => g.price).reduce((s, g) => s + g.totals.n, 0));
	const unpriced = $derived(modelGroups.filter((g) => !g.price && tokensOf(g.totals) > 0));
	const coverage = $derived(totals.n ? (100 * pricedRows) / totals.n : 100);

	const prevCost = $derived(prevRows ? costRows(prevRows).total : null);
	const costDelta = $derived(prevCost === null ? null : deltaPct(totalCost.total, prevCost));

	const perRequest = $derived(pricedRows ? totalCost.total / pricedRows : 0);
	const pricedTokens = $derived(modelGroups.filter((g) => g.price).reduce((s, g) => s + tokensOf(g.totals), 0));
	const perMillion = $derived(pricedTokens ? (totalCost.total / pricedTokens) * 1_000_000 : 0);

	const firstRow = $derived(minTime(rows, now));
	const windowDays = $derived.by(() => {
		if (days === 1) return Math.max((now - since) / 86_400_000, 1 / 24);
		if (days) return days;
		return Math.max(1, (now - firstRow) / 86_400_000);
	});
	const runRate = $derived((totalCost.total / windowDays) * 30);

	// Cost over time, stacked by model: the five costliest models keep their own colour, the rest fold into Other.
	const grouped = $derived(groupByBucket(rows, granularity));
	const topModels = $derived(
		topWithOther([...Map.groupBy(rows, modelOf)], (rs) => costRows(rs).total, 5).filter((t) => costRows(t.rows).total > 0)
	);
	const costSeries = $derived.by(() => {
		const keyOf = new Map<string, string>();
		for (const t of topModels) for (const m of new Set(t.rows.map(modelOf))) keyOf.set(m, t.key);
		const values = new Map(topModels.map((t) => [t.key, buckets.map(() => 0)]));
		buckets.forEach((b, i) => {
			for (const [model, rs] of Map.groupBy(grouped.get(b) ?? [], modelOf)) {
				const p = resolved.get(model)?.price;
				const key = keyOf.get(model);
				const arr = key === undefined ? undefined : values.get(key);
				if (!p || !arr) continue;
				arr[i] += costOf(sum(rs), p).total;
			}
		});
		return topModels.map((t) => ({
			key: t.key,
			label: t.folded ? 'Other' : shortModel(t.key),
			color: t.folded ? 'var(--text-muted)' : modelColor(slots, t.key),
			values: values.get(t.key) ?? []
		}));
	});

	const byAccount = $derived(
		[...Map.groupBy(rows, (r) => r.account)]
			.map(([account, rs]) => ({ label: account, value: costRows(rs).total, sublabel: `${n(rs.length)} req` }))
			.sort((a, b) => b.value - a.value)
	);

	function setPrice(model: string, key: keyof Price, raw: string) {
		const v = Number.parseFloat(raw);
		if (!Number.isFinite(v) || v < 0) return;
		const base = resolved.get(model)?.price ?? { input: 0, cacheRead: 0, cacheWrite: 0, output: 0 };
		overrides = { ...overrides, [model]: { ...base, [key]: v } };
		saveOverrides(overrides);
	}
	function resetPrice(model: string) {
		const next = { ...overrides };
		delete next[model];
		overrides = next;
		saveOverrides(overrides);
	}
	function sourceLabel(model: string): string {
		const r = resolved.get(model);
		if (!r || r.source === 'none') return 'no price';
		if (r.source === 'override') return 'custom';
		return r.rule ? `list: ${r.rule.label}` : 'list';
	}
</script>

<section class="tiles">
	<StatTile label="Estimated API cost" value={usd(totalCost.total)} delta={costDelta} deltaGoodWhen="down" accent="var(--series-1)" />
	<StatTile label="Saved by caching" value={usd(savings)} accent="var(--series-3)" />
	<StatTile label="Per request" value={pricedRows ? usd(perRequest) : '—'} accent="var(--series-1)" />
	<StatTile label="Per 1M tokens, blended" value={pricedTokens ? usd(perMillion) : '—'} accent="var(--series-1)" />
	<StatTile label="30-day run rate" value={usd(runRate)} accent="var(--series-2)" />
</section>
<p class="caption lead">
	What this usage would cost at published pay-as-you-go list prices. Subscriptions bill differently, so read this as
	the value consumed, not a bill. Cache writes use the 5-minute rate Claude Code defaults to; cache reads are what
	the provider actually charged. Savings compare cache reads with paying full input price for the same tokens.
</p>
{#if unpriced.length}
	<p class="warn">
		{pct(coverage)} of requests are priced. No price is known for
		{unpriced.map((g) => `${g.model} (${n(g.totals.n)} req, ${n(tokensOf(g.totals))} tokens)`).join(', ')}. Set one in
		the pricing table below.
	</p>
{/if}

<h2 class="spaced">Cost over time by model</h2>
<TimeSeriesChart {buckets} {granularity} mode="stacked-area" formatValue={usd} series={costSeries} />

<h2 class="spaced">Where the money goes</h2>
<CompositionBar
	formatValue={usd}
	segments={[
		{ key: 'input', label: 'Input', value: totalCost.input, color: 'var(--series-3)' },
		{ key: 'cache_read', label: 'Cache read', value: totalCost.cacheRead, color: 'var(--series-4)' },
		{ key: 'cache_write', label: 'Cache write', value: totalCost.cacheWrite, color: 'var(--series-5)' },
		{ key: 'output', label: 'Output', value: totalCost.output, color: 'var(--series-6)' }
	]}
/>
<p class="caption">Output tokens cost five times input on every model here, so a small share of output tokens can be most of the bill.</p>

<h2 class="spaced">Cost by account</h2>
<RankedBarChart items={byAccount} formatValue={usd} color="var(--series-1)" />

<h2 class="spaced">Cost by model</h2>
<div class="scroll">
	<table>
		<thead>
			<tr>
				<th>Model</th>
				<th class="num">Requests</th>
				<th class="num">Tokens</th>
				<th class="num">Input $</th>
				<th class="num">Cache read $</th>
				<th class="num">Cache write $</th>
				<th class="num">Output $</th>
				<th class="num">Total</th>
				<th class="num">Share</th>
				<th class="num">Per request</th>
				<th class="num">Saved by cache</th>
			</tr>
		</thead>
		<tbody>
			{#each modelGroups as g (g.model)}
				<tr>
					<td><i class="dot" style="background: {modelColor(slots, g.model)}"></i>{g.model}{#if !g.price && tokensOf(g.totals) > 0}<span class="badge">unpriced</span>{/if}</td>
					<td class="num">{n(g.totals.n)}</td>
					<td class="num">{n(tokensOf(g.totals))}</td>
					<td class="num">{g.cost ? usd(g.cost.input) : '—'}</td>
					<td class="num">{g.cost ? usd(g.cost.cacheRead) : '—'}</td>
					<td class="num">{g.cost ? usd(g.cost.cacheWrite) : '—'}</td>
					<td class="num">{g.cost ? usd(g.cost.output) : '—'}</td>
					<td class="num"><b>{g.cost ? usd(g.cost.total) : '—'}</b></td>
					<td class="num">{g.cost && totalCost.total ? pct((100 * g.cost.total) / totalCost.total) : '—'}</td>
					<td class="num">{g.cost ? usd(g.cost.total / g.totals.n) : '—'}</td>
					<td class="num">{g.price ? usd(g.savings) : '—'}</td>
				</tr>
			{:else}
				<tr><td colspan="11">No requests in this window.</td></tr>
			{/each}
		</tbody>
	</table>
</div>

<h2 class="spaced">Pricing</h2>
<p class="caption">USD per million tokens. Edit a cell to override the list price for that model in this browser; the proxy never stores prices.</p>
<div class="scroll">
	<table class="pricing">
		<thead>
			<tr>
				<th>Model</th>
				<th>Source</th>
				{#each PRICE_FIELDS as f (f.key)}<th class="num">{f.label}</th>{/each}
				<th></th>
			</tr>
		</thead>
		<tbody>
			{#each models as model (model)}
				{@const r = resolved.get(model)}
				<tr>
					<td>{model}</td>
					<td class="muted">{sourceLabel(model)}</td>
					{#each PRICE_FIELDS as f (f.key)}
						<td class="num">
							<input
								type="number"
								min="0"
								step="0.01"
								aria-label="{f.label} price for {model}"
								value={r?.price ? r.price[f.key] : ''}
								placeholder="—"
								onchange={(e) => setPrice(model, f.key, e.currentTarget.value)}
							/>
						</td>
					{/each}
					<td>{#if r?.source === 'override'}<button onclick={() => resetPrice(model)}>Reset</button>{/if}</td>
				</tr>
			{:else}
				<tr><td colspan="7">No models in this window.</td></tr>
			{/each}
		</tbody>
	</table>
</div>
<p class="caption">
	List prices read on 2026-09-13 from the Anthropic and OpenAI pricing pages. Cache read on Claude Fable 5.1 is
	0.025× input; every other Claude model is 0.1×. OpenAI has no cache-write charge.
</p>

<style>
	.tiles {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
		gap: 12px;
		margin-bottom: 16px;
	}
	h2 {
		font-size: 15px;
		margin: 0 0 12px;
		opacity: 0.85;
	}
	h2.spaced {
		margin-top: 32px;
	}
	.caption {
		color: var(--text-muted);
		font-size: 12px;
		margin: 8px 0 0;
	}
	.caption.lead {
		max-width: 72ch;
		line-height: 1.5;
	}
	.warn {
		font-size: 12px;
		color: var(--text-secondary);
		border: 1px dashed var(--border);
		border-radius: 8px;
		padding: 10px 12px;
		margin: 12px 0 0;
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
	}
	th {
		font-weight: 500;
		color: var(--text-secondary);
		font-variant-numeric: normal;
	}
	.num {
		text-align: right;
	}
	.muted {
		color: var(--text-muted);
	}
	.dot {
		display: inline-block;
		width: 8px;
		height: 8px;
		border-radius: 2px;
		margin-right: 8px;
		vertical-align: middle;
	}
	.badge {
		margin-left: 8px;
		font-size: 10px;
		font-weight: 600;
		letter-spacing: 0.03em;
		color: var(--text-muted);
		border: 1px solid var(--border);
		border-radius: 4px;
		padding: 1px 5px;
	}
	.pricing input {
		font: inherit;
		font-size: 12px;
		width: 84px;
		padding: 3px 6px;
		border-radius: 4px;
		border: 1px solid var(--border);
		background: var(--surface);
		color: inherit;
		text-align: right;
		font-variant-numeric: tabular-nums;
	}
	.pricing button {
		font: inherit;
		font-size: 11px;
		padding: 2px 8px;
		border-radius: 4px;
		border: 1px solid var(--border);
		background: none;
		color: inherit;
		cursor: pointer;
	}
</style>
