const compactNumber = new Intl.NumberFormat('en', { notation: 'compact', maximumFractionDigits: 1 });
const exactNumber = new Intl.NumberFormat('en');

export function n(v: number): string {
	return compactNumber.format(v);
}

export function exact(v: number): string {
	return exactNumber.format(v);
}

export function pct(v: number, digits = 0): string {
	return `${v.toFixed(digits)}%`;
}

export function ms(v: number): string {
	if (v >= 100_000) return `${Math.round(v / 1000)}s`;
	if (v >= 10_000) return `${(v / 1000).toFixed(1)}s`;
	return `${Math.round(v)}ms`;
}

/** For durations expected to be tiny (microseconds) — shows the true scale instead of
 * rounding everything down to "0ms". */
export function us(v: number): string {
	if (v < 1000) return `${Math.round(v)}µs`;
	return ms(v / 1000);
}

export function when(t: number): string {
	return new Date(t).toLocaleString('en', {
		month: 'short',
		day: 'numeric',
		hour: 'numeric',
		minute: '2-digit'
	});
}

export function dayLabel(t: number): string {
	return new Date(t).toLocaleDateString('en', { month: 'short', day: 'numeric' });
}

export function hourLabel(t: number): string {
	return new Date(t).toLocaleTimeString('en', { hour: 'numeric' });
}

export function fullTimestamp(t: number): string {
	return new Date(t).toLocaleString('en', {
		weekday: 'short',
		month: 'short',
		day: 'numeric',
		hour: 'numeric',
		minute: '2-digit',
		second: '2-digit'
	});
}

/** US dollars. Sub-cent amounts keep enough precision to be meaningful (a single Haiku call
 * is a fraction of a cent); larger amounts read like a bill. */
export function usd(v: number): string {
	if (!isFinite(v)) return '—';
	const abs = Math.abs(v);
	if (abs === 0) return '$0.00';
	if (abs < 0.01) return `$${v.toFixed(4)}`;
	if (abs < 1) return `$${v.toFixed(3)}`;
	if (abs < 1000) return `$${v.toFixed(2)}`;
	return `$${compactNumber.format(v)}`;
}

/** Human duration from milliseconds: "12s", "4m 05s", "1h 12m", "2d 3h". */
export function duration(msTotal: number): string {
	const s = Math.max(0, Math.round(msTotal / 1000));
	if (s < 60) return `${s}s`;
	const m = Math.floor(s / 60);
	if (m < 60) return `${m}m ${String(s % 60).padStart(2, '0')}s`;
	const h = Math.floor(m / 60);
	if (h < 24) return `${h}h ${String(m % 60).padStart(2, '0')}m`;
	const d = Math.floor(h / 24);
	return `${d}d ${h % 24}h`;
}

/** Tokens per second, one decimal below 100. */
export function tps(v: number): string {
	if (!isFinite(v)) return '—';
	return v >= 100 ? `${Math.round(v)} tok/s` : `${v.toFixed(1)} tok/s`;
}

export function weekdayLabel(dayIndex: number): string {
	return ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'][dayIndex] ?? '';
}

export function monthLabel(t: number): string {
	return new Date(t).toLocaleDateString('en', { month: 'short' });
}

export function timeLabel(t: number): string {
	return new Date(t).toLocaleTimeString('en', { hour: 'numeric', minute: '2-digit' });
}
