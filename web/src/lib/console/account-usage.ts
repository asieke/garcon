import type { LimitAccount, LimitWindow } from '../limits.ts';
import type { Account } from './model.ts';
import { accountStatus, barPercent, expired, widgetReset } from '../limits.ts';

export type UsageAccount = {
  key: string;
  provider: 'codex' | 'claude';
  email: string;
  detail: string;
  plan?: string;
  quota?: LimitAccount;
  routing?: Account;
};

// A routing ID is a Codex workspace/account ID, not a quota ID or an email.
// Never join by email: one person can have multiple provider/workspace limits.
export function quotaForRoute(route: Account, quotas: LimitAccount[]): LimitAccount | undefined {
  const matches = quotas.filter(q => q.provider === 'codex' && q.workspace === route.id);
  return matches.length === 1 ? matches[0] : undefined;
}

export function usageAccounts(quotas: LimitAccount[], routes: Account[]): UsageAccount[] {
  const assigned = new Map<string, Account>();
  for (const route of routes) {
    const quota = quotaForRoute(route, quotas);
    if (quota) assigned.set(quota.id, route);
  }
  const rows: UsageAccount[] = quotas.map(quota => {
    const route = assigned.get(quota.id);
    return {
      key: quota.id, provider: quota.provider, email: quota.email,
      detail: route?.profile || quota.workspace || (quota.provider === 'claude' ? 'Claude Code login' : 'Codex login'),
      plan: quota.plan || route?.plan, quota, routing: route
    };
  });
  for (const route of routes) {
    if (!quotaForRoute(route, quotas)) rows.push({
      key: `routing:${route.id}`, provider: 'codex', email: route.email,
      detail: route.profile || 'Saved login missing', plan: route.plan, routing: route
    });
  }
  return rows.sort((a, b) => (a.provider === b.provider ? 0 : a.provider === 'codex' ? -1 : 1) || a.email.localeCompare(b.email) || a.key.localeCompare(b.key));
}

export function orderedUsageWindows(windows: LimitWindow[]): LimitWindow[] {
  const rank = (w: LimitWindow) => w.label === '5-hour' ? 0 : w.label === 'Weekly' ? 1 : 2;
  return [...windows].sort((a, b) => rank(a) - rank(b) || a.label.localeCompare(b.label));
}

export function usageWindowLabel(window: LimitWindow, provider: string): string {
  if (window.label === 'Weekly') return provider === 'claude' ? 'Weekly · all models' : 'Weekly';
  // Preserve provider-reported model and feature names; do not assume every
  // Claude account has a Fable allowance, or every Codex account has 5 hours.
  if (window.label.endsWith(' · Weekly')) return `Weekly · ${window.label.slice(0, -9)}`;
  return window.label || 'Usage';
}

export function usageWindowDisplay(window: LimitWindow, now: number) {
  const value = window.used_percent;
  const known = value !== null && Number.isFinite(value) && value >= 0;
  const ended = expired(window, now);
  const reset = widgetReset(window, now);
  return {
    percent: known ? `${Number(value.toFixed(1))}%` : '—',
    fill: known ? barPercent(value) : 0,
    known,
    exhausted: known && value >= 100,
    ended,
    reset: ended ? 'Awaiting reset update' : window.resets_at > 0 ? `Resets in ${reset.text}` : 'Reset not reported',
    resetDescription: reset.description
  };
}

export function usageFreshness(quota: LimitAccount | undefined, now: number) {
  if (!quota) return { stale: true, text: 'Usage unavailable', detail: 'No matching usage snapshot for this account.' };
  const status = accountStatus(quota, now);
  const fresh = status === 'Up to date' && quota.fetched_at > 0;
  const age = quota.fetched_at > 0 ? Math.max(0, now - quota.fetched_at) : 0;
  const checked = quota.fetched_at > 0 ? `Checked ${age < 60_000 ? 'just now' : `${Math.floor(age / 60_000)}m ago`}` : 'Not checked yet';
  return {
    stale: !fresh,
    text: fresh ? checked : `${status === 'Up to date' ? 'Update time unavailable' : status} · ${checked.toLowerCase()}`,
    detail: [quota.error, quota.fetched_at > 0 ? `Last successful check: ${new Date(quota.fetched_at).toLocaleString()}` : 'No successful usage check yet.'].filter(Boolean).join(' · ')
  };
}
