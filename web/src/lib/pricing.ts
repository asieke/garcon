import type { Row, Totals } from './usage';

/** USD per one million tokens. */
export type Price = { input: number; cacheRead: number; cacheWrite: number; output: number };

export type PriceRule = {
	id: string;
	label: string;
	/** Tested against the model id the provider reported. First matching rule wins. */
	match: RegExp;
	price: Price;
	source: string;
	asOf: string;
	note?: string;
};

// Published pay-as-you-go list prices. Subscriptions (Claude Max, ChatGPT Pro) bill differently, so
// the dashboard presents these as "what this usage would cost at API list price", never as a bill.
// Anthropic cache writes: 5-minute cache is 1.25× input, 1-hour cache is 2×. Claude Code uses the
// 5-minute cache by default, so that is the rate here. OpenAI charges nothing extra for cache writes.
export const PRICE_RULES: PriceRule[] = [
	// Anthropic, https://platform.claude.com/docs/en/about-claude/pricing, read 2026-09-13.
	// cacheWrite is the 5-minute rate (1.25× input); cacheRead is 0.1× input, except Fable/Mythos 5.1 at 0.025×.
	a('claude-fable-5-1', 'Claude Fable 5.1 / Mythos 5.1', /^claude-(fable|mythos)-5-1/, 10, 0.25, 50),
	a('claude-fable-5', 'Claude Fable 5 / Mythos 5', /^claude-(fable|mythos)-5/, 10, 1, 50),
	a('claude-opus-5', 'Claude Opus 5 / 4.5 to 4.8', /^claude-opus-(5|4-[5-8])/, 5, 0.5, 25),
	a('claude-opus-4', 'Claude Opus 4 / 4.1', /^claude-opus-4/, 15, 1.5, 75),
	a('claude-sonnet-5', 'Claude Sonnet 5', /^claude-sonnet-5/, 2, 0.2, 10),
	a('claude-sonnet-4', 'Claude Sonnet 4 / 4.5 / 4.6', /^claude-sonnet-4/, 3, 0.3, 15),
	a('claude-sonnet-3', 'Claude Sonnet 3.5 / 3.7', /^claude-3-[57]-sonnet/, 3, 0.3, 15),
	a('claude-haiku-4-5', 'Claude Haiku 4.5', /^claude-haiku-4-5/, 1, 0.1, 5),
	a('claude-haiku-3-5', 'Claude Haiku 3.5', /^claude-3-5-haiku/, 0.8, 0.08, 4),
	// OpenAI, https://developers.openai.com/api/docs/pricing, read 2026-09-13. Standard tier; cached input
	// is 0.1× input and there is no separate cache-write charge.
	o('gpt-6-astra', 'GPT-6 Astra', /^gpt-6-astra/, 10, 1, 50),
	o('gpt-5.6-sol', 'GPT-5.6 Sol', /^gpt-5\.6-sol/, 4, 0.4, 20),
	o('gpt-5.3-codex', 'GPT-5.3 Codex', /^gpt-5\.3-codex/, 1.75, 0.175, 14),
	o('gpt-5.2', 'GPT-5.2', /^gpt-5\.2(-codex)?(-\d{4}-\d{2}-\d{2})?$/, 1.75, 0.175, 14),
	o('gpt-5.1', 'GPT-5.1', /^gpt-5\.1(-codex)?(-max)?(-\d{4}-\d{2}-\d{2})?$/, 1.25, 0.125, 10),
	o('gpt-5', 'GPT-5', /^gpt-5(-codex)?(-\d{4}-\d{2}-\d{2})?$/, 1.25, 0.125, 10)
];

function a(id: string, label: string, match: RegExp, input: number, cacheRead: number, output: number): PriceRule {
	return {
		id,
		label,
		match,
		price: { input, cacheRead, cacheWrite: input * 1.25, output },
		source: 'https://platform.claude.com/docs/en/about-claude/pricing',
		asOf: '2026-09-13'
	};
}

