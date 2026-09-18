export type LimitWindow = {
	id: string;
	label: string;
	used_percent: number | null;
	window_seconds: number;
	resets_at: number;
	expired: boolean;
};
export type LimitAccount = {
	id: string;
	provider: 'codex' | 'claude';
	email: string;
	workspace?: string;
	plan?: string;
	status: 'fresh' | 'stale' | 'needs_login' | 'unavailable';
	error?: string;
	fetched_at: number;
	windows: LimitWindow[];
	reset_credits?: {
		available_count: number | null;
		credits: { expires_at: number }[];
		status: 'fresh' | 'stale' | 'unavailable';
		fetched_at: number;
		error?: string;
	};
};
export type LimitsSnapshot = { accounts: LimitAccount[]; refreshing: boolean; updated_at: number; error?: string };

export function expired(window: LimitWindow, now: number): boolean {
	return window.expired || (window.resets_at > 0 && window.resets_at <= now);
}
export function elapsedPercent(window: LimitWindow, now: number): number | null {
	if (!window.resets_at || window.window_seconds <= 0 || expired(window, now)) return null;
	const duration = window.window_seconds * 1000;
	return Math.min(100, Math.max(0, 100 * (now - (window.resets_at - duration)) / duration));
}
export function barPercent(value: number | null): number {
	return value == null || !Number.isFinite(value) ? 0 : Math.max(0, Math.min(100, value));
}
export function countdown(reset: number, now: number): string {
	if (!reset) return 'Reset time unavailable';
	if (reset <= now) return 'Waiting for the next window';
	const minutes = Math.ceil((reset - now) / 60_000);
	const days = Math.floor(minutes / 1440), hours = Math.floor((minutes % 1440) / 60);
	if (days) return `Resets in ${days}d ${hours}h`;
	if (hours) return `Resets in ${hours}h ${minutes % 60}m`;
	return `Resets in ${minutes}m`;
}
export function accountStatus(account: LimitAccount, now: number): string {
	if (account.status === 'needs_login') return 'Login needs refresh';
	if (account.status === 'stale' || (account.fetched_at > 0 && now - account.fetched_at > 600_000)) return 'Stale snapshot';
	if (account.status === 'unavailable') return 'Limits unavailable';
	return 'Up to date';
}
export function overviewWindow(account: LimitAccount): LimitWindow | undefined {
	return account.windows.find(w => w.label === 'Weekly') ?? account.windows[0];
}

export function resetCreditInfo(account: LimitAccount, now: number) {
	const credits = account.reset_credits;
	const expired = credits?.credits.filter(c => c.expires_at > 0 && c.expires_at <= now).length ?? 0;
	return {
		count: credits?.available_count == null ? null : Math.max(0, credits.available_count - expired),
		expirations: credits?.credits.filter(c => !c.expires_at || c.expires_at > now).map(c => c.expires_at) ?? [],
		stale: credits?.status === 'stale' || expired > 0 || Boolean(credits?.fetched_at && now - credits.fetched_at > 600_000)
	};
}

export function widgetGroups(accounts: LimitAccount[]): { key: string; label: string; accounts: LimitAccount[] }[] {
	const groups = new Map<string, { key: string; label: string; accounts: LimitAccount[] }>();
	for (const account of accounts) {
		const key = account.email.toLowerCase() || account.id;
		const group = groups.get(key) ?? { key, label: account.email || 'Account identity unavailable', accounts: [] };
		group.accounts.push(account);
		groups.set(key, group);
	}
	return [...groups.values()].sort((a, b) => a.label.localeCompare(b.label)).map(g => ({ ...g,
		accounts: g.accounts.sort((a, b) => (a.provider === b.provider ? a.id.localeCompare(b.id) : a.provider === 'codex' ? -1 : 1))
	}));
}

export function widgetWindows(account: LimitAccount): (LimitWindow | null)[] {
	if (!account.windows.length) return [null];
	const rank = (w: LimitWindow) => w.label === '5-hour' ? 0 : w.label === 'Weekly' ? 1 : 2;
	return [...account.windows].sort((a, b) => rank(a) - rank(b) || a.label.localeCompare(b.label));
}

export function widgetWindowLabel(window: LimitWindow | null, provider: string): string {
	if (!window) return 'Unavailable';
	if (window.label === 'Weekly') return provider === 'claude' ? 'Week · all' : 'Week';
	if (window.label.endsWith(' · Weekly')) return `Week · ${window.label.slice(0, -9)}`;
	return window.label;
}

export function widgetReset(window: LimitWindow | null, now: number): { text: string; title: string } {
	if (!window) return { text: '—', title: 'Usage window unavailable' };
	if (!window.resets_at) return { text: '—', title: 'The provider has not reported a reset time for this window.' };
	if (expired(window, now)) return { text: 'Pending', title: 'The previous window ended; waiting for updated provider usage.' };
	return { text: countdown(window.resets_at, now).replace('Resets in ', ''), title: new Date(window.resets_at).toLocaleString() };
}
