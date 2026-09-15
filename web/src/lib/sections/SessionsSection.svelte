<script lang="ts">
	import StatTile from '../charts/StatTile.svelte';
	import Sparkline from '../charts/Sparkline.svelte';
	import SessionTimeline from '../charts/SessionTimeline.svelte';
	import { n, pct, when, duration, usd, timeLabel, ms as msFmt } from '../format';
	import { percentile, harnessLabel, harnessVar, modelSlots, modelColor, shortModel, contextOf, minTime, maxOf, type Row, type Granularity } from '../usage';
	import { sessionize, isLive, type Session } from '../sessions';
	import { priceFor, costOf } from '../pricing';
	import { prices } from '../prices.svelte';

	let {
		rows,
		days,
		now,
		since,
		granularity
	}: {
		rows: Row[];
		days: number;
		now: number;
		since: number;
		granularity: Granularity;
	} = $props();

	const GAPS = [10, 30, 60] as const;
	const TABLE_LIMIT = 200;
	const DETAIL_LIMIT = 100;

	let gapMin = $state<(typeof GAPS)[number]>(30);
	let expanded = $state<string | null>(null);

	const gapMs = $derived(gapMin * 60_000);
	const sessions = $derived(sessionize(rows, gapMs));
	const live = $derived(sessions.filter((s) => isLive(s, now, gapMs)));
	const lengths = $derived(sessions.map((s) => s.end - s.start));
	const longest = $derived(maxOf(lengths));

	const from = $derived(since || minTime(rows, now));
	const models = $derived([...new Set(rows.map((r) => r.model || '(unknown)'))]);
	const slots = $derived(modelSlots(models));

	function costOfSession(s: Session): number | null {
		let total = 0;
		let priced = false;
		for (const [model, rs] of Map.groupBy(s.rows, (r) => r.model || '(unknown)')) {
			const p = priceFor(model, prices.catalog).price;
			if (!p) continue;
			priced = true;
			for (const r of rs) total += costOf(r, p).total;
		}
		return priced ? total : null;
	}

	const shown = $derived(sessions.slice(0, TABLE_LIMIT));
	function toggle(id: string) {
		expanded = expanded === id ? null : id;
	}
	function onKey(e: KeyboardEvent, id: string) {
		if (e.key === 'Enter' || e.key === ' ') {
			e.preventDefault();
			toggle(id);
		}
	}
</script>

