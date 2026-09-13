import { contextOf, genSpeed, percentile, shortModel, type Row } from './usage';
import { niceCeil } from './charts/niceScale';

export type Bins = { edges: number[]; counts: number[] };

const NICE_STEPS = [1, 2, 2.5, 5, 10];

/** Trim floating-point noise from a computed edge (0.30000000000000004 → 0.3). */
function clean(v: number): number {
	return Number(v.toPrecision(12));
}

/** Smallest "nice" number (1/2/2.5/5 × a power of ten) that is ≥ raw, so bin widths read as
 * round figures ("0–25K") instead of an arbitrary fraction of the maximum. */
export function niceStepFor(raw: number): number {
	if (!(raw > 0) || !isFinite(raw)) return 1;
	const magnitude = 10 ** Math.floor(Math.log10(raw));
	const normalized = raw / magnitude;
	const step = NICE_STEPS.find((s) => s >= normalized - 1e-9) ?? 10;
	return clean(step * magnitude);
}

/** Largest value, without spreading a possibly huge array into Math.max. */
export function maxOf(values: number[]): number {
	let max = 0;
	for (const v of values) if (v > max) max = v;
	return max;
}

/** Equal-width bins from 0 to a nice ceiling of the maximum. With `niceStep` the bin width is
 * itself a nice number, so the bin count is at most `binCount` and the edges are round figures;
 * without it the width is exactly max/binCount. Values above the top edge land in the last bin,
 * values below zero in the first. Empty input gives empty bins. */
export function linearBins(values: number[], binCount: number, niceStep = true): Bins {
	if (!values.length || binCount < 1) return { edges: [], counts: [] };
	const max = maxOf(values);
	const top = niceStep ? niceCeil(max) : max > 0 ? max : 1;
	const step = niceStep ? niceStepFor(top / binCount) : top / binCount;
	const count = Math.max(1, Math.ceil(top / step - 1e-9));
	const edges = Array.from({ length: count + 1 }, (_, i) => clean(i * step));
	const counts = new Array<number>(count).fill(0);
	for (const v of values) {
		const idx = Math.min(count - 1, Math.max(0, Math.floor(v / step + 1e-9)));
		counts[idx]++;
	}
	return { edges, counts };
}

/** Logarithmic bins with `perDecade` edges per factor of `base` (the default gives 1, 3, 10, 30,
 * 100, …), covering the smallest positive value up to the largest. Values ≤ 0 go in the first
 * bin; values at or above the top edge go in the last. Empty input gives empty bins. */
export function logBins(values: number[], base = 10, perDecade = 2): Bins {
	if (!values.length) return { edges: [], counts: [] };
	let minPos = Infinity;
	let max = 0;
	for (const v of values) {
		if (v > 0 && v < minPos) minPos = v;
		if (v > max) max = v;
	}
	if (!isFinite(minPos)) {
		minPos = 1;
		max = 1;
	}
	const steps = Math.max(1, Math.round(perDecade));
	const mantissas = [
		...new Set(Array.from({ length: steps }, (_, j) => Math.round(base ** (j / steps))))
	]
		.filter((m) => m >= 1 && m < base)
		.sort((a, b) => a - b);
	const logBase = Math.log(base);
	// Span one decade beyond each end, then trim to the largest edge ≤ min and the smallest ≥ max.
	const k0 = Math.floor(Math.log(minPos) / logBase) - 1;
	const k1 = Math.ceil(Math.log(max) / logBase) + 1;
	const all: number[] = [];
	for (let k = k0; k <= k1; k++) for (const m of mantissas) all.push(clean(m * base ** k));
	let lo = 0;
	for (let i = 0; i < all.length; i++) if (all[i] <= minPos) lo = i;
	let hi = all.length - 1;
	for (let i = all.length - 1; i >= 0; i--) if (all[i] >= max) hi = i;
	if (hi <= lo) hi = Math.min(all.length - 1, lo + 1);
	const edges = all.slice(lo, hi + 1);
	const count = edges.length - 1;
	const counts = new Array<number>(count).fill(0);
	for (const v of values) {
		let idx = 0;
		if (v > 0) {
			while (idx < count - 1 && v >= edges[idx + 1]) idx++;
		}
		counts[idx]++;
	}
	return { edges, counts };
}

export type ModelSpeed = {
	model: string;
	/** All rows for this model in the window. */
	requests: number;
	/** Rows with a measurable generation speed (see genSpeed). */
	n: number;
	/** Rows carrying a first-byte timing. */
	ttftN: number;
	/** ttftN / requests, as a fraction 0..1. */
	measuredShare: number;
	p50Speed: number | null;
	p95Speed: number | null;
	p50Ttft: number | null;
	p95Ttft: number | null;
	p50Total: number;
	p50Output: number;
	p50Context: number;
};

/** Per-model speed and size percentiles. Speed and first-byte percentiles are null (never a
 * misleading 0) when no row of that model has the measurement. Sorted by request count, largest
 * first, ties by model id. */
export function byModelSpeed(rows: Row[]): ModelSpeed[] {
	return [...Map.groupBy(rows, (r) => r.model || '(unknown)')]
		.map(([model, rs]): ModelSpeed => {
			const speeds: number[] = [];
			const ttfts: number[] = [];
			for (const r of rs) {
				const s = genSpeed(r);
				if (s !== null) speeds.push(s);
				if (r.first_byte_ms !== undefined) ttfts.push(r.first_byte_ms);
			}
			return {
				model,
				requests: rs.length,
				n: speeds.length,
				ttftN: ttfts.length,
				measuredShare: rs.length ? ttfts.length / rs.length : 0,
				p50Speed: speeds.length ? percentile(speeds, 50) : null,
				p95Speed: speeds.length ? percentile(speeds, 95) : null,
				p50Ttft: ttfts.length ? percentile(ttfts, 50) : null,
				p95Ttft: ttfts.length ? percentile(ttfts, 95) : null,
				p50Total: percentile(rs.map((r) => r.ms), 50),
				p50Output: percentile(rs.map((r) => r.output), 50),
				p50Context: percentile(rs.map(contextOf), 50)
			};
		})
		.sort((a, b) => b.requests - a.requests || a.model.localeCompare(b.model));
}

/** Display name per model id: shortModel, except where two ids would collapse to the same short
 * name (e.g. two dated snapshots of one model), in which case both keep their full id so keys
 * and labels stay unique. */
export function displayNames(models: Iterable<string>): Map<string, string> {
	const ids = [...new Set(models)];
	const seen = new Map<string, number>();
	for (const id of ids) {
		const s = shortModel(id);
		seen.set(s, (seen.get(s) ?? 0) + 1);
	}
	return new Map(
		ids.map((id) => {
			const s = shortModel(id);
			return [id, (seen.get(s) ?? 0) > 1 ? id : s];
		})
	);
}

/** Model ids ordered by request count, largest first (ties by id), truncated to `limit`. */
export function topModelsByRequests(rows: Row[], limit: number): string[] {
	const counts = new Map<string, number>();
	for (const r of rows) {
		const m = r.model || '(unknown)';
		counts.set(m, (counts.get(m) ?? 0) + 1);
	}
	return [...counts]
		.sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
		.slice(0, limit)
		.map(([m]) => m);
}
