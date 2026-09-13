<script lang="ts">
	import { thinTicks, harnessLabel, harnessVar, shortModel } from '../usage';
	import { dayLabel, hourLabel, timeLabel, when, duration, n } from '../format';
	import type { Session } from '../sessions';

	let {
		sessions,
		from,
		to,
		now,
		isLive,
		granularity
	}: {
		sessions: Session[];
		from: number;
		to: number;
		now: number;
		isLive: (s: Session) => boolean;
		granularity: 'hour' | 'day';
	} = $props();

	const LANE_H = 30;
	const BAR_H = 14;
	const MAX_LANES = 8;
	const MAX_BARS = 500;
	const AXIS_H = 22;

	let containerWidth = $state(0);
	let hover = $state<string | null>(null);
	let svgEl: SVGSVGElement = $state()!;

	function onMove(e: PointerEvent) {
		if (!svgEl) return;
		const rect = svgEl.getBoundingClientRect();
		const px = e.clientX - rect.left;
		const py = e.clientY - rect.top;
		const lane = Math.floor(py / LANE_H);
		if (lane < 0 || lane >= lanes.length || px < labelW) {
			hover = null;
			return;
		}
		// Narrowest bar under the pointer wins, so a short session inside a long one stays reachable.
		let best: Session | null = null;
		let bestW = Infinity;
		for (const s of drawn) {
			if (laneIndex(s.account) !== lane) continue;
			const x0 = xFor(s.start);
			const x1 = Math.max(x0 + 3, xFor(s.end));
			if (px >= x0 - 4 && px <= x1 + 4 && x1 - x0 < bestW) {
				best = s;
				bestW = x1 - x0;
			}
		}
		hover = best ? best.id : null;
	}

	const labelW = $derived(containerWidth < 600 ? 90 : 150);
	const plotW = $derived(Math.max(0, containerWidth - labelW - 8));
	const span = $derived(Math.max(1, to - from));

	const accounts = $derived([...new Set(sessions.map((s) => s.account))].sort());
	const lanes = $derived.by(() => {
		if (accounts.length <= MAX_LANES) return accounts;
		return [...accounts.slice(0, MAX_LANES - 1), 'Other accounts'];
	});
	const laneOf = $derived(new Map(lanes.map((a, i) => [a, i])));
	function laneIndex(account: string): number {
		return laneOf.get(account) ?? lanes.length - 1;
	}

	const drawn = $derived(sessions.slice(0, MAX_BARS));
	const height = $derived(lanes.length * LANE_H + AXIS_H);

	function xFor(t: number): number {
		return labelW + ((Math.max(from, Math.min(to, t)) - from) / span) * plotW;
	}

	const tickCount = $derived(Math.max(2, Math.floor(plotW / 80)));
	const ticks = $derived.by(() => {
		const step = granularity === 'hour' ? 3_600_000 : 86_400_000;
		const d = new Date(from);
		if (granularity === 'hour') d.setMinutes(0, 0, 0);
		else d.setHours(0, 0, 0, 0);
		const all: number[] = [];
		for (let t = d.getTime(); t <= to; t += step) if (t >= from) all.push(t);
		if (!all.length) all.push(from);
		return thinTicks(all.length, tickCount).map((i) => all[i]);
	});

	const hoveredSession = $derived(hover === null ? null : (drawn.find((s) => s.id === hover) ?? null));
	const tooltipLeft = $derived.by(() => {
		if (!hoveredSession || !containerWidth) return 0;
		return Math.min(70, Math.max(0, (100 * xFor(hoveredSession.start)) / containerWidth));
	});
	const tooltipTop = $derived(hoveredSession ? laneIndex(hoveredSession.account) * LANE_H + LANE_H : 0);
</script>

