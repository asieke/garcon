<script lang="ts">
	let {
		rowLabels,
		colLabels,
		cells,
		formatValue = (v: number) => String(Math.round(v)),
		cellTitle,
		max = null,
		legendLabel = 'Fewer to more',
		ariaLabel = 'Heat map'
	}: {
		rowLabels: string[];
		/** One per column; null draws no label (used to thin labels). */
		colLabels: (string | null)[];
		/** rows × cols. null = outside the data window (drawn faint, no tooltip). */
		cells: (number | null)[][];
		formatValue?: (v: number) => string;
		cellTitle: (row: number, col: number) => string;
		max?: number | null;
		legendLabel?: string;
		ariaLabel?: string;
	} = $props();

	const STEPS = 7;
	const GAP = 2;
	const LABEL_W = 34;

	let containerWidth = $state(0);
	let hover = $state<{ r: number; c: number } | null>(null);
	let svgEl: SVGSVGElement = $state()!;

	function onMove(e: PointerEvent) {
		if (!svgEl) return;
		const rect = svgEl.getBoundingClientRect();
		const px = e.clientX - rect.left;
		const py = e.clientY - rect.top;
		const c = Math.floor((px - LABEL_W) / (size + GAP));
		const r = Math.floor((py - labelBand) / (size + GAP));
		hover = c >= 0 && c < cols && r >= 0 && r < rows ? { r, c } : null;
	}

	const cols = $derived(colLabels.length);
	const rows = $derived(rowLabels.length);

	const dataMax = $derived.by(() => {
		if (max !== null) return max;
		let m = 0;
		for (const row of cells) for (const v of row) if (v !== null && v > m) m = v;
		return m;
	});

	/** Cell size fills the container when it can, but never drops below 10px (the grid then scrolls). */
	const size = $derived.by(() => {
		if (!containerWidth || !cols) return 12;
		const avail = containerWidth - LABEL_W - GAP * (cols - 1);
		return Math.max(10, Math.min(30, Math.floor(avail / cols)));
	});
	const gridW = $derived(LABEL_W + cols * size + (cols - 1) * GAP);
	const gridH = $derived(rows * size + (rows - 1) * GAP);
	const labelBand = $derived(colLabels.some((l) => l !== null) ? 16 : 0);

	function step(v: number): number {
		if (v <= 0 || dataMax <= 0) return 0;
		return Math.max(1, Math.min(STEPS, Math.ceil((v / dataMax) * STEPS)));
	}
	function fill(v: number | null): string {
		if (v === null) return 'var(--seq-empty)';
		const s = step(v);
		return s === 0 ? 'var(--seq-empty)' : `var(--seq-${s})`;
	}
	function x(c: number): number {
		return LABEL_W + c * (size + GAP);
	}
	function y(r: number): number {
		return labelBand + r * (size + GAP);
	}

	const hovered = $derived(hover && cells[hover.r]?.[hover.c] !== null && cells[hover.r]?.[hover.c] !== undefined ? hover : null);
	const tooltipLeft = $derived.by(() => {
		if (!hovered || !gridW) return 0;
		return Math.min(70, Math.max(0, (100 * x(hovered.c)) / gridW));
	});
	const tooltipTop = $derived(hovered ? y(hovered.r) + size + 6 : 0);
	const hasData = $derived(cells.some((row) => row.some((v) => v !== null)));
</script>

<div class="heat" bind:clientWidth={containerWidth}>
	{#if !hasData}
		<div class="empty">No data in this window.</div>
	{:else}
		<div class="scroll">
			<div class="stage" style="width: {gridW}px">
				<svg bind:this={svgEl} width={gridW} height={gridH + labelBand} viewBox="0 0 {gridW} {gridH + labelBand}" role="img" aria-label={ariaLabel} onpointermove={onMove} onpointerleave={() => (hover = null)}>
					{#each colLabels as label, c}
						{#if label !== null}
							<text x={x(c)} y={11} class="axis">{label}</text>
						{/if}
					{/each}
					{#each rowLabels as label, r}
						<text x={LABEL_W - 6} y={y(r) + size / 2 + 3.5} text-anchor="end" class="axis">{label}</text>
					{/each}
					{#each cells as row, r}
						{#each row as v, c}
							<rect
								x={x(c)}
								y={y(r)}
								width={size}
								height={size}
								rx="2"
								fill={fill(v)}
								opacity={v === null ? 0.4 : 1}
								class="cell"
								class:hot={hovered?.r === r && hovered?.c === c}
							/>
						{/each}
					{/each}
				</svg>
				{#if hovered}
					<div class="tooltip" style="left: {tooltipLeft}%; top: {tooltipTop}px">
						<div class="t-date">{cellTitle(hovered.r, hovered.c)}</div>
						<div class="t-row">
							<span class="t-value tabular">{formatValue(cells[hovered.r][hovered.c] ?? 0)}</span>
						</div>
					</div>
				{/if}
			</div>
		</div>
		<div class="legend">
			<span class="legend-label">{legendLabel}</span>
			<span class="tabular">0</span>
			<span class="ramp" aria-hidden="true">
				{#each Array.from({ length: STEPS }, (_, i) => i + 1) as s}
					<i style="background: var(--seq-{s})"></i>
				{/each}
			</span>
			<span class="tabular">{formatValue(dataMax)}</span>
		</div>
		<details class="table-view">
			<summary>Table view</summary>
			<div class="scroll">
				<table>
					<thead>
						<tr>
							<th></th>
							{#each colLabels as label, c}<th class="num">{label ?? c}</th>{/each}
						</tr>
					</thead>
					<tbody>
						{#each cells as row, r}
							<tr>
								<th>{rowLabels[r]}</th>
								{#each row as v}<td class="num">{v === null ? '' : formatValue(v)}</td>{/each}
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		</details>
	{/if}
</div>

<style>
	.heat {
		position: relative;
		width: 100%;
	}
	.scroll {
		overflow-x: auto;
	}
	.stage {
		position: relative;
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
	.cell {
		cursor: default;
	}
	.cell.hot {
		stroke: var(--text-primary);
		stroke-width: 1.5;
		stroke-opacity: 0.6;
	}
	.legend {
		display: flex;
		align-items: center;
		gap: 6px;
		margin-top: 8px;
		font-size: 11px;
		color: var(--text-muted);
	}
	.legend-label {
		margin-right: 4px;
	}
	.ramp {
		display: inline-flex;
		gap: 2px;
	}
	.ramp i {
		width: 12px;
		height: 12px;
		border-radius: 2px;
		display: inline-block;
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
		min-width: 120px;
		z-index: 2;
	}
	.t-date {
		color: var(--text-secondary);
		font-size: 11px;
		margin-bottom: 2px;
	}
	.t-row {
		display: flex;
		justify-content: space-between;
	}
	.t-value {
		font-weight: 600;
	}
	.table-view {
		margin-top: 10px;
		font-size: 12px;
	}
	.table-view summary {
		color: var(--text-muted);
		cursor: pointer;
		font-size: 11px;
	}
	table {
		border-collapse: collapse;
		white-space: nowrap;
		margin-top: 6px;
	}
	th,
	td {
		padding: 3px 6px;
		border-bottom: 1px solid var(--border);
		font-variant-numeric: tabular-nums;
		text-align: left;
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
		height: 120px;
		color: var(--text-muted);
		border: 1px dashed var(--border);
		border-radius: 8px;
	}
</style>
