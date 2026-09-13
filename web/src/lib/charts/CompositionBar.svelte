<script lang="ts">
	let {
		segments,
		formatValue = (v: number) => String(Math.round(v))
	}: {
		segments: { key: string; label: string; value: number; color: string }[];
		formatValue?: (v: number) => string;
	} = $props();

	const total = $derived(segments.reduce((s, x) => s + x.value, 0));
	const withPct = $derived(segments.map((s) => ({ ...s, pct: total ? (100 * s.value) / total : 0 })));
</script>

<div class="comp">
	<div class="bar" role="img" aria-label="Token composition for the selected window">
		{#each withPct as s (s.key)}
			{#if s.value > 0}
				<div
					class="seg"
					style="width: {s.pct}%; background: {s.color}"
					title="{s.label}: {formatValue(s.value)} ({s.pct.toFixed(1)}%)"
				></div>
			{/if}
		{/each}
	</div>
	<div class="legend">
		{#each withPct as s (s.key)}
			<span class="item">
				<i style="background: {s.color}"></i>{s.label}
				<b class="tabular">{formatValue(s.value)}</b>
				<span class="pct">{s.pct.toFixed(0)}%</span>
			</span>
		{/each}
	</div>
</div>

<style>
	.bar {
		display: flex;
		height: 28px;
		border-radius: 6px;
		overflow: hidden;
		background: var(--grid);
		gap: 2px;
	}
	.seg {
		height: 100%;
	}
	.legend {
		display: flex;
		flex-wrap: wrap;
		gap: 14px 20px;
		margin-top: 10px;
		font-size: 12px;
	}
	.item {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		color: var(--text-secondary);
	}
	.item i {
		width: 10px;
		height: 10px;
		border-radius: 2px;
		display: inline-block;
		flex-shrink: 0;
	}
	.item b {
		color: var(--text-primary);
		font-weight: 600;
	}
	.pct {
		color: var(--text-muted);
	}
</style>
