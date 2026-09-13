<script lang="ts">
	import { niceCeil } from './niceScale';
	import { thinTicks } from '../usage';

	let {
		edges,
		counts,
		formatEdge = (v: number) => String(Math.round(v)),
		markers = [],
		color = 'var(--series-1)',
		height = 180,
		ariaLabel = 'Distribution'
	}: {
		edges: number[];
		counts: number[];
		formatEdge?: (v: number) => string;
		/** Reference values (p50, p95) drawn as hairlines at their position within the bins. */
		markers?: { label: string; value: number }[];
		color?: string;
		height?: number;
		ariaLabel?: string;
	} = $props();

	const margin = { left: 40, right: 12, top: 18, bottom: 24 };
	const MAX_BAR = 24;
	const GAP = 2;

	let containerWidth = $state(0);
	let hover = $state<number | null>(null);
	let svgEl: SVGSVGElement = $state()!;

	function onMove(e: PointerEvent) {
		if (!bins || !svgEl) return;
		const rect = svgEl.getBoundingClientRect();
		const k = containerWidth ? containerWidth / rect.width : 1;
		const x = (e.clientX - rect.left) * k;
		const i = Math.floor((x - margin.left) / slot);
		hover = i >= 0 && i < bins ? i : null;
	}

	const bins = $derived(counts.length);
	const total = $derived(counts.reduce((s, c) => s + c, 0));
	const plotW = $derived(Math.max(0, containerWidth - margin.left - margin.right));
	const plotH = $derived(height - margin.top - margin.bottom);
	const slot = $derived(bins ? plotW / bins : 0);
	const barW = $derived(Math.max(1, Math.min(MAX_BAR, slot - GAP)));
	const niceMax = $derived(niceCeil(Math.max(1, ...counts)));
	const yTicks = $derived([0, niceMax / 2, niceMax]);

	function yFor(v: number): number {
		return margin.top + plotH - (v / niceMax) * plotH;
	}
	function slotX(i: number): number {
		return margin.left + i * slot;
	}
	function barX(i: number): number {
		return slotX(i) + (slot - barW) / 2;
	}
	/** Pixel x of a value, interpolated within whichever bin contains it (works for log bins too). */
	function xForValue(v: number): number | null {
		if (!bins) return null;
		if (v <= edges[0]) return slotX(0);
		if (v >= edges[bins]) return slotX(bins);
		let i = 0;
		while (i < bins - 1 && v >= edges[i + 1]) i++;
		const lo = edges[i];
		const hi = edges[i + 1];
		const t = hi > lo ? (v - lo) / (hi - lo) : 0;
		return slotX(i) + t * slot;
	}

	const maxLabels = $derived(Math.max(2, Math.floor(plotW / 56)));
	const labelIdxs = $derived(thinTicks(edges.length, maxLabels));

	function barPath(i: number): string {
		const x = barX(i);
		const top = yFor(counts[i]);
		const bottom = yFor(0);
		const h = bottom - top;
		if (h <= 0) return '';
		const r = Math.min(4, barW / 2, h);
		return `M ${x} ${bottom} V ${top + r} Q ${x} ${top} ${x + r} ${top} H ${x + barW - r} Q ${x + barW} ${top} ${x + barW} ${top + r} V ${bottom} Z`;
	}

	const tooltipLeft = $derived.by(() => {
		if (hover === null || !containerWidth) return 0;
		return Math.min(78, Math.max(2, (100 * slotX(hover)) / containerWidth));
	});
</script>

