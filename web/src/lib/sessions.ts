import { contextOf, tokensOf, type Row } from './usage';

/** A run of requests from one harness+account with no idle gap longer than the threshold.
 * Garcon never sees a session id, so this is inferred from timing alone: a coding session is a
 * burst of calls, and a long silence means the person walked away or started something else. */
export type Session = {
	id: string;
	harness: string;
	account: string;
	start: number;
	/** Wall-clock end: last request's start plus its duration. */
	end: number;
	rows: Row[];
	n: number;
	errors: number;
	tokens: number;
	input: number;
	cache_read: number;
	cache_write: number;
	output: number;
	/** Largest context (input + cache read + cache write) of any single request. */
	peakContext: number;
	/** Context size of each request in time order, for a growth sparkline. */
	contextSeries: number[];
	/** Request counts per model, largest first. */
	models: { model: string; n: number }[];
	/** Sum of request durations: "model busy" time, as opposed to wall-clock length. */
	busyMs: number;
};

export const DEFAULT_GAP_MS = 30 * 60_000;

export function sessionize(rows: Row[], gapMs = DEFAULT_GAP_MS): Session[] {
	const byKey = new Map<string, Row[]>();
	for (const r of rows) {
		const key = `${r.harness}|${r.account}`;
		const list = byKey.get(key);
		if (list) list.push(r);
		else byKey.set(key, [r]);
	}
	const sessions: Session[] = [];
	for (const list of byKey.values()) {
		list.sort((a, b) => a.time - b.time);
		let current: Row[] = [];
		let lastEnd = -Infinity;
		for (const r of list) {
			if (current.length && r.time - lastEnd > gapMs) {
				sessions.push(build(current));
				current = [];
			}
			current.push(r);
			lastEnd = Math.max(lastEnd, r.time + r.ms);
		}
		if (current.length) sessions.push(build(current));
	}
	return sessions.sort((a, b) => b.start - a.start);
}

function build(rows: Row[]): Session {
	const first = rows[0];
	let end = first.time;
	let errors = 0;
	let input = 0;
	let cache_read = 0;
	let cache_write = 0;
	let output = 0;
	let peakContext = 0;
	let busyMs = 0;
	const modelCounts = new Map<string, number>();
	const contextSeries: number[] = [];
	for (const r of rows) {
		end = Math.max(end, r.time + r.ms);
		if (r.status >= 400) errors++;
		input += r.input;
		cache_read += r.cache_read;
		cache_write += r.cache_write;
		output += r.output;
		busyMs += r.ms;
		const ctx = contextOf(r);
		if (ctx > peakContext) peakContext = ctx;
		contextSeries.push(ctx);
		const m = r.model || '(unknown)';
		modelCounts.set(m, (modelCounts.get(m) ?? 0) + 1);
	}
	return {
		id: `${first.harness}:${first.account}:${first.time}`,
		harness: first.harness,
		account: first.account,
		start: first.time,
		end,
		rows,
		n: rows.length,
		errors,
		tokens: rows.reduce((s, r) => s + tokensOf(r), 0),
		input,
		cache_read,
		cache_write,
		output,
		peakContext,
		contextSeries,
		models: [...modelCounts].map(([model, n]) => ({ model, n })).sort((a, b) => b.n - a.n),
		busyMs
	};
}

/** True when the session's last activity is within one gap of `now`: it may still be going. */
export function isLive(s: Session, now: number, gapMs = DEFAULT_GAP_MS): boolean {
	return now - s.end <= gapMs;
}
