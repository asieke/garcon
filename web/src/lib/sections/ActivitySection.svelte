<script lang="ts">
	import StatTile from '../charts/StatTile.svelte';
	import Heatmap from '../charts/Heatmap.svelte';
	import RankedBarChart from '../charts/RankedBarChart.svelte';
	import { n, hourLabel, dayLabel, weekdayLabel } from '../format';
	import { tokensOf, peakConcurrency, minTime, type Row } from '../usage';
	import { hourWeekdayMatrix, dailyTotals, calendarGrid, streaks, jsDayFromMonday, type ValueFn } from '../activity';

	let {
		rows,
		days,
		now,
		since
	}: {
		rows: Row[];
		days: number;
		now: number;
		since: number;
	} = $props();

	const METRICS = [
		['requests', 'Requests'],
		['tokens', 'Tokens'],
		['output', 'Output tokens']
	] as const;
	let metric = $state<(typeof METRICS)[number][0]>('requests');

	const valueFn = $derived<ValueFn>(
		metric === 'requests' ? () => 1 : metric === 'tokens' ? (r) => tokensOf(r) : (r) => r.output
	);
	const metricLabel = $derived(METRICS.find(([k]) => k === metric)?.[1] ?? 'Requests');

	// Hour × weekday
	const matrix = $derived(hourWeekdayMatrix(rows, valueFn));
	const weekdayRows = Array.from({ length: 7 }, (_, r) => weekdayLabel(jsDayFromMonday(r)));
	const weekdayLong = Array.from({ length: 7 }, (_, r) => new Date(2024, 0, 1 + r).toLocaleDateString('en', { weekday: 'long' }));
	function hourAt(h: number): string {
		return hourLabel(new Date(2024, 0, 1, h).getTime());
	}
	const hourCols = Array.from({ length: 24 }, (_, h) => (h % 6 === 0 ? hourAt(h) : null));
	function hourTitle(r: number, c: number): string {
		return `${weekdayLong[r]}, ${hourAt(c)} to ${hourAt((c + 1) % 24)}`;
	}

	const busiest = $derived.by(() => {
		let best = { r: 0, c: 0, v: 0 };
		matrix.forEach((row, r) => row.forEach((v, c) => { if (v > best.v) best = { r, c, v }; }));
		return best.v > 0 ? best : null;
	});

	// Calendar
	const from = $derived(since || minTime(rows, now));
	const totals = $derived(dailyTotals(rows, valueFn));
	const grid = $derived(calendarGrid(from, now, totals));
	const calendarCells = $derived(weekdayRows.map((_, r) => grid.weeks.map((week) => week[r])));
	function calendarTitle(r: number, c: number): string {
		const t = grid.dayStarts[c]?.[r];
		return t === null || t === undefined ? '' : new Date(t).toLocaleDateString('en', { weekday: 'short', month: 'short', day: 'numeric' });
	}
	const busiestDay = $derived.by(() => {
		let best: { day: number; v: number } | null = null;
		for (const [day, v] of totals) if (v > 0 && (!best || v > best.v)) best = { day, v };
		return best;
	});
	const requestDays = $derived(dailyTotals(rows, () => 1));
	const activeDays = $derived([...requestDays.keys()].filter((d) => (requestDays.get(d) ?? 0) > 0));
	const windowDayCount = $derived(grid.weeks.reduce((s, w) => s + w.filter((v) => v !== null).length, 0));
	const streak = $derived(streaks(activeDays, now));
	const concurrency = $derived(peakConcurrency(rows));

	const byWeekday = $derived(
		matrix.map((row, r) => ({ label: weekdayLong[r], value: row.reduce((s, v) => s + v, 0) }))
	);
	const fmt = $derived((v: number) => (metric === 'requests' ? String(Math.round(v)) : n(v)));
</script>

<div class="controls">
	<nav class="seg" aria-label="Metric">
		{#each METRICS as [key, label]}
			<button class:active={metric === key} onclick={() => (metric = key)}>{label}</button>
		{/each}
	</nav>
</div>

<section class="tiles">
	<StatTile label="Busiest hour" value={busiest ? `${weekdayRows[busiest.r]} ${hourAt(busiest.c)}` : '—'} accent="var(--series-1)" />
	<StatTile label="Busiest day" value={busiestDay ? dayLabel(busiestDay.day) : '—'} accent="var(--series-1)" />
	<StatTile label="Active days" value={windowDayCount ? `${activeDays.length} of ${windowDayCount}` : '—'} accent="var(--series-1)" />
	<StatTile label="Current streak" value={`${streak.current} day${streak.current === 1 ? '' : 's'}`} accent="var(--series-3)" />
	<StatTile label="Longest streak" value={`${streak.longest} day${streak.longest === 1 ? '' : 's'}`} accent="var(--series-3)" />
	<StatTile label="Peak concurrent requests" value={n(concurrency)} accent="var(--series-2)" />
</section>

<h2>{metricLabel} by hour and weekday</h2>
<Heatmap rowLabels={weekdayRows} colLabels={hourCols} cells={matrix} formatValue={fmt} cellTitle={hourTitle} legendLabel={metricLabel} ariaLabel="{metricLabel} by hour of day and weekday" />
<p class="caption">
	Local time. Each cell sums every {metricLabel.toLowerCase()} that started in that hour on that weekday across the window.
	{#if days === 1}Today shows a single day; widen the window to see weekly patterns.{/if}
</p>

<h2 class="spaced">Calendar</h2>
<Heatmap rowLabels={weekdayRows} colLabels={grid.monthLabels} cells={calendarCells} formatValue={fmt} cellTitle={calendarTitle} legendLabel={metricLabel} ariaLabel="{metricLabel} per day" />
<p class="caption">One column per week, Monday at the top. Faint cells fall outside the selected window.</p>

<h2 class="spaced">By weekday</h2>
<RankedBarChart items={byWeekday} formatValue={fmt} color="var(--series-1)" />
<p class="caption">Totals per weekday in calendar order, so the weekly rhythm reads without the grid.</p>

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
</style>
