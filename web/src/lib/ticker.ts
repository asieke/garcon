import type { Row } from './usage';

export type RecentRequest = Row & { sequence: number; state?: string };

/** The first snapshot is a baseline. Only later token-bearing activity enters the FIFO. */
export class RequestQueue {
	private latest: number | null = null;
	private pending: RecentRequest[] = [];
	private waiting = new Map<number, RecentRequest>();

	update(rows: RecentRequest[]): boolean {
		const newest = rows.at(-1)?.sequence ?? 0;
		const reset = this.latest !== null && newest < this.latest;
		if (this.latest === null || reset) {
			this.latest = newest;
			this.pending = [];
			this.waiting.clear();
			return reset;
		}
		const latest = this.latest;
		const current = new Map(rows.map(row => [row.sequence, row]));
		this.pending = this.pending
			.map(row => current.get(row.sequence) ?? row)
			.filter(row => requestTokens(row) > 0);
		for (const [sequence] of this.waiting) {
			if (!current.has(sequence)) this.waiting.delete(sequence);
		}
		for (const row of rows) {
			if (row.sequence <= latest && !this.waiting.has(row.sequence)) continue;
			if (requestTokens(row) > 0) {
				this.pending.push(row);
				this.waiting.delete(row.sequence);
			} else if (row.state === 'streaming') {
				this.waiting.set(row.sequence, row);
			} else {
				this.waiting.delete(row.sequence);
			}
		}
		this.pending = this.pending.slice(-30);
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
