<script lang="ts">
	import RankedBarChart from '../charts/RankedBarChart.svelte';
	import { n, pct } from '../format';
	import { tokensOf, avgMs, errorRate, type Totals } from '../usage';

	let { modelGroups }: { modelGroups: { model: string; totals: Totals }[] } = $props();

	const TOP = 8;
	const top = $derived(
		modelGroups.slice(0, TOP).map((g) => ({ label: g.model, value: tokensOf(g.totals), sublabel: `${n(g.totals.n)} req` }))
	);
	const note = $derived(
		modelGroups.length > TOP ? `Showing the top ${TOP} of ${modelGroups.length} models by tokens — the rest are in the table below.` : null
	);
</script>

<h2>Models by total tokens</h2>
<RankedBarChart items={top} formatValue={n} truncatedNote={note} />

<h2 class="spaced">All models</h2>
<div class="scroll">
	<table>
		<thead>
			<tr>
				<th>Model</th>
				<th class="num">Requests</th>
				<th class="num">Input</th>
				<th class="num">Cache read</th>
				<th class="num">Cache write</th>
				<th class="num">Output</th>
				<th class="num">Avg latency</th>
				<th class="num">Errors</th>
			</tr>
		</thead>
		<tbody>
			{#each modelGroups as g (g.model)}
				<tr>
					<td>{g.model}</td>
					<td class="num">{n(g.totals.n)}</td>
					<td class="num">{n(g.totals.input)}</td>
					<td class="num">{n(g.totals.cache_read)}</td>
					<td class="num">{n(g.totals.cache_write)}</td>
					<td class="num">{n(g.totals.output)}</td>
					<td class="num">{Math.round(avgMs(g.totals))}ms</td>
					<td class="num" class:error={g.totals.errors > 0}>{pct(errorRate(g.totals), 1)}</td>
				</tr>
			{:else}
				<tr><td colspan="8">No requests in this window.</td></tr>
			{/each}
		</tbody>
	</table>
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
	.error {
		color: var(--status-critical);
	}
</style>
