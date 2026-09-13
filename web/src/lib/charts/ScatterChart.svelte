<script lang="ts">
	type Point = { x: number; y: number; series: string; label: string };
	type Series = { key: string; label: string; color: string };

	let {
		points,
		series,
		xLabel,
		yLabel,
		formatX = (v: number) => String(Math.round(v)),
		formatY = (v: number) => String(Math.round(v)),
		xLog = false,
		yLog = false,
		height = 260,
		ariaLabel = 'Scatter plot'
	}: {
		points: Point[];
		series: Series[];
		xLabel: string;
		yLabel: string;
		formatX?: (v: number) => string;
		formatY?: (v: number) => string;
		xLog?: boolean;
		yLog?: boolean;
		height?: number;
		ariaLabel?: string;
	} = $props();

	const margin = { left: 46, right: 12, top: 12, bottom: 34 };
	const HIT_RADIUS = 24;

	let containerWidth = $state(0);
	let svgEl: SVGSVGElement = $state()!;
	let hover = $state<number | null>(null);

	const plotW = $derived(Math.max(0, containerWidth - margin.left - margin.right));
	const plotH = $derived(height - margin.top - margin.bottom);

	function axisFor(values: number[], log: boolean): { min: number; max: number; ticks: number[] } {
		if (!values.length) return { min: 0, max: 1, ticks: [0, 1] };
		if (log) {
			let lo = Infinity;
			let hi = 0;
			for (const v of values) {
				if (v > 0 && v < lo) lo = v;
				if (v > hi) hi = v;
			}
			if (lo === Infinity) {
				lo = 1;
				hi = 10;
			}
			const min = 10 ** Math.floor(Math.log10(lo));
			const max = 10 ** Math.ceil(Math.log10(hi) + (hi === min ? 1 : 0));
			const ticks: number[] = [];
			for (let t = min; t <= max * 1.0001; t *= 10) ticks.push(t);
			return { min, max, ticks };
		}
		let hi = 0;
		for (const v of values) if (v > hi) hi = v;
		const max = hi > 0 ? hi : 1;
		return { min: 0, max, ticks: [0, max / 2, max] };
	}

	const xs = $derived(points.map((p) => p.x));
	const ys = $derived(points.map((p) => p.y));
	const xAxis = $derived(axisFor(xs, xLog));
	const yAxis = $derived(axisFor(ys, yLog));

	function scale(v: number, axis: { min: number; max: number }, log: boolean): number {
		if (log) {
			const clamped = Math.max(axis.min, Math.min(axis.max, v));
			const span = Math.log10(axis.max) - Math.log10(axis.min) || 1;
			return (Math.log10(clamped) - Math.log10(axis.min)) / span;
		}
		const span = axis.max - axis.min || 1;
		return (Math.max(axis.min, Math.min(axis.max, v)) - axis.min) / span;
	}
	function xFor(v: number): number {
		return margin.left + scale(v, xAxis, xLog) * plotW;
	}
	function yFor(v: number): number {
		return margin.top + plotH - scale(v, yAxis, yLog) * plotH;
	}

	const colorOf = $derived(new Map(series.map((s) => [s.key, s.color])));
	const labelOf = $derived(new Map(series.map((s) => [s.key, s.label])));

	const screen = $derived(points.map((p) => ({ x: xFor(p.x), y: yFor(p.y) })));

	function onMove(e: PointerEvent) {
		if (!points.length || !svgEl) return;
		const rect = svgEl.getBoundingClientRect();
		const k = containerWidth ? containerWidth / rect.width : 1;
		const px = (e.clientX - rect.left) * k;
		const py = (e.clientY - rect.top) * k;
		let best = -1;
		let bestD = HIT_RADIUS * HIT_RADIUS;
		for (let i = 0; i < screen.length; i++) {
			const dx = screen[i].x - px;
			const dy = screen[i].y - py;
			const d = dx * dx + dy * dy;
			if (d < bestD) {
				bestD = d;
				best = i;
			}
		}
		hover = best >= 0 ? best : null;
	}

	const tooltipLeft = $derived.by(() => {
		if (hover === null || !containerWidth) return 0;
		return Math.min(72, Math.max(2, (100 * screen[hover].x) / containerWidth));
	});
	const tooltipTop = $derived(hover === null ? 0 : Math.max(0, screen[hover].y - 8));
</script>

