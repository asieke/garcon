<script lang="ts">
	let {
		items,
		color = 'var(--series-1)',
		formatValue = (v: number) => String(Math.round(v)),
		truncatedNote = null
	}: {
		items: { label: string; value: number; sublabel?: string }[];
		color?: string;
		formatValue?: (v: number) => string;
		truncatedNote?: string | null;
	} = $props();

	const max = $derived(Math.max(1, ...items.map((i) => i.value)));
</script>

<div class="bars">
	{#each items as item (item.label)}
		<div class="row" title="{item.label}: {formatValue(item.value)}">
			<span class="label">{item.label}{#if item.sublabel}<span class="sub"> · {item.sublabel}</span>{/if}</span>
			<div class="track">
				<div class="fill" style="width: {((item.value / max) * 100).toFixed(1)}%; background: {color}"></div>
			</div>
			<span class="value tabular">{formatValue(item.value)}</span>
		</div>
	{:else}
		<p class="empty">No data in this window.</p>
	{/each}
</div>
{#if truncatedNote}<p class="note">{truncatedNote}</p>{/if}

<style>
	.bars {
		display: flex;
		flex-direction: column;
		gap: 10px;
	}
	.row {
		display: grid;
		grid-template-columns: minmax(90px, 170px) 1fr auto;
		align-items: center;
		gap: 10px;
	}
	.label {
		font-size: 12px;
		color: var(--text-secondary);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.sub {
		color: var(--text-muted);
	}
	.track {
		height: 14px;
		background: var(--grid);
		border-radius: 3px;
		overflow: hidden;
	}
	.fill {
		height: 100%;
		border-radius: 0 4px 4px 0;
		min-width: 2px;
	}
	.value {
		font-size: 12px;
		min-width: 48px;
		text-align: right;
	}
	.empty {
		color: var(--text-muted);
		font-size: 13px;
	}
	.note {
		color: var(--text-muted);
		font-size: 11px;
		margin: 8px 0 0;
	}
	@media (max-width: 520px) {
		.row {
			grid-template-columns: minmax(70px, 110px) 1fr auto;
		}
	}
</style>
