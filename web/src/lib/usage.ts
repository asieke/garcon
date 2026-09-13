export type Row = {
	time: number;
	harness: string;
	account: string;
	/** Absent on rows logged before multi-provider support; see providerOf. */
	provider?: string;
	model: string;
	status: number;
	ms: number;
	queue_us?: number;
	reused?: boolean;
	connect_ms?: number;
	dns_ms?: number;
	tcp_ms?: number;
	tls_ms?: number;
	first_byte_ms?: number;
	input: number;
	cache_read: number;
	cache_write: number;
	output: number;
	/** The machine that recorded the row: this one, or another device pulled in by sync. */
	device?: string;
};

export type Totals = {
	n: number;
	errors: number;
	ms: number;
	input: number;
	cache_read: number;
	cache_write: number;
	output: number;
};

export function tokensOf(t: Totals | Row): number {
	return t.input + t.cache_read + t.cache_write + t.output;
}

export function sum(rs: Row[]): Totals {
	const t: Totals = { n: rs.length, errors: 0, ms: 0, input: 0, cache_read: 0, cache_write: 0, output: 0 };
	for (const r of rs) {
		t.errors += r.status >= 400 ? 1 : 0;
		t.ms += r.ms;
		t.input += r.input;
		t.cache_read += r.cache_read;
		t.cache_write += r.cache_write;
		t.output += r.output;
	}
	return t;
}

export function errorRate(t: Totals): number {
	return t.n ? (100 * t.errors) / t.n : 0;
}

export function cacheHitRate(t: Totals): number {
	const base = t.input + t.cache_read;
	return base ? (100 * t.cache_read) / base : 0;
}

export function avgMs(t: Totals): number {
	return t.n ? t.ms / t.n : 0;
}

/** Nearest-rank percentile (p in 0..100) over a numeric array. Returns 0 for an empty input. */
export function percentile(values: number[], p: number): number {
	if (!values.length) return 0;
	const sorted = [...values].sort((a, b) => a - b);
	const idx = Math.min(sorted.length - 1, Math.max(0, Math.ceil((p / 100) * sorted.length) - 1));
	return sorted[idx];
}

export type Granularity = 'hour' | 'day';

export function granularityFor(days: number): Granularity {
	return days === 1 ? 'hour' : 'day';
}

function bucketStart(t: number, granularity: Granularity): number {
	const d = new Date(t);
	if (granularity === 'hour') d.setMinutes(0, 0, 0);
	else d.setHours(0, 0, 0, 0);
	return d.getTime();
}

function stepMs(granularity: Granularity): number {
	return granularity === 'hour' ? 3_600_000 : 86_400_000;
}

/** Every bucket start from `from` to `to` inclusive, so gaps in the data still render as zero. */
export function bucketRange(from: number, to: number, granularity: Granularity): number[] {
	const step = stepMs(granularity);
	const start = bucketStart(from, granularity);
	const end = bucketStart(to, granularity);
	const out: number[] = [];
	for (let t = start; t <= end; t += step) out.push(t);
	return out;
}

export function groupByBucket(rows: Row[], granularity: Granularity): Map<number, Row[]> {
	const map = new Map<number, Row[]>();
	for (const r of rows) {
		const b = bucketStart(r.time, granularity);
		const list = map.get(b);
		if (list) list.push(r);
		else map.set(b, [r]);
	}
	return map;
}

/** Thin an array of tick positions down to at most `max` evenly-spaced indices, always keeping the first and last. */
export function thinTicks(count: number, max: number): number[] {
	if (count <= max) return Array.from({ length: count }, (_, i) => i);
	const out: number[] = [];
	const step = (count - 1) / (max - 1);
	for (let i = 0; i < max; i++) out.push(Math.round(i * step));
	return [...new Set(out)];
}

const CATEGORICAL_SLOTS = 8;
const HARNESS_SLOTS = 2; // slot 1 (codex) + slot 2 (claude) are reserved for harness identity

/** Stable slot index (3..8) per account, assigned by sorted name so it never shifts when filters change the visible set. Overflow past the available slots folds into "Other". */
export function accountSlots(accounts: Iterable<string>): Map<string, number | null> {
	const sorted = [...new Set(accounts)].sort();
	const map = new Map<string, number | null>();
	sorted.forEach((a, i) => {
		const slot = HARNESS_SLOTS + 1 + i; // starts at slot 3
		map.set(a, slot <= CATEGORICAL_SLOTS ? slot : null);
	});
	return map;
}

/** Known harnesses, in display order. Each has a colour token in theme.css; anything else
 * (a harness name someone made up in a base URL) is still accepted and drawn in muted ink. */
export const HARNESSES: { key: string; label: string }[] = [
	{ key: 'claude', label: 'Claude' },
	{ key: 'codex', label: 'Codex' },
	{ key: 'openclaw', label: 'OpenClaw' },
	{ key: 'hermes', label: 'Hermes' }
];
const HARNESS_LABEL = new Map(HARNESSES.map((h) => [h.key, h.label]));
const HARNESS_ORDER = new Map(HARNESSES.map((h, i) => [h.key, i]));

export function harnessLabel(h: string): string {
	return HARNESS_LABEL.get(h) ?? (h ? h.charAt(0).toUpperCase() + h.slice(1) : h);
}

export function harnessVar(h: string): string {
	return HARNESS_LABEL.has(h) ? `var(--harness-${h})` : 'var(--text-muted)';
}

