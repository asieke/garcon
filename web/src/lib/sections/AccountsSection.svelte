<script lang="ts">
	import RankedBarChart from '../charts/RankedBarChart.svelte';
	import Sparkline from '../charts/Sparkline.svelte';
	import { n, pct } from '../format';
	import { tokensOf, avgMs, errorRate, harnessLabel, harnessVar, type Totals } from '../usage';

	let {
		accountGroups
	}: {
		accountGroups: { account: string; harnesses: string[]; totals: Totals; trend: number[]; color: string }[];
	} = $props();

	const ranked = $derived(
		[...accountGroups]
			.sort((a, b) => tokensOf(b.totals) - tokensOf(a.totals))
			.map((g) => ({ label: g.account, value: tokensOf(g.totals), sublabel: `${n(g.totals.n)} req` }))
	);
</script>

<h2>Accounts by total tokens</h2>
<RankedBarChart items={ranked} formatValue={n} color="var(--series-1)" />

<h2 class="spaced">By account</h2>
<div class="grid">
	{#each accountGroups as g (g.account)}
		<div class="card">
			<div class="head">
				<span class="name" title={g.account}>{g.account}</span>
				<span class="badges">
					{#each g.harnesses as h}<span class="badge"><i style="background: {harnessVar(h)}"></i>{harnessLabel(h)}</span>{/each}
				</span>
			</div>
			<div class="row">
				<div class="stat"><small>Requests</small><b>{n(g.totals.n)}</b></div>
				<div class="stat"><small>Tokens</small><b>{n(tokensOf(g.totals))}</b></div>
				<div class="stat"><small>Errors</small><b class:error={g.totals.errors > 0}>{pct(errorRate(g.totals), 1)}</b></div>
				<div class="stat"><small>Avg latency</small><b>{Math.round(avgMs(g.totals))}ms</b></div>
			</div>
			{#if g.trend.length >= 2}
				<div class="spark"><Sparkline points={g.trend} color={g.color} width={200} height={32} /></div>
			{/if}
		</div>
	{:else}
		<p class="empty">No accounts in this window.</p>
	{/each}
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
	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
		gap: 12px;
	}
	.card {
		border: 1px solid var(--border);
		background: var(--surface);
		border-radius: 8px;
		padding: 14px 16px;
	}
	.head {
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		gap: 8px;
		margin-bottom: 10px;
	}
	.name {
		font-weight: 600;
		font-size: 13px;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.badges {
		display: flex;
		gap: 8px;
		flex-shrink: 0;
	}
	.badge {
		font-size: 10px;
		font-weight: 600;
		letter-spacing: 0.03em;
		color: var(--text-secondary);
		display: inline-flex;
		align-items: center;
		gap: 4px;
	}
	.badge i {
		width: 7px;
		height: 7px;
		border-radius: 2px;
		display: inline-block;
	}
	.row {
		display: grid;
		grid-template-columns: repeat(4, 1fr);
		gap: 8px;
	}
	.stat small {
		display: block;
		color: var(--text-secondary);
		font-size: 10px;
	}
	.stat b {
		font-size: 15px;
		font-weight: 600;
	}
	.stat b.error {
		color: var(--status-critical);
	}
	.spark {
		margin-top: 10px;
		opacity: 0.9;
	}
	.spark :global(svg) {
		width: 100%;
		height: 28px;
	}
	.empty {
		color: var(--text-muted);
		font-size: 13px;
	}
</style>
