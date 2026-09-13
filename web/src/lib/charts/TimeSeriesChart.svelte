<script lang="ts">
	import { niceCeil } from './niceScale';
	import { thinTicks, type Granularity } from '../usage';
	import { dayLabel, hourLabel, fullTimestamp } from '../format';

	type Series = { key: string; label: string; color: string; values: number[] };

	let {
		buckets,
		series,
		granularity,
		mode = 'stacked-area',
		height = 220,
		formatValue = (v: number) => String(Math.round(v)),
		errorCounts = null
	}: {
		buckets: number[];
		series: Series[];
		granularity: Granularity;
		mode?: 'stacked-area' | 'lines';
		height?: number;
		formatValue?: (v: number) => string;
		errorCounts?: number[] | null;
	} = $props();

	const margin = { left: 46, right: 12, top: 12, bottom: 24 };

	let containerWidth = $state(0);
	let hoverIndex = $state<number | null>(null);
	let svgEl: SVGSVGElement = $state()!;

	const plotW = $derived(Math.max(0, containerWidth - margin.left - margin.right));
	const plotH = $derived(height - margin.top - margin.bottom);

	function xFor(i: number): number {
		if (buckets.length <= 1) return margin.left + plotW / 2;
		return margin.left + (i / (buckets.length - 1)) * plotW;
	}

	const stackedTotals = $derived(
		buckets.map((_, i) => series.reduce((s, ser) => s + (ser.values[i] ?? 0), 0))
	);

	const rawMax = $derived(
		mode === 'stacked-area'
			? Math.max(0, ...stackedTotals)
			: Math.max(0, ...series.flatMap((s) => s.values))
	);
	const niceMax = $derived(niceCeil(rawMax || 1));

	function yFor(v: number): number {
		return margin.top + plotH - (niceMax ? v / niceMax : 0) * plotH;
	}

	const yTicks = $derived([0, niceMax / 2, niceMax]);

	const maxLabels = $derived(Math.max(2, Math.floor(plotW / 72)));
	const tickIdxs = $derived(thinTicks(buckets.length, maxLabels));

	function xLabel(t: number): string {
		return granularity === 'hour' ? hourLabel(t) : dayLabel(t);
	}

	const layers = $derived.by(() => {
		if (mode !== 'stacked-area') return [];
		let cum = buckets.map(() => 0);
		return series.map((ser) => {
			const baseline = cum;
			const top = cum.map((c, i) => c + (ser.values[i] ?? 0));
			cum = top;
			return { ser, baseline, top };
		});
	});

	function areaPath(baseline: number[], top: number[]): string {
		if (!buckets.length) return '';
		const topPts = top.map((v, i) => `${xFor(i)},${yFor(v)}`);
		const basePts = baseline
			.map((v, i) => `${xFor(i)},${yFor(v)}`)
			.reverse();
		return `M ${topPts.join(' L ')} L ${basePts.join(' L ')} Z`;
	}

	function linePath(values: number[]): string {
		if (!buckets.length) return '';
		return values.map((v, i) => `${i === 0 ? 'M' : 'L'} ${xFor(i)},${yFor(v)}`).join(' ');
	}

	function indexFromClientX(clientX: number): number {
		const rect = svgEl.getBoundingClientRect();
		const scale = containerWidth ? containerWidth / rect.width : 1;
		const localX = (clientX - rect.left) * scale;
		const ratio = plotW ? (localX - margin.left) / plotW : 0;
		return Math.min(buckets.length - 1, Math.max(0, Math.round(ratio * (buckets.length - 1))));
	}

	function onMove(e: PointerEvent) {
		if (!buckets.length) return;
		hoverIndex = indexFromClientX(e.clientX);
	}
	function onLeave() {
		hoverIndex = null;
	}

	const hoverX = $derived(hoverIndex === null ? 0 : xFor(hoverIndex));
	const tooltipLeft = $derived.by(() => {
		if (hoverIndex === null || !containerWidth) return 0;
		const raw = (hoverX / containerWidth) * 100;
		return Math.min(82, Math.max(2, raw));
	});
</script>

