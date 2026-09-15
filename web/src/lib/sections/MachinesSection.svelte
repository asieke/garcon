<script lang="ts">
	import TimeSeriesChart from '../charts/TimeSeriesChart.svelte';
	import RankedBarChart from '../charts/RankedBarChart.svelte';
	import Sparkline from '../charts/Sparkline.svelte';
	import { n, pct, when } from '../format';
	import { tokensOf, avgMs, errorRate, harnessLabel, harnessVar, type Totals, type Granularity } from '../usage';

	export type MachineGroup = {
		machine: string;
		/** True for the machine whose dashboard this is, so it can be marked among the others. */
		local: boolean;
		harnesses: string[];
		accounts: string[];
		totals: Totals;
		trend: number[];
		lastTime: number;
		color: string;
	};

	let {
		machineGroups,
		machineTokenSeries,
		buckets,
		granularity
	}: {
		machineGroups: MachineGroup[];
		machineTokenSeries: { key: string; label: string; color: string; values: number[] }[];
		buckets: number[];
		granularity: Granularity;
	} = $props();

	const ranked = $derived(
		[...machineGroups]
			.sort((a, b) => tokensOf(b.totals) - tokensOf(a.totals))
			.map((g) => ({ label: g.machine, value: tokensOf(g.totals), sublabel: `${n(g.totals.n)} req` }))
	);
	const grand = $derived(machineGroups.reduce((s, g) => s + tokensOf(g.totals), 0));
</script>

<h2>Token throughput by machine</h2>
<TimeSeriesChart {buckets} {granularity} mode="stacked-area" formatValue={n} series={machineTokenSeries} />

<h2 class="spaced">Machines by total tokens</h2>
<RankedBarChart items={ranked} formatValue={n} />

<h2 class="spaced">By machine</h2>
<div class="grid">
	{#each machineGroups as g (g.machine)}
		<div class="card">
			<div class="head">
				<span class="name"><i class="swatch" style="background: {g.color}"></i><span class="label" title={g.machine}>{g.machine}</span>{#if g.local}<span class="tag">this machine</span>{/if}</span>
				<span class="badges">
					{#each g.harnesses as h}<span class="badge"><i style="background: {harnessVar(h)}"></i>{harnessLabel(h)}</span>{/each}
				</span>
			</div>
			<div class="row">
				<div class="stat"><small>Requests</small><b>{n(g.totals.n)}</b></div>
				<div class="stat"><small>Tokens</small><b>{n(tokensOf(g.totals))}</b></div>
				<div class="stat"><small>Share</small><b>{pct(grand ? (100 * tokensOf(g.totals)) / grand : 0, 0)}</b></div>
				<div class="stat"><small>Errors</small><b class:error={g.totals.errors > 0}>{pct(errorRate(g.totals), 1)}</b></div>
				<div class="stat"><small>Avg latency</small><b>{Math.round(avgMs(g.totals))}ms</b></div>
				<div class="stat"><small>Last request</small><b>{when(g.lastTime)}</b></div>
			</div>
			<p class="accounts" title={g.accounts.join(', ')}>{g.accounts.length === 1 ? '1 account' : `${g.accounts.length} accounts`}: {g.accounts.join(', ')}</p>
			{#if g.trend.length >= 2}
				<div class="spark"><Sparkline points={g.trend} color={g.color} width={200} height={32} /></div>
			{/if}
		</div>
	{:else}
		<p class="empty">No machines in this window.</p>
	{/each}
</div>
<p class="caption">Rows from other machines arrive through sync; each machine is labelled with the device name set in its own Settings.</p>

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
		grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
		gap: 12px;
	}
	.card {
		border: 1px solid var(--border);
		background: var(--surface);
		border-radius: 8px;
		padding: 14px 16px;
	}
	/* Name and badges stack so a long device name is never squeezed by the harness list. */
	.head {
		display: flex;
		flex-direction: column;
		gap: 6px;
		margin-bottom: 12px;
	}
	.name {
		display: flex;
		align-items: center;
		gap: 7px;
		min-width: 0;
		font-weight: 600;
		font-size: 13px;
	}
	.label {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.tag {
		flex-shrink: 0;
	}
	.swatch {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		flex-shrink: 0;
	}
	.tag {
		font-size: 10px;
		font-weight: 600;
		letter-spacing: 0.03em;
		color: var(--accent);
		background: var(--accent-soft);
		border: 1px solid var(--accent-border);
		border-radius: 999px;
		padding: 1px 7px;
	}
	.badges {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
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
		grid-template-columns: repeat(3, 1fr);
		gap: 10px 8px;
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
	.accounts {
		margin: 10px 0 0;
		font-size: 11px;
		color: var(--text-secondary);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.spark {
		margin-top: 10px;
		opacity: 0.9;
	}
	.spark :global(svg) {
		width: 100%;
		height: 28px;
	}
	.empty,
	.caption {
		color: var(--text-muted);
		font-size: 13px;
	}
	.caption {
		margin-top: 16px;
		font-size: 12px;
	}
</style>
