<script lang="ts">
	import StatTile from '../charts/StatTile.svelte';
	import RankedBarChart from '../charts/RankedBarChart.svelte';
	import ScatterChart from '../charts/ScatterChart.svelte';
	import Histogram from '../charts/Histogram.svelte';
	import { n, ms as msFmt, tps, pct, when } from '../format';
	import { contextOf, genSpeed, percentile, modelSlots, modelColor, maxOf, type Row } from '../usage';
	import { byModelSpeed, linearBins, logBins, topModelsByRequests, displayNames } from '../performance';

	let { rows: allRows }: { rows: Row[] } = $props();

	const SCATTER_CAP = 1500;

	// Error replies have no model output to time, so everything here is over successful requests.
	const rows = $derived(allRows.filter((r) => r.status < 400));
	const errorN = $derived(allRows.length - rows.length);

	const speeds = $derived(rows.map(genSpeed).filter((v): v is number => v !== null));
	const ttfts = $derived(rows.map((r) => r.first_byte_ms).filter((v): v is number => v !== undefined));
	const contexts = $derived(rows.map(contextOf));
	const outputs = $derived(rows.map((r) => r.output));
	const measured = $derived(ttfts.length > 0);

	const byModel = $derived(byModelSpeed(rows));
	const names = $derived(displayNames(byModel.map((m) => m.model)));
	const slots = $derived(modelSlots(byModel.map((m) => m.model)));

	const speedRanked = $derived(
		byModel
			.filter((m) => m.p50Speed !== null)
			.sort((a, b) => (b.p50Speed ?? 0) - (a.p50Speed ?? 0))
			.map((m) => ({ label: names.get(m.model) ?? m.model, value: m.p50Speed ?? 0, sublabel: `${n(m.n)} measured` }))
	);
	const ttftRanked = $derived(
		byModel
			.filter((m) => m.p50Ttft !== null)
			.sort((a, b) => (a.p50Ttft ?? 0) - (b.p50Ttft ?? 0))
			.map((m) => ({ label: names.get(m.model) ?? m.model, value: m.p50Ttft ?? 0, sublabel: `p95 ${msFmt(m.p95Ttft ?? 0)}` }))
	);

	const top3 = $derived(topModelsByRequests(rows, 3));
	const scatterSeries = $derived.by(() => {
		const s = top3.map((m) => ({ key: m, label: names.get(m) ?? m, color: modelColor(slots, m) }));
		if (new Set(rows.map((r) => r.model || '(unknown)')).size > 3) s.push({ key: 'Other', label: 'Other', color: 'var(--text-muted)' });
		return s;
	});
	const scatterPoints = $derived.by(() => {
		const keep = new Set(top3);
		const withOutput = rows.filter((r) => r.output > 0);
		const recent = withOutput.length > SCATTER_CAP ? [...withOutput].sort((a, b) => b.time - a.time).slice(0, SCATTER_CAP) : withOutput;
		return recent.map((r) => {
			const m = r.model || '(unknown)';
			return { x: r.output, y: r.ms, series: keep.has(m) ? m : 'Other', label: `${names.get(m) ?? m}, ${when(r.time)}` };
		});
	});
	const scatterCapped = $derived(rows.filter((r) => r.output > 0).length > SCATTER_CAP);

	const contextBins = $derived(linearBins(contexts, 12));
	const outputBins = $derived(logBins(outputs));
</script>

<section class="tiles">
	<StatTile label="Median generation speed" value={speeds.length ? tps(percentile(speeds, 50)) : '—'} accent="var(--series-3)" />
	<StatTile label="Median time to first byte" value={measured ? msFmt(percentile(ttfts, 50)) : '—'} accent="var(--series-2)" />
	<StatTile label="p95 time to first byte" value={measured ? msFmt(percentile(ttfts, 95)) : '—'} accent="var(--series-2)" />
	<StatTile label="Median context" value={rows.length ? n(percentile(contexts, 50)) : '—'} accent="var(--series-1)" />
	<StatTile label="Largest context" value={rows.length ? n(maxOf(contexts)) : '—'} accent="var(--series-1)" />
	<StatTile label="Median output" value={rows.length ? n(percentile(outputs, 50)) : '—'} accent="var(--series-1)" />
