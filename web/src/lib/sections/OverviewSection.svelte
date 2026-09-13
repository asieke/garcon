<script lang="ts">
	import StatTile from '../charts/StatTile.svelte';
	import TimeSeriesChart from '../charts/TimeSeriesChart.svelte';
	import { n, ms as msFmt, pct } from '../format';
	import { deltaPct, errorRate, cacheHitRate, avgMs, tokensOf, type Totals, type Granularity } from '../usage';

	let {
		totals,
		prevTotals,
		buckets,
		granularity,
		harnessSeries,
		errorCounts,
		tokensTrend,
		latencyTrend
	}: {
		totals: Totals;
		prevTotals: Totals | null;
		buckets: number[];
		granularity: Granularity;
		harnessSeries: { key: string; label: string; color: string; values: number[] }[];
		errorCounts: number[];
		tokensTrend: number[];
		latencyTrend: number[];
	} = $props();

	const requestsTrend = $derived(buckets.map((_, i) => harnessSeries.reduce((s, h) => s + (h.values[i] ?? 0), 0)));

	const reqDelta = $derived(prevTotals ? deltaPct(totals.n, prevTotals.n) : null);
	const tokDelta = $derived(prevTotals ? deltaPct(tokensOf(totals), tokensOf(prevTotals)) : null);
	const errDelta = $derived(prevTotals ? deltaPct(errorRate(totals), errorRate(prevTotals)) : null);
	const cacheDelta = $derived(prevTotals ? deltaPct(cacheHitRate(totals), cacheHitRate(prevTotals)) : null);
	const latDelta = $derived(prevTotals ? deltaPct(avgMs(totals), avgMs(prevTotals)) : null);
</script>

<section class="tiles">
	<StatTile label="Requests" value={n(totals.n)} delta={reqDelta} trend={requestsTrend} accent="var(--series-1)" />
	<StatTile label="Total tokens" value={n(tokensOf(totals))} delta={tokDelta} trend={tokensTrend} accent="var(--series-3)" />
	<StatTile
		label="Cache hit rate"
		value={pct(cacheHitRate(totals))}
		delta={cacheDelta}
		deltaGoodWhen="up"
		accent="var(--series-3)"
	/>
	<StatTile
		label="Error rate"
		value={pct(errorRate(totals), 1)}
		delta={errDelta}
		deltaGoodWhen="down"
		accent="var(--status-critical)"
	/>
	<StatTile
		label="Avg latency"
		value={msFmt(avgMs(totals))}
		delta={latDelta}
		deltaGoodWhen="down"
		trend={latencyTrend}
		accent="var(--series-2)"
	/>
</section>

<h2>Requests by harness</h2>
<TimeSeriesChart
	{buckets}
	{granularity}
	mode="stacked-area"
	formatValue={n}
	{errorCounts}
	series={harnessSeries}
/>
<p class="caption">Red dots mark buckets with errors.</p>

<style>
	.tiles {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
		gap: 12px;
		margin-bottom: 28px;
	}
	h2 {
		font-size: 15px;
		margin: 0 0 12px;
		opacity: 0.85;
	}
	.caption {
		color: var(--text-muted);
		font-size: 12px;
		margin: 8px 0 0;
	}
</style>