<div class="chart" bind:clientWidth={containerWidth}>
	{#if series.length >= 2}
		<div class="legend">
			{#each series as s (s.key)}
				<span class="key"><i style="background: {s.color}"></i>{s.label}</span>
			{/each}
		</div>
	{/if}
	{#if containerWidth > 0 && points.length > 0}
		<svg bind:this={svgEl} width={containerWidth} {height} viewBox="0 0 {containerWidth} {height}" role="img" aria-label={ariaLabel} onpointermove={onMove} onpointerleave={() => (hover = null)}>
			{#each yAxis.ticks as tick}
				<line x1={margin.left} x2={containerWidth - margin.right} y1={yFor(tick)} y2={yFor(tick)} stroke="var(--grid)" stroke-width="1" />
				<text x={margin.left - 8} y={yFor(tick) + 3} text-anchor="end" class="axis">{formatY(tick)}</text>
			{/each}
			{#each xAxis.ticks as tick}
				<line x1={xFor(tick)} x2={xFor(tick)} y1={margin.top} y2={margin.top + plotH} stroke="var(--grid)" stroke-width="1" />
				<text x={xFor(tick)} y={margin.top + plotH + 14} text-anchor="middle" class="axis">{formatX(tick)}</text>
			{/each}
			<line x1={margin.left} x2={containerWidth - margin.right} y1={margin.top + plotH} y2={margin.top + plotH} stroke="var(--baseline)" stroke-width="1" />
			<text x={containerWidth - margin.right} y={height - 4} text-anchor="end" class="axis label">{xLabel}</text>
			<text x={margin.left} y={margin.top - 2} text-anchor="start" class="axis label">{yLabel}</text>

			{#each points as p, i}
				<circle
					cx={screen[i].x}
					cy={screen[i].y}
					r={hover === i ? 6 : 4}
					fill={colorOf.get(p.series) ?? 'var(--text-muted)'}
					fill-opacity={hover === i ? 1 : 0.7}
					stroke="var(--surface)"
					stroke-width="1.5"
				/>
			{/each}
		</svg>
		{#if hover !== null}
			<div class="tooltip" style="left: {tooltipLeft}%; top: {tooltipTop}px">
				<div class="t-date">{points[hover].label}</div>
				<div class="t-row"><span class="t-label"><i style="background: {colorOf.get(points[hover].series) ?? 'var(--text-muted)'}"></i>{labelOf.get(points[hover].series) ?? points[hover].series}</span></div>
				<div class="t-row"><span class="t-label">{xLabel}</span><span class="t-value tabular">{formatX(points[hover].x)}</span></div>
				<div class="t-row"><span class="t-label">{yLabel}</span><span class="t-value tabular">{formatY(points[hover].y)}</span></div>
			</div>
		{/if}
		<details class="table-view">
			<summary>Table view{points.length > 200 ? ' (first 200)' : ''}</summary>
			<div class="scroll">
				<table>
					<thead><tr><th>Request</th><th>Series</th><th class="num">{xLabel}</th><th class="num">{yLabel}</th></tr></thead>
					<tbody>
						{#each points.slice(0, 200) as p}
							<tr><td>{p.label}</td><td>{labelOf.get(p.series) ?? p.series}</td><td class="num">{formatX(p.x)}</td><td class="num">{formatY(p.y)}</td></tr>
						{/each}
					</tbody>
				</table>
			</div>
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
	.legend {
		display: flex;
		flex-wrap: wrap;
		gap: 14px;
		margin-bottom: 6px;
		font-size: 12px;
		color: var(--text-secondary);
	}
	.key {
		display: inline-flex;
		align-items: center;
		gap: 6px;
	}
	.key i {
		display: inline-block;
		width: 9px;
		height: 9px;
		border-radius: 50%;
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
	.axis.label {
		fill: var(--text-secondary);
	}
	.tooltip {
		position: absolute;
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: 6px;
		padding: 8px 10px;
		font-size: 12px;
		box-shadow: 0 4px 16px rgba(0, 0, 0, 0.12);
		pointer-events: none;
		min-width: 150px;
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
		display: inline-flex;
		align-items: center;
		gap: 6px;
		color: var(--text-secondary);
	}
	.t-label i {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		display: inline-block;
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
	.scroll {
		overflow-x: auto;
	}
	table {
		border-collapse: collapse;
		margin-top: 6px;
		white-space: nowrap;
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
