import { priceFor, type Catalog } from "$lib/pricing";
export type QuotaWindow = {
  id: string;
  label: string;
  used_percent: number | null;
  resets_at: number;
  expired: boolean;
};
export type Account = {
  id: string;
  email: string;
  plan: string;
  enrolled: boolean;
  status: string;
  remaining_percent: number | null;
  score: number | null;
  hours_left: number | null;
  priority: number;
  profile: string;
  windows: QuotaWindow[];
  active_requests: number;
  conversations: number;
};
export type Routing = { pinned_account?: string; next_account?: string; enabled: boolean; accounts: Account[]; error?: string };
export type RequestRow = {
  harness?: string;
  provider?: string;
  sequence: number;
  request_id?: string;
  time: number;
  account: string;
  account_id?: string;
  session_id?: string;
  model: string;
  status: number;
  state?: string;
  error?: string;
  method?: string;
  path?: string;
  kind?: string;
  ms: number;
  input: number;
  cache_read: number;
  cache_write: number;
  output: number;
  first_byte_ms?: number;
};
export type Session = {
  harness: string;
  provider?: string;
	task?: { title: string; cwd: string; archived: boolean };
  key: string;
  session_id: string;
  account_id: string;
  account: string;
  model: string;
  created_at: number;
  last_seen: number;
  requests: number;
  active: number;
  active_reviews: number;
  active_since: number;
  last_state: string;
  last_status: number;
  last_error: string;
};
export type Aggregate = {
  time: number;
  model: string;
  account: string;
  requests: number;
  errors: number;
  ms: number;
  input: number;
  cache_read: number;
  cache_write: number;
  output: number;
};
export type LogPage = {
  requests: RequestRow[];
  total: number;
  offset: number;
  limit: number;
};
export const count = (n: number) =>
  new Intl.NumberFormat("en", {
    notation: n >= 10000 ? "compact" : "standard",
    maximumFractionDigits: 1,
  }).format(n);
export const usd = (n: number) =>
  new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    maximumFractionDigits: n < 1 ? 4 : 2,
  }).format(n);
export const tokens = (r: {
  input: number;
  cache_read: number;
  cache_write: number;
  output: number;
}) => r.input + r.cache_read + r.cache_write + r.output;
export function cost(
  r: Aggregate | RequestRow,
  c: Catalog | null,
): number | null {
  const p = priceFor(r.model, c).price;
  return p
    ? (r.input * p.input +
        r.cache_read * p.cacheRead +
        r.cache_write * p.cacheWrite +
        r.output * p.output) /
        1e6
    : null;
}
export function ago(t: number, now: number) {
  if (!t) return "Before migration";
  const s = Math.max(0, (now - t) / 1000);
  return s < 60
    ? "Just now"
    : s < 3600
      ? `${Math.floor(s / 60)}m ago`
      : s < 86400
        ? `${Math.floor(s / 3600)}h ago`
        : `${Math.floor(s / 86400)}d ago`;
}
export function duration(h: number | null) {
  if (h === null) return "Unknown reset";
  return h < 1
    ? `${Math.max(1, Math.round(h * 60))}m`
    : h < 48
      ? `${h.toFixed(1)}h`
      : `${Math.floor(h / 24)}d ${Math.floor(h % 24)}h`;
}
export function initials(s: string) {
  return (s.split("@")[0] || "?").slice(0, 2).toUpperCase();
}
export function tone(s: string) {
  let n = 0;
  for (const c of s) n = (n * 31 + c.charCodeAt(0)) >>> 0;
  return n % 4;
}
export function mergeGroups(rows: Aggregate[], key: "model" | "account") {
  const groups = new Map<string, Aggregate>();
  for (const r of rows) {
    const k = r[key] || "Unknown";
    const v = groups.get(k) ?? {
      time: 0,
      model: key === "model" ? k : "",
      account: key === "account" ? k : "",
      requests: 0,
      errors: 0,
      ms: 0,
      input: 0,
      cache_read: 0,
      cache_write: 0,
      output: 0,
    };
    for (const f of [
      "requests",
      "errors",
      "ms",
      "input",
      "cache_read",
      "cache_write",
      "output",
    ] as const)
      v[f] += r[f];
    groups.set(k, v);
  }
  return [...groups.values()].sort((a, b) => tokens(b) - tokens(a));
}