<div class="timeline" bind:clientWidth={containerWidth}>
	<div class="legend">
		<span class="key"><i style="background: var(--harness-claude)"></i>Claude</span>
		<span class="key"><i style="background: var(--harness-codex)"></i>Codex</span>
		<span class="key"><i class="live"></i>Live</span>
	</div>
	{#if containerWidth > 0 && sessions.length > 0}
		<svg bind:this={svgEl} width={containerWidth} {height} viewBox="0 0 {containerWidth} {height}" role="img" aria-label="Session timeline" onpointermove={onMove} onpointerleave={() => (hover = null)}>
			{#each lanes as account, i}
				<line x1={labelW} x2={containerWidth} y1={(i + 1) * LANE_H} y2={(i + 1) * LANE_H} stroke="var(--grid)" stroke-width="1" />
				<text x={labelW - 8} y={i * LANE_H + LANE_H / 2 + 3.5} text-anchor="end" class="lane"><title>{account}</title>{account.length > (labelW < 100 ? 12 : 22) ? account.slice(0, labelW < 100 ? 11 : 21) + '…' : account}</text>
			{/each}
			{#each ticks as t}
				<line x1={xFor(t)} x2={xFor(t)} y1={0} y2={lanes.length * LANE_H} stroke="var(--grid)" stroke-width="1" />
				<text x={xFor(t)} y={height - 6} text-anchor="middle" class="axis">{granularity === 'hour' ? hourLabel(t) : dayLabel(t)}</text>
			{/each}
			<line x1={labelW} x2={containerWidth} y1={lanes.length * LANE_H} y2={lanes.length * LANE_H} stroke="var(--baseline)" stroke-width="1" />
			{#if now >= from && now <= to}
				<line x1={xFor(now)} x2={xFor(now)} y1={0} y2={lanes.length * LANE_H} stroke="var(--baseline)" stroke-width="1" />
			{/if}

			{#each drawn as s (s.id)}
				{@const x0 = xFor(s.start)}
				{@const x1 = Math.max(x0 + 3, xFor(s.end))}
				{@const y = laneIndex(s.account) * LANE_H + (LANE_H - BAR_H) / 2}
				<rect x={x0} {y} width={x1 - x0} height={BAR_H} rx="3" fill={harnessVar(s.harness)} opacity={hover === null || hover === s.id ? 0.85 : 0.4} />
				{#if isLive(s)}
					<circle cx={x1} cy={y + BAR_H / 2} r="4" fill="var(--status-good)" stroke="var(--surface)" stroke-width="2" />
				{/if}
			{/each}
		</svg>
		{#if hoveredSession}
			{@const s = hoveredSession}
			<div class="tooltip" style="left: {tooltipLeft}%; top: {tooltipTop}px">
				<div class="t-date">{s.account}</div>
				<div class="t-row"><span class="t-label"><i style="background: {harnessVar(s.harness)}"></i>{harnessLabel(s.harness)}{isLive(s) ? ' (live)' : ''}</span></div>
				<div class="t-row"><span class="t-label">Started</span><span class="t-value tabular">{when(s.start)}</span></div>
				<div class="t-row"><span class="t-label">Ended</span><span class="t-value tabular">{timeLabel(s.end)}</span></div>
				<div class="t-row"><span class="t-label">Duration</span><span class="t-value tabular">{duration(s.end - s.start)}</span></div>
				<div class="t-row"><span class="t-label">Requests</span><span class="t-value tabular">{n(s.n)}</span></div>
				<div class="t-row"><span class="t-label">Tokens</span><span class="t-value tabular">{n(s.tokens)}</span></div>
				<div class="t-row"><span class="t-label">Models</span><span class="t-value">{s.models.map((m) => `${shortModel(m.model)} ×${m.n}`).join(', ')}</span></div>
			</div>
		{/if}
		{#if sessions.length > MAX_BARS}
			<p class="note">Drawing the {MAX_BARS} most recent of {n(sessions.length)} sessions.</p>
		{/if}
		{#if accounts.length > MAX_LANES}
			<p class="note">{accounts.length - MAX_LANES + 1} accounts share the "Other accounts" lane.</p>
		{/if}
	{:else}
		<div class="empty">No sessions in this window.</div>
	{/if}
</div>

<style>
	.timeline {
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
		width: 10px;
		height: 8px;
		border-radius: 2px;
	}
	.key i.live {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: var(--status-good);
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
	.lane {
		fill: var(--text-secondary);
		font-size: 11px;
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
		min-width: 200px;
		max-width: 320px;
		z-index: 2;
	}
	.t-date {
		color: var(--text-secondary);
		font-size: 11px;
		margin-bottom: 4px;
		overflow: hidden;
		text-overflow: ellipsis;
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
		flex-shrink: 0;
	}
	.t-label i {
		width: 8px;
		height: 8px;
		border-radius: 2px;
		display: inline-block;
	}
	.t-value {
		font-weight: 600;
		text-align: right;
	}
	.note {
		color: var(--text-muted);
		font-size: 11px;
		margin: 6px 0 0;
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
