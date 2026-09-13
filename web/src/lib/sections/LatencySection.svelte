<script lang="ts">
	import StatTile from '../charts/StatTile.svelte';
	import TimeSeriesChart from '../charts/TimeSeriesChart.svelte';
	import CompositionBar from '../charts/CompositionBar.svelte';
	import { ms as msFmt, us as usFmt, pct, n } from '../format';
	import { harnessLabel, harnessVar, type Granularity } from '../usage';

	type HarnessRow = {
		harness: string;
		count: number;
		p50: number;
		p95: number;
		p99: number;
		measuredN: number;
		reusedPct: number;
		avgDispatchUs: number;
		avgConnect: number;
		avgFirstByte: number;
	};

	let {
		granularity,
		latencyBuckets,
		p50Series,
		p95Series,
		overheadBuckets,
		dispatchSeries,
		connectSeries,
		measuredN,
		totalN,
		p50Ms,
		p95Ms,
		overheadP50,
		overheadP95,
		dispatchP50Us,
		reusedPct,
		avgFirstByte,
		connectionBreakdown,
		freshDialN,
		byHarness
	}: {
		granularity: Granularity;
		latencyBuckets: number[];
		p50Series: number[];
		p95Series: number[];
		overheadBuckets: number[];
		dispatchSeries: number[];
		connectSeries: number[];
		measuredN: number;
		totalN: number;
		p50Ms: number;
		p95Ms: number;
		overheadP50: number;
		overheadP95: number;
		dispatchP50Us: number;
		reusedPct: number;
		avgFirstByte: number;
		connectionBreakdown: { dns: number; tcp: number; tls: number } | null;
		freshDialN: number;
		byHarness: HarnessRow[];
	} = $props();
</script>

<section class="tiles">
	<StatTile label="Median proxy overhead" value={measuredN ? msFmt(overheadP50) : '—'} accent="var(--series-2)" />
	<StatTile label="p95 proxy overhead" value={measuredN ? msFmt(overheadP95) : '—'} accent="var(--series-2)" />
	<StatTile label="Proxy dispatch (median)" value={measuredN ? usFmt(dispatchP50Us) : '—'} accent="var(--series-2)" />
	<StatTile label="Connections reused" value={measuredN ? pct(reusedPct) : '—'} accent="var(--series-2)" />
	<StatTile label="Median total latency" value={msFmt(p50Ms)} accent="var(--series-1)" />
	<StatTile label="p95 total latency" value={msFmt(p95Ms)} accent="var(--series-1)" />
</section>

<p class="caption lead">
	<b>Proxy overhead</b> is the part of total latency garcon actually controls: <b>dispatch</b> (its own
	Go-side request handling — URL rewrite, request cloning, scheduling — before it even asks for a
	connection, measured in microseconds because it's expected to be negligible) plus <b>connect</b>
	(acquiring the upstream connection — near-instant when the pool already has one warm, or a fresh
	DNS+TCP+TLS handshake otherwise). Everything else in "total latency" — sending the request body,
	waiting for the model's first token, and streaming the reply — is the provider's doing, not the
	proxy's, and would take just as long calling the provider directly.
</p>

{#if measuredN === 0}
	<p class="empty">
		No requests since this measurement shipped yet ({n(totalN)} older requests on record don't have it) —
		overhead detail appears as new requests come through.
	</p>
{:else}
	<h2 class="spaced">Proxy overhead over time</h2>
	<TimeSeriesChart
		buckets={overheadBuckets}
		{granularity}
		mode="stacked-area"
		formatValue={msFmt}
		series={[
			{ key: 'connect', label: 'Connect', color: 'var(--series-2)', values: connectSeries },
			{ key: 'dispatch', label: 'Dispatch', color: 'var(--series-4)', values: dispatchSeries }
		]}
	/>
	<p class="caption">Dispatch is usually an imperceptible sliver next to connect — that's the point: proxying this way costs almost nothing on its own.</p>

	<h2 class="spaced">Connection setup breakdown</h2>
	{#if connectionBreakdown}
		<CompositionBar
			formatValue={msFmt}
			segments={[
				{ key: 'dns', label: 'DNS lookup', value: connectionBreakdown.dns, color: 'var(--series-3)' },
				{ key: 'tcp', label: 'TCP handshake', value: connectionBreakdown.tcp, color: 'var(--series-4)' },
				{ key: 'tls', label: 'TLS handshake', value: connectionBreakdown.tls, color: 'var(--series-5)' }
			]}
		/>
		<p class="caption">
			Median duration of each phase across the {n(freshDialN)} fresh dial{freshDialN === 1 ? '' : 's'} in this
			window (phases run one after another within a single dial, not simultaneously — this shows where that
			time typically goes, not one specific connection).
		</p>
	{:else}
		<p class="caption">Every request in this window reused a warm pooled connection — no fresh dials to break down.</p>
	{/if}

	<h2 class="spaced">By harness</h2>
	<div class="scroll">
		<table>
			<thead>
				<tr>
					<th>Harness</th>
					<th class="num">Requests</th>
					<th class="num">p50</th>
					<th class="num">p95</th>
					<th class="num">p99</th>
					<th class="num">Reused</th>
					<th class="num">Avg dispatch</th>
					<th class="num">Avg connect</th>
					<th class="num">Avg first byte</th>
				</tr>
			</thead>
			<tbody>
				{#each byHarness as row (row.harness)}
					<tr>
						<td><i class="dot" style="background: {harnessVar(row.harness)}"></i>{harnessLabel(row.harness)}</td>
						<td class="num">{n(row.count)}</td>
						<td class="num">{Math.round(row.p50)}ms</td>
						<td class="num">{Math.round(row.p95)}ms</td>
						<td class="num">{Math.round(row.p99)}ms</td>
						<td class="num">{row.measuredN ? pct(row.reusedPct) : '—'}</td>
						<td class="num">{row.measuredN ? usFmt(row.avgDispatchUs) : '—'}</td>
						<td class="num">{row.measuredN ? `${Math.round(row.avgConnect)}ms` : '—'}</td>
						<td class="num">{row.measuredN ? `${Math.round(row.avgFirstByte)}ms` : '—'}</td>
					</tr>
				{:else}
					<tr><td colspan="9">No requests in this window.</td></tr>
				{/each}
			</tbody>
		</table>
	</div>
{/if}

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
	.caption b {
		color: var(--text-secondary);
	}
	.empty {
		color: var(--text-muted);
		font-size: 13px;
		border: 1px dashed var(--border);
		border-radius: 8px;
		padding: 16px;
		margin-top: 12px;
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
	.dot {
		display: inline-block;
		width: 8px;
		height: 8px;
		border-radius: 2px;
		margin-right: 6px;
		vertical-align: middle;
	}
</style>