/** Known harnesses first in registry order, then anything else alphabetically. */
export function sortHarnesses(hs: Iterable<string>): string[] {
	return [...new Set(hs)].sort((a, b) => (HARNESS_ORDER.get(a) ?? 99) - (HARNESS_ORDER.get(b) ?? 99) || a.localeCompare(b));
}

/** The upstream a row went to. Rows from before the provider segment existed imply it from the harness. */
export function providerOf(r: Row): string {
	return r.provider ?? (r.harness === 'codex' ? 'chatgpt' : 'anthropic');
}

export function seriesFor(buckets: number[], grouped: Map<number, Row[]>, reduce: (rows: Row[]) => number): number[] {
	return buckets.map((b) => reduce(grouped.get(b) ?? []));
}

/** True for rows recorded after connection-timing instrumentation first shipped. Older rows omit
 * this field entirely, which must read as "not measured", never as a measured zero.
 *
 * Deliberately narrows ONLY first_byte_ms, not queue_us/reused/connect_ms/dns_ms/tcp_ms/tls_ms —
 * those were added in a later pass and briefly a real deployment logged rows with first_byte_ms
 * set but none of the others. Treat every one of those fields as independently possibly-undefined
 * everywhere, even on a row that passes this check. */
export function hasLatencyDetail(r: Row): r is Row & { first_byte_ms: number } {
	return r.first_byte_ms !== undefined;
}

/** Mean of the defined values only — undefined entries are excluded from both sum and count,
 * never treated as zero (which would silently understate the average). */
export function meanDefined(values: (number | undefined)[]): number {
	const defined = values.filter((v): v is number => v !== undefined);
	return defined.length ? defined.reduce((s, v) => s + v, 0) / defined.length : 0;
}

/** Total latency the proxy itself is responsible for: its own pre-dispatch processing (queue_us)
 * plus the time spent acquiring an upstream connection (connect_ms — near-zero when reused, the
 * full DNS+TCP+TLS span on a fresh dial). Everything else in `ms` is upstream/model time. */
export function proxyOverheadMs(r: Row): number {
	return (r.queue_us ?? 0) / 1000 + (r.connect_ms ?? 0);
}

export function deltaPct(curr: number, prev: number): number | null {
	if (!isFinite(prev) || prev === 0) return null;
	return (100 * (curr - prev)) / prev;
}

/** Earliest row time, without spreading a large array into Math.min (which throws past ~100k args). */
export function minTime(rows: Row[], fallback: number): number {
	let m = Infinity;
	for (const r of rows) if (r.time < m) m = r.time;
	return m === Infinity ? fallback : m;
}

/** Largest value in a possibly huge array; 0 for an empty one. */
export function maxOf(values: number[]): number {
	let m = 0;
	for (const v of values) if (v > m) m = v;
	return m;
}

/** Everything the model had to read for this request: fresh input plus both cache flavours. */
export function contextOf(r: Row): number {
	return r.input + r.cache_read + r.cache_write;
}

/** Output tokens generated per second of streaming time (first byte to last byte). Null when the
 * row has no first-byte measurement, when the reply is too short to measure meaningfully, or when
 * the streaming span is zero. */
export function genSpeed(r: Row, minOutput = 20): number | null {
	if (r.first_byte_ms === undefined || r.output < minOutput) return null;
	const streamMs = r.ms - r.first_byte_ms;
	if (streamMs <= 0) return null;
	return r.output / (streamMs / 1000);
}

/** Short display name for a model id: strips the vendor prefix and date suffix so legends and
 * chips stay readable ("claude-haiku-4-5-20251001" → "haiku-4-5", "gpt-6-astra" stays as is). */
export function shortModel(model: string): string {
	if (!model) return '(unknown)';
	return model.replace(/^claude-/, '').replace(/-\d{8}$/, '');
}

/** Stable categorical slot (1..8) per model, assigned by sorted id so a model keeps its colour when
 * filters change which models are visible. Past eight, null → fold into "Other". Models get the
 * full slot range because no chart mixes model identity with harness identity. */
export function modelSlots(models: Iterable<string>): Map<string, number | null> {
	const sorted = [...new Set(models)].sort();
	const map = new Map<string, number | null>();
	sorted.forEach((m, i) => map.set(m, i < CATEGORICAL_SLOTS ? i + 1 : null));
	return map;
}

export function modelColor(slots: Map<string, number | null>, model: string): string {
	const slot = slots.get(model);
	return slot ? `var(--series-${slot})` : 'var(--text-muted)';
}

/** Top `limit` keys by value plus everything else summed under "Other". Returned in descending
 * order with Other last; Other is omitted when nothing was folded. */
export function topWithOther<T>(
	groups: [string, T[]][],
	value: (rows: T[]) => number,
	limit: number
): { key: string; rows: T[]; folded: boolean }[] {
	const sorted = [...groups].sort((a, b) => value(b[1]) - value(a[1]));
	const top = sorted.slice(0, limit).map(([key, rows]) => ({ key, rows, folded: false }));
	const rest = sorted.slice(limit).flatMap(([, rows]) => rows);
	if (rest.length) top.push({ key: 'Other', rows: rest, folded: true });
	return top;
}

/** Peak number of requests in flight at the same instant, by sweep-line over [time, time+ms). */
export function peakConcurrency(rows: Row[]): number {
	const events: [number, number][] = [];
	for (const r of rows) {
		events.push([r.time, 1]);
		events.push([r.time + Math.max(1, r.ms), -1]);
	}
	events.sort((a, b) => a[0] - b[0] || a[1] - b[1]);
	let cur = 0;
	let peak = 0;
	for (const [, d] of events) {
		cur += d;
		if (cur > peak) peak = cur;
	}
	return peak;
}