<div class="chart" bind:clientWidth={containerWidth}>
	{#if series.length >= 2}
		<div class="legend">
			{#each series as s (s.key)}
				<span class="key"><i class={mode === 'stacked-area' ? 'swatch' : 'line'} style="background: {s.color}"></i>{s.label}</span>
			{/each}
			{#if errorCounts}
				<span class="key"><i class="dot" style="background: var(--status-critical)"></i>Errors</span>
			{/if}
		</div>
	{/if}
	{#if containerWidth > 0 && buckets.length > 0}
		<svg
			bind:this={svgEl}
			width={containerWidth}
			{height}
			viewBox="0 0 {containerWidth} {height}"
			onpointermove={onMove}
			onpointerleave={onLeave}
			role="img"
			aria-label="Usage over time"
		>
			{#each yTicks as tick}
				<line
					x1={margin.left}
					x2={containerWidth - margin.right}
					y1={yFor(tick)}
					y2={yFor(tick)}
					stroke="var(--grid)"
					stroke-width="1"
				/>
				<text x={margin.left - 8} y={yFor(tick) + 3} text-anchor="end" class="axis">{formatValue(tick)}</text>
			{/each}
			<line
				x1={margin.left}
				x2={containerWidth - margin.right}
				y1={yFor(0)}
				y2={yFor(0)}
				stroke="var(--baseline)"
				stroke-width="1"
			/>

			{#if mode === 'stacked-area'}
				{#each layers as layer (layer.ser.key)}
					<path d={areaPath(layer.baseline, layer.top)} fill={layer.ser.color} opacity="0.16" />
					<path d={linePath(layer.top)} fill="none" stroke={layer.ser.color} stroke-width="2" stroke-linejoin="round" />
				{/each}
			{:else}
				{#each series as s (s.key)}
					<path d={linePath(s.values)} fill="none" stroke={s.color} stroke-width="2" stroke-linejoin="round" stroke-linecap="round" />
				{/each}
			{/if}

			{#if buckets.length === 1}
				<!-- A single bucket has no line segment to draw, so it would otherwise render as an empty chart. -->
				{#if mode === 'stacked-area'}
					{#each layers as layer (layer.ser.key)}
						<circle cx={xFor(0)} cy={yFor(layer.top[0])} r="5" fill={layer.ser.color} stroke="var(--surface)" stroke-width="2" />
					{/each}
				{:else}
					{#each series as s (s.key)}
						<circle cx={xFor(0)} cy={yFor(s.values[0] ?? 0)} r="5" fill={s.color} stroke="var(--surface)" stroke-width="2" />
					{/each}
				{/if}
			{/if}

			{#if errorCounts}
				{#each errorCounts as count, i}
					{#if count > 0}
						<circle
							cx={xFor(i)}
							cy={yFor(mode === 'stacked-area' ? stackedTotals[i] : Math.max(...series.map((s) => s.values[i] ?? 0))) - 7}
							r="3"
							fill="var(--status-critical)"
							stroke="var(--surface)"
							stroke-width="2"
						/>
					{/if}
				{/each}
			{/if}

			{#each tickIdxs as i}
				<text x={xFor(i)} y={height - 6} text-anchor="middle" class="axis">{xLabel(buckets[i])}</text>
			{/each}

			{#if hoverIndex !== null}
				<line x1={hoverX} x2={hoverX} y1={margin.top} y2={margin.top + plotH} stroke="var(--baseline)" stroke-width="1" />
			{/if}
		</svg>

		{#if hoverIndex !== null}
			<div class="tooltip" style="left: {tooltipLeft}%">
				<div class="t-date">{fullTimestamp(buckets[hoverIndex])}</div>
				{#each series as s (s.key)}
					<div class="t-row">
						<span class="t-label"><i style="background: {s.color}"></i>{s.label}</span>
						<span class="t-value tabular">{formatValue(s.values[hoverIndex] ?? 0)}</span>
					</div>
				{/each}
				{#if mode === 'stacked-area' && series.length > 1}
					<div class="t-row total">
						<span class="t-label">Total</span>
						<span class="t-value tabular">{formatValue(stackedTotals[hoverIndex])}</span>
					</div>
				{/if}
				{#if errorCounts && errorCounts[hoverIndex] > 0}
					<div class="t-row">
						<span class="t-label"><i style="background: var(--status-critical)"></i>Errors</span>
						<span class="t-value tabular">{errorCounts[hoverIndex]}</span>
					</div>
				{/if}
			</div>
		{/if}
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
	}
	.key i.line {
		width: 10px;
		height: 2px;
		border-radius: 1px;
	}
	.key i.swatch {
		width: 9px;
		height: 9px;
		border-radius: 2px;
	}
	.key i.dot {
		width: 6px;
		height: 6px;
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
	.tooltip {
		position: absolute;
		top: 4px;
		transform: translateX(0);
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
	.t-row.total {
		border-top: 1px solid var(--border);
		margin-top: 3px;
		padding-top: 3px;
		font-weight: 600;
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
		border-radius: 2px;
		display: inline-block;
	}
	.t-value {
		font-weight: 600;
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