</section>
<p class="caption lead">
	Time to first byte is the provider's time to first token as the proxy sees it. Generation speed is output tokens
	per second of streaming after that first byte, measured on replies of at least 20 tokens so tiny replies do not
	skew it. Context is everything the model read: fresh input plus cache reads and writes.
	{#if errorN}Successful requests only; {n(errorN)} error{errorN === 1 ? '' : 's'} in this window are left out.{/if}
</p>

{#if !measured}
	<p class="empty">No first-byte timing in this window yet ({n(rows.length)} requests predate it).</p>
{:else}
	<div class="pair">
		<div>
			<h2 class="spaced">Generation speed by model</h2>
			<RankedBarChart items={speedRanked} formatValue={(v) => tps(v)} color="var(--series-3)" />
			<p class="caption">Median output tokens/s.</p>
		</div>
		<div>
			<h2 class="spaced">Time to first byte by model</h2>
			<RankedBarChart items={ttftRanked} formatValue={msFmt} color="var(--series-2)" />
			<p class="caption">Median time to first byte.</p>
		</div>
	</div>

	<h2 class="spaced">Latency vs output size</h2>
	<ScatterChart
		points={scatterPoints}
		series={scatterSeries}
		xLabel="Output tokens"
		yLabel="Total latency"
		formatX={n}
		formatY={msFmt}
		xLog
		yLog
	/>
	<p class="caption">Log axes.{#if scatterCapped} Latest {n(SCATTER_CAP)} requests.{/if}</p>
{/if}

<div class="pair">
	<div>
		<h2 class="spaced">Context size per request</h2>
		<Histogram
			edges={contextBins.edges}
			counts={contextBins.counts}
			formatEdge={n}
			markers={rows.length ? [{ label: 'p50', value: percentile(contexts, 50) }, { label: 'p95', value: percentile(contexts, 95) }] : []}
			color="var(--series-1)"
		/>
			</div>
	<div>
		<h2 class="spaced">Output tokens per request</h2>
		<Histogram
			edges={outputBins.edges}
			counts={outputBins.counts}
			formatEdge={n}
			markers={rows.length ? [{ label: 'p50', value: percentile(outputs, 50) }, { label: 'p95', value: percentile(outputs, 95) }] : []}
			color="var(--series-3)"
		/>
		<p class="caption">Log bins.</p>
	</div>
</div>

<h2 class="spaced">By model</h2>
<div class="scroll">
	<table>
		<thead>
			<tr>
				<th>Model</th>
				<th class="num">Requests</th>
				<th class="num">Measured</th>
				<th class="num">p50 speed</th>
				<th class="num">p95 speed</th>
				<th class="num">p50 first byte</th>
				<th class="num">p95 first byte</th>
				<th class="num">p50 total</th>
				<th class="num">p50 output</th>
				<th class="num">p50 context</th>
			</tr>
		</thead>
		<tbody>
			{#each byModel as m (m.model)}
				<tr>
					<td title={m.model}><i class="dot" style="background: {modelColor(slots, m.model)}"></i>{names.get(m.model) ?? m.model}</td>
					<td class="num">{n(m.requests)}</td>
					<td class="num">{pct(100 * m.measuredShare)}</td>
					<td class="num">{m.p50Speed === null ? '—' : tps(m.p50Speed)}</td>
					<td class="num">{m.p95Speed === null ? '—' : tps(m.p95Speed)}</td>
					<td class="num">{m.p50Ttft === null ? '—' : msFmt(m.p50Ttft)}</td>
					<td class="num">{m.p95Ttft === null ? '—' : msFmt(m.p95Ttft)}</td>
					<td class="num">{msFmt(m.p50Total)}</td>
					<td class="num">{n(m.p50Output)}</td>
					<td class="num">{n(m.p50Context)}</td>
				</tr>
			{:else}
				<tr><td colspan="10">No requests in this window.</td></tr>
			{/each}
		</tbody>
	</table>
</div>

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
	.empty {
		color: var(--text-muted);
		font-size: 13px;
		border: 1px dashed var(--border);
		border-radius: 8px;
		padding: 16px;
		margin-top: 12px;
	}
	.pair {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(min(100%, 320px), 1fr));
		gap: 0 32px;
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
		margin-right: 8px;
		vertical-align: middle;
	}
</style>