<div class="controls">
	<nav class="seg" aria-label="Idle gap that ends a session">
		{#each GAPS as g}
			<button class:active={gapMin === g} onclick={() => (gapMin = g)}>{g} min gap</button>
		{/each}
	</nav>
	<span class="caption">A session is a run of requests from one account and harness; it ends after {gapMin} minutes without a request.</span>
</div>

<section class="tiles">
	<StatTile label="Sessions" value={n(sessions.length)} accent="var(--series-1)" />
	<StatTile label="Live now" value={n(live.length)} accent={live.length ? 'var(--status-good)' : 'var(--series-1)'} />
	<StatTile label="Median length" value={sessions.length ? duration(percentile(lengths, 50)) : '—'} accent="var(--series-1)" />
	<StatTile label="Median requests" value={sessions.length ? n(percentile(sessions.map((s) => s.n), 50)) : '—'} accent="var(--series-1)" />
	<StatTile label="Median tokens" value={sessions.length ? n(percentile(sessions.map((s) => s.tokens), 50)) : '—'} accent="var(--series-3)" />
	<StatTile label="Longest session" value={sessions.length ? duration(longest) : '—'} accent="var(--series-2)" />
</section>

<h2>Timeline</h2>
<SessionTimeline {sessions} {from} to={now} {now} isLive={(s) => isLive(s, now, gapMs)} {granularity} />
<p class="caption">One lane per account. Green dot: possibly still running.</p>

<h2 class="spaced">Sessions</h2>
<div class="scroll">
	<table>
		<thead>
			<tr>
				<th>Start</th>
				<th>Account</th>
				<th>Harness</th>
				<th class="num">Duration</th>
				<th class="num">Requests</th>
				<th>Models</th>
				<th class="num">Tokens</th>
				<th class="num">Output</th>
				<th class="num">Peak context</th>
				<th class="num">Est. cost</th>
				<th class="num">Errors</th>
				<th>Context</th>
			</tr>
		</thead>
		<tbody>
			{#each shown as s (s.id)}
				{@const cost = costOfSession(s)}
				{@const open = expanded === s.id}
				<tr class="row" class:open tabindex="0" role="button" aria-expanded={open} onclick={() => toggle(s.id)} onkeydown={(e) => onKey(e, s.id)}>
					<td>{when(s.start)}{#if isLive(s, now, gapMs)}<span class="live" title="May still be going">live</span>{/if}</td>
					<td class="account" title={s.account}>{s.account}</td>
					<td><i class="dot" style="background: {harnessVar(s.harness)}"></i>{harnessLabel(s.harness)}</td>
					<td class="num">{duration(s.end - s.start)}</td>
					<td class="num">{n(s.n)}</td>
					<td class="chips">
						{#each s.models as m (m.model)}
							<span class="chip" title={m.model}><i class="dot" style="background: {modelColor(slots, m.model)}"></i>{shortModel(m.model)} ×{m.n}</span>
						{/each}
					</td>
					<td class="num">{n(s.tokens)}</td>
					<td class="num">{n(s.output)}</td>
					<td class="num">{n(s.peakContext)}</td>
					<td class="num">{cost === null ? '—' : usd(cost)}</td>
					<td class="num" class:error={s.errors > 0}>{s.errors}</td>
					<td class="spark">{#if s.contextSeries.length >= 2}<Sparkline points={s.contextSeries} color={harnessVar(s.harness)} width={120} height={20} />{/if}</td>
				</tr>
				{#if open}
					<tr class="detail">
						<td colspan="12">
							<div class="kv">
								<div><small>Input</small><b>{n(s.input)}</b></div>
								<div><small>Cache read</small><b>{n(s.cache_read)}</b></div>
								<div><small>Cache write</small><b>{n(s.cache_write)}</b></div>
								<div><small>Output</small><b>{n(s.output)}</b></div>
								<div><small>Model busy</small><b>{duration(s.busyMs)}</b></div>
								<div><small>Busy share of wall clock</small><b>{s.end > s.start ? pct(Math.min(999, (100 * s.busyMs) / (s.end - s.start))) : '—'}</b></div>
								<div><small>Ended</small><b>{timeLabel(s.end)}</b></div>
							</div>
							<table class="inner">
								<thead>
									<tr><th>Time</th><th>Model</th><th class="num">Status</th><th class="num">Latency</th><th class="num">Context</th><th class="num">Output</th></tr>
								</thead>
								<tbody>
									{#each s.rows.slice(0, DETAIL_LIMIT) as r, i (i)}
										<tr class:error={r.status >= 400}>
											<td>{timeLabel(r.time)}</td>
											<td>{r.model || '—'}</td>
											<td class="num">{r.status}</td>
											<td class="num">{msFmt(r.ms)}</td>
											<td class="num">{n(contextOf(r))}</td>
											<td class="num">{n(r.output)}</td>
										</tr>
									{/each}
								</tbody>
							</table>
							{#if s.rows.length > DETAIL_LIMIT}<p class="note">First {DETAIL_LIMIT} of {n(s.rows.length)} requests.</p>{/if}
							<p class="note">Busy over 100% means overlapping requests.</p>
						</td>
					</tr>
				{/if}
			{:else}
				<tr><td colspan="12">No sessions in this window.</td></tr>
			{/each}
		</tbody>
	</table>
</div>
{#if sessions.length > TABLE_LIMIT}
	<p class="note">Latest {TABLE_LIMIT} of {n(sessions.length)} sessions.</p>
{/if}

<style>
	.controls {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 12px;
		margin-bottom: 16px;
	}
	.seg {
		display: flex;
		gap: 4px;
	}
	.seg button {
		font: inherit;
		padding: 4px 12px;
		border-radius: 6px;
		cursor: pointer;
		border: 1px solid var(--border);
		background: none;
		color: inherit;
	}
	.seg button.active {
		background: var(--text-primary);
		color: var(--page);
		border-color: var(--text-primary);
	}
	.tiles {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
		gap: 12px;
		margin-bottom: 28px;
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
	.controls .caption {
		margin: 0;
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
		vertical-align: middle;
	}
	th {
		font-weight: 500;
		color: var(--text-secondary);
		font-variant-numeric: normal;
	}
	.num {
		text-align: right;
	}
	.row {
		cursor: pointer;
	}
	.row:hover td,
	.row:focus-visible td {
		background: color-mix(in srgb, var(--text-primary) 4%, transparent);
	}
	.row:focus-visible {
		outline: 2px solid var(--series-1);
		outline-offset: -2px;
	}
	.row.open td {
		border-bottom-color: transparent;
	}
	.account {
		max-width: 200px;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.dot {
		display: inline-block;
		width: 8px;
		height: 8px;
		border-radius: 2px;
		margin-right: 6px;
		vertical-align: middle;
	}
	.chips {
		white-space: normal;
		min-width: 180px;
	}
	.chip {
		display: inline-flex;
		align-items: center;
		font-size: 11px;
		color: var(--text-secondary);
		border: 1px solid var(--border);
		border-radius: 10px;
		padding: 1px 7px;
		margin: 1px 4px 1px 0;
	}
	.live {
		margin-left: 8px;
		font-size: 10px;
		font-weight: 600;
		letter-spacing: 0.03em;
		color: var(--status-good);
	}
	.error {
		color: var(--status-critical);
	}
	.spark :global(svg) {
		display: block;
	}
	.detail td {
		background: color-mix(in srgb, var(--text-primary) 3%, transparent);
		white-space: normal;
	}
	.kv {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(130px, 1fr));
		gap: 10px;
		margin: 4px 0 12px;
	}
	.kv small {
		display: block;
		color: var(--text-secondary);
		font-size: 10px;
	}
	.kv b {
		font-size: 14px;
		font-weight: 600;
	}
	.inner {
		width: auto;
		min-width: 420px;
		font-size: 12px;
	}
	.inner th,
	.inner td {
		padding: 3px 8px;
	}
	.note {
		color: var(--text-muted);
		font-size: 11px;
		margin: 8px 0 0;
	}
</style>
