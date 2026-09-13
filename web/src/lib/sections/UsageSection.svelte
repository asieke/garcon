<script lang="ts">
	import TimeSeriesChart from '../charts/TimeSeriesChart.svelte';
	import CompositionBar from '../charts/CompositionBar.svelte';
	import { n } from '../format';
	import type { Totals, Granularity } from '../usage';

	let {
		buckets,
		granularity,
		claudeTokenSeries,
		codexTokenSeries,
		totals
	}: {
		buckets: number[];
		granularity: Granularity;
		claudeTokenSeries: number[];
		codexTokenSeries: number[];
		totals: Totals;
	} = $props();
</script>

<h2>Token throughput by harness</h2>
<TimeSeriesChart
	{buckets}
	{granularity}
	mode="stacked-area"
	formatValue={n}
	series={[
		{ key: 'codex', label: 'Codex', color: 'var(--harness-codex)', values: codexTokenSeries },
		{ key: 'claude', label: 'Claude', color: 'var(--harness-claude)', values: claudeTokenSeries }
	]}
/>

<h2 class="spaced">Token composition, this window</h2>
<CompositionBar
	formatValue={n}
	segments={[
		{ key: 'input', label: 'Input', value: totals.input, color: 'var(--series-3)' },
		{ key: 'cache_read', label: 'Cache read', value: totals.cache_read, color: 'var(--series-4)' },
		{ key: 'cache_write', label: 'Cache write', value: totals.cache_write, color: 'var(--series-5)' },
		{ key: 'output', label: 'Output', value: totals.output, color: 'var(--series-6)' }
	]}
/>
<p class="caption">Cache read tokens are far cheaper than fresh input on both providers — a low share here means prompts aren't reusing context well.</p>

<style>
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
		margin: 10px 0 0;
	}
</style>