function o(id: string, label: string, match: RegExp, input: number, cacheRead: number, output: number): PriceRule {
	return {
		id,
		label,
		match,
		price: { input, cacheRead, cacheWrite: 0, output },
		source: 'https://developers.openai.com/api/docs/pricing',
		asOf: '2026-09-13'
	};
}

export const PRICE_FIELDS: { key: keyof Price; label: string }[] = [
	{ key: 'input', label: 'Input' },
	{ key: 'cacheRead', label: 'Cache read' },
	{ key: 'cacheWrite', label: 'Cache write' },
	{ key: 'output', label: 'Output' }
];

/** OpenRouter reports ids as "anthropic/claude-sonnet-5"; the rules match the bare id. */
function bareModel(model: string): string {
	return model.replace(/^[a-z0-9_.-]+\//i, '');
}

export function listRuleFor(model: string): PriceRule | null {
	const bare = bareModel(model);
	return PRICE_RULES.find((r) => r.match.test(model) || r.match.test(bare)) ?? null;
}

export type Resolved = { price: Price; source: 'override' | 'list'; rule: PriceRule | null } | { price: null; source: 'none'; rule: null };

/** The price to use for a model: an explicit override first, then the list rule, else nothing. */
export function priceFor(model: string, overrides: Readonly<Record<string, Price>>): Resolved {
	const o = overrides[model];
	if (o) return { price: o, source: 'override', rule: listRuleFor(model) };
	const rule = listRuleFor(model);
	if (rule) return { price: rule.price, source: 'list', rule };
	return { price: null, source: 'none', rule: null };
}

export type Cost = { input: number; cacheRead: number; cacheWrite: number; output: number; total: number };

const M = 1_000_000;

export function costOf(t: Pick<Row | Totals, 'input' | 'cache_read' | 'cache_write' | 'output'>, p: Price): Cost {
	const input = (t.input / M) * p.input;
	const cacheRead = (t.cache_read / M) * p.cacheRead;
	const cacheWrite = (t.cache_write / M) * p.cacheWrite;
	const output = (t.output / M) * p.output;
	return { input, cacheRead, cacheWrite, output, total: input + cacheRead + cacheWrite + output };
}

/** What the cache-read tokens would have cost as fresh input, minus what they did cost. */
export function cacheSavingsOf(t: Pick<Row | Totals, 'cache_read'>, p: Price): number {
	return (t.cache_read / M) * Math.max(0, p.input - p.cacheRead);
}

export function addCost(a: Cost, b: Cost): Cost {
	return {
		input: a.input + b.input,
		cacheRead: a.cacheRead + b.cacheRead,
		cacheWrite: a.cacheWrite + b.cacheWrite,
		output: a.output + b.output,
		total: a.total + b.total
	};
}

export const ZERO_COST: Cost = { input: 0, cacheRead: 0, cacheWrite: 0, output: 0, total: 0 };

const STORAGE_KEY = 'garcon.pricing.v1';

/** Price overrides live in the browser only; the proxy never stores prices. Wrapped in try/catch
 * because storage can be unavailable (private window, blocked site data). */
export function loadOverrides(): Record<string, Price> {
	try {
		const raw = localStorage.getItem(STORAGE_KEY);
		if (!raw) return {};
		const parsed: unknown = JSON.parse(raw);
		if (!parsed || typeof parsed !== 'object') return {};
		const out: Record<string, Price> = {};
		for (const [model, v] of Object.entries(parsed as Record<string, unknown>)) {
			if (isPrice(v)) out[model] = v;
		}
		return out;
	} catch {
		return {};
	}
}

export function saveOverrides(overrides: Record<string, Price>): void {
	try {
		if (Object.keys(overrides).length) localStorage.setItem(STORAGE_KEY, JSON.stringify(overrides));
		else localStorage.removeItem(STORAGE_KEY);
	} catch {
		// Storage unavailable: the override still applies for this page view.
	}
}

function isPrice(v: unknown): v is Price {
	if (!v || typeof v !== 'object') return false;
	const p = v as Record<string, unknown>;
	return PRICE_FIELDS.every(({ key }) => typeof p[key] === 'number' && isFinite(p[key] as number) && (p[key] as number) >= 0);
}
