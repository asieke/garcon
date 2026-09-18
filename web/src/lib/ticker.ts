import type { Row } from './usage';

export type RecentRequest = Row & { sequence: number; remote?: boolean };

/** The first snapshot is a baseline. Only later completions enter the FIFO. */
export class RequestQueue {
	private latest: number | null = null;
	private pending: RecentRequest[] = [];

	update(rows: RecentRequest[]): boolean {
		const newest = rows.at(-1)?.sequence ?? 0;
		const reset = this.latest !== null && newest < this.latest;
		if (this.latest === null || reset) {
			this.latest = newest;
			this.pending = [];
			return reset;
		}
		const latest = this.latest;
		const arrivals = rows.filter(row => row.sequence > latest);
		this.pending = [...this.pending, ...arrivals].slice(-30);
		this.latest = newest;
		return reset;
	}

	next(): RecentRequest | undefined {
		return this.pending.shift();
	}
}

export function requestProvider(row: Row): { label: string; color: string } {
	const provider = row.provider || (row.harness === 'codex' ? 'chatgpt' : row.harness === 'claude' ? 'anthropic' : '');
	if (provider === 'chatgpt') return { label: 'Codex', color: '#3c82eb' };
	if (provider === 'anthropic') return { label: 'Claude', color: '#df805d' };
	const names: Record<string, string> = { openai: 'OpenAI', openrouter: 'OpenRouter', gemini: 'Gemini' };
	return { label: names[provider] || provider || row.harness || 'LLM', color: '#a67add' };
}

export function requestTokens(row: Row): number {
	return (row.input || 0) + (row.cache_read || 0) + (row.cache_write || 0) + (row.output || 0);
}

export function requestMachine(row: RecentRequest): string {
	return row.device || (row.remote ? 'Remote machine' : 'This machine');
}
