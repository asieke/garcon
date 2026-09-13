import { monthLabel } from './format';
import type { Row } from './usage';

/** Picks the number a row contributes to a cell (1 for request counts, tokensOf for tokens, …). */
export type ValueFn = (r: Row) => number;

/** Monday-first weekday index (0 = Monday … 6 = Sunday) for a local-time date. JS getDay() puts
 * Sunday at 0, so the remap is a rotation by one. */
export function mondayIndex(t: number | Date): number {
	const d = typeof t === 'number' ? new Date(t) : t;
	return (d.getDay() + 6) % 7;
}

/** Inverse of mondayIndex: the JS getDay() value (Sunday = 0) for a Monday-first row index, so
 * weekdayLabel() from format.ts can be reused on Monday-first rows. */
export function jsDayFromMonday(row: number): number {
	return (row + 1) % 7;
}

/** Local midnight of the calendar day containing `t`. */
export function dayStart(t: number): number {
	return new Date(t).setHours(0, 0, 0, 0);
}

/** Local midnight `k` calendar days after `t`. Steps by calendar date rather than by 86,400,000 ms
 * so a DST change never shifts the result off midnight. */
export function addDays(t: number, k: number): number {
	const d = new Date(t);
	d.setDate(d.getDate() + k);
	d.setHours(0, 0, 0, 0);
	return d.getTime();
}

/** 7 × 24 matrix (row 0 = Monday … row 6 = Sunday; column = local hour of day) summing `value`
 * over every row that falls in that hour of that weekday. Always fully populated with zeros. */
export function hourWeekdayMatrix(rows: Row[], value: ValueFn): number[][] {
	const m = Array.from({ length: 7 }, () => new Array<number>(24).fill(0));
	for (const r of rows) {
		const d = new Date(r.time);
		m[mondayIndex(d)][d.getHours()] += value(r);
	}
	return m;
}

/** Sum of `value` per local calendar day, keyed by the day's midnight timestamp. */
export function dailyTotals(rows: Row[], value: ValueFn): Map<number, number> {
	const map = new Map<number, number>();
	for (const r of rows) {
		const d = dayStart(r.time);
		map.set(d, (map.get(d) ?? 0) + value(r));
	}
	return map;
}

export type CalendarGrid = {
	/** One entry per week column; each is 7 cells Monday-first. Null = outside [from, to]. */
	weeks: (number | null)[][];
	/** Parallel to `weeks`: the midnight timestamp of each cell's day (null outside the window). */
	dayStarts: (number | null)[][];
	/** One per week column: the short month name when that week is the first column or contains
	 * an in-window first-of-month, else null. */
	monthLabels: (string | null)[];
	/** One per week column: the Monday midnight that starts it (always set, even when that Monday
	 * itself is outside the window). */
	weekStarts: number[];
};

/** Lays the days from `from` to `to` (inclusive, by local calendar day) out as Monday-first week
 * columns, GitHub-contribution style. Days in the first and last columns that fall outside the
 * window are null so the grid stays rectangular. */
export function calendarGrid(from: number, to: number, totals: Map<number, number>): CalendarGrid {
	const first = dayStart(from);
	const last = dayStart(to);
	const weeks: (number | null)[][] = [];
	const dayStarts: (number | null)[][] = [];
	const monthLabels: (string | null)[] = [];
	const weekStarts: number[] = [];
	if (last < first) return { weeks, dayStarts, monthLabels, weekStarts };

	for (let weekStart = addDays(first, -mondayIndex(first)); weekStart <= last; weekStart = addDays(weekStart, 7)) {
		const week: (number | null)[] = [];
		const starts: (number | null)[] = [];
		let label: string | null = null;
		for (let i = 0; i < 7; i++) {
			const day = addDays(weekStart, i);
			const inWindow = day >= first && day <= last;
			week.push(inWindow ? (totals.get(day) ?? 0) : null);
			starts.push(inWindow ? day : null);
			if (inWindow && label === null && new Date(day).getDate() === 1) label = monthLabel(day);
		}
		if (label === null && weeks.length === 0) label = monthLabel(first);
		weeks.push(week);
		dayStarts.push(starts);
		monthLabels.push(label);
		weekStarts.push(weekStart);
	}
	return { weeks, dayStarts, monthLabels, weekStarts };
}

/** Consecutive-day runs over the days that had activity. `longest` is the longest run anywhere in
 * the input; `current` is the run ending at the most recent active day, or 0 when that day is
 * before today (relative to `now`). Input days may be any timestamps within the day. */
export function streaks(activeDays: Iterable<number>, now: number): { current: number; longest: number } {
	const sorted = [...new Set([...activeDays].map(dayStart))].sort((a, b) => a - b);
	if (!sorted.length) return { current: 0, longest: 0 };
	let longest = 1;
	let run = 1;
	for (let i = 1; i < sorted.length; i++) {
		run = sorted[i] === addDays(sorted[i - 1], 1) ? run + 1 : 1;
		if (run > longest) longest = run;
	}
	const current = sorted[sorted.length - 1] === dayStart(now) ? run : 0;
	return { current, longest };
}
