import { contextOf, genSpeed, providerOf, type Row } from './usage';

const COLUMNS = [
	'time',
	'harness',
	'account',
	'device',
	'provider',
	'model',
	'status',
	'ms',
	'first_byte_ms',
	'queue_us',
	'reused',
	'connect_ms',
	'dns_ms',
	'tcp_ms',
	'tls_ms',
	'input',
	'cache_read',
	'cache_write',
	'output',
	'context',
	'gen_tok_per_s'
] as const;

/** RFC 4180: quote a field when it contains a comma, quote or line break; double embedded quotes. */
function field(v: unknown): string {
	if (v === undefined || v === null) return '';
	const s = String(v);
	return /[",\r\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
}

export function toCsv(rows: Row[]): string {
	const lines = [COLUMNS.join(',')];
	for (const r of rows) {
		const speed = genSpeed(r);
		lines.push(
			[
				new Date(r.time).toISOString(),
				r.harness,
				r.account,
				r.device,
				providerOf(r),
				r.model,
				r.status,
				r.ms,
				r.first_byte_ms,
				r.queue_us,
				r.reused,
				r.connect_ms,
				r.dns_ms,
				r.tcp_ms,
				r.tls_ms,
				r.input,
				r.cache_read,
				r.cache_write,
				r.output,
				contextOf(r),
				speed === null ? '' : speed.toFixed(1)
			]
				.map(field)
				.join(',')
		);
	}
	return lines.join('\r\n') + '\r\n';
}

/** Offers `text` as a file download. The object URL is revoked once the click has been dispatched. */
export function download(filename: string, text: string, type = 'text/csv'): void {
	const url = URL.createObjectURL(new Blob([text], { type }));
	const a = document.createElement('a');
	a.href = url;
	a.download = filename;
	document.body.appendChild(a);
	a.click();
	a.remove();
	setTimeout(() => URL.revokeObjectURL(url), 1000);
}
