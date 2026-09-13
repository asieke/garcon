<script lang="ts">
	import Sparkline from './Sparkline.svelte';

	let {
		label,
		value,
		delta = null,
		deltaGoodWhen = 'up',
		trend = null,
		accent = 'var(--series-1)'
	}: {
		label: string;
		value: string;
		delta?: number | null;
		deltaGoodWhen?: 'up' | 'down' | 'neutral';
		trend?: number[] | null;
		accent?: string;
	} = $props();

	const deltaColor = $derived(
		delta === null || deltaGoodWhen === 'neutral' || !isFinite(delta)
			? 'var(--text-secondary)'
			: (deltaGoodWhen === 'up' ? delta >= 0 : delta <= 0)
				? 'var(--status-good)'
				: 'var(--status-critical)'
	);
</script>

<div class="tile">
	<small>{label}</small>
	<div class="row">
		<b>{value}</b>
		{#if trend && trend.length >= 2}
			<span class="trend"><Sparkline points={trend} color={accent} /></span>
		{/if}
	</div>
	{#if delta !== null && isFinite(delta)}
		<span class="delta" style="color: {deltaColor}">{delta >= 0 ? '+' : ''}{delta.toFixed(1)}% vs prior period</span>
	{/if}
</div>

<style>
	.tile {
		padding: 14px 16px;
		border-radius: 8px;
		border: 1px solid var(--border);
		background: var(--surface);
		display: flex;
		flex-direction: column;
		gap: 5px;
		min-width: 0;
	}
	small {
		color: var(--text-secondary);
	}
	.row {
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		gap: 8px;
	}
	b {
		font-size: 26px;
		font-weight: 600;
		white-space: nowrap;
	}
	.trend {
		flex-shrink: 0;
		opacity: 0.9;
	}
	.delta {
		font-size: 11px;
		font-variant-numeric: tabular-nums;
	}
</style>