<div class="chart" bind:clientWidth={containerWidth}>
	{#if containerWidth > 0 && bins > 0}
		<svg bind:this={svgEl} width={containerWidth} {height} viewBox="0 0 {containerWidth} {height}" role="img" aria-label={ariaLabel} onpointermove={onMove} onpointerleave={() => (hover = null)}>
			{#each yTicks as tick}
				<line x1={margin.left} x2={containerWidth - margin.right} y1={yFor(tick)} y2={yFor(tick)} stroke="var(--grid)" stroke-width="1" />
				<text x={margin.left - 8} y={yFor(tick) + 3} text-anchor="end" class="axis">{Math.round(tick)}</text>
			{/each}
			<line x1={margin.left} x2={containerWidth - margin.right} y1={yFor(0)} y2={yFor(0)} stroke="var(--baseline)" stroke-width="1" />

			{#each counts as c, i}
				<path d={barPath(i)} fill={color} opacity={hover === null || hover === i ? 1 : 0.55} />
			{/each}

			{#each markers as m (m.label)}
				{@const mx = xForValue(m.value)}
				{#if mx !== null}
					<line x1={mx} x2={mx} y1={margin.top - 2} y2={yFor(0)} stroke="var(--baseline)" stroke-width="1" />
					<text x={mx + 3} y={margin.top - 6} class="axis marker">{m.label} {formatEdge(m.value)}</text>
				{/if}
			{/each}

			{#each labelIdxs as i}
				<text x={slotX(i)} y={height - 6} text-anchor={i === 0 ? 'start' : i === edges.length - 1 ? 'end' : 'middle'} class="axis">{formatEdge(edges[i])}</text>
			{/each}
		</svg>
		{#if hover !== null}
			<div class="tooltip" style="left: {tooltipLeft}%">
				<div class="t-date">{formatEdge(edges[hover])} to {formatEdge(edges[hover + 1])}</div>
				<div class="t-row">
					<span class="t-label">Requests</span>
					<span class="t-value tabular">{counts[hover]}</span>
				</div>
				<div class="t-row">
					<span class="t-label">Share</span>
					<span class="t-value tabular">{total ? ((100 * counts[hover]) / total).toFixed(1) : '0.0'}%</span>
				</div>
			</div>
		{/if}
		<details class="table-view">
			<summary>Table view</summary>
			<table>
				<thead><tr><th>Range</th><th class="num">Requests</th><th class="num">Share</th></tr></thead>
				<tbody>
					{#each counts as c, i}
						<tr>
							<td>{formatEdge(edges[i])} to {formatEdge(edges[i + 1])}</td>
							<td class="num">{c}</td>
							<td class="num">{total ? ((100 * c) / total).toFixed(1) : '0.0'}%</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</details>
	{:else}
		<div class="empty" style="height: {height}px">No data in this window.</div>
	{/if}
</div>

<style>
	.chart {
		position: relative;
		width: 100%;
	}
	svg {
		display: block;
		overflow: visible;
		touch-action: none;
	}
	.axis {
		fill: var(--text-muted);
		font-size: 10px;
	}
	.axis.marker {
		fill: var(--text-secondary);
	}
	.tooltip {
		position: absolute;
		top: 4px;
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: 6px;
		padding: 8px 10px;
		font-size: 12px;
		box-shadow: 0 4px 16px rgba(0, 0, 0, 0.12);
		pointer-events: none;
		min-width: 140px;
		z-index: 2;
	}
	.t-date {
		color: var(--text-secondary);
		font-size: 11px;
		margin-bottom: 4px;
	}
	.t-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
	}
	.t-label {
		color: var(--text-secondary);
	}
	.t-value {
		font-weight: 600;
	}
	.table-view {
		margin-top: 6px;
		font-size: 12px;
	}
	.table-view summary {
		color: var(--text-muted);
		cursor: pointer;
		font-size: 11px;
	}
	table {
		border-collapse: collapse;
		margin-top: 6px;
		min-width: 260px;
	}
	th,
	td {
		padding: 3px 8px;
		border-bottom: 1px solid var(--border);
		text-align: left;
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
	.empty {
		display: flex;
		align-items: center;
		justify-content: center;
		color: var(--text-muted);
		border: 1px dashed var(--border);
		border-radius: 8px;
	}
</style>
