import type { Account } from "./model";
import type { LimitAccount } from "../limits";

// Normalize Claude's quota snapshots into the same row contract as Codex.
// Claude still selects its own login, so no pool membership or routing score
// is inferred from an available quota or a discovered credential.
export function claudeRows(accounts: LimitAccount[], now: number): Account[] {
  return accounts.filter(a => a.provider === "claude").map(a => {
    const window = a.windows.find(w => w.label === "Weekly") ?? a.windows[0];
    const fresh = a.status === "fresh" && a.fetched_at > 0 && now - a.fetched_at <= 600_000;
    const usable = fresh && window && !window.expired && (window.resets_at === 0 || window.resets_at > now);
    const remaining = usable && window.used_percent !== null && Number.isFinite(window.used_percent)
      ? Math.max(0, Math.min(100, 100 - window.used_percent)) : null;
    return {
      id: a.id, email: a.email, plan: a.plan ?? "Claude", profile: a.workspace || "Local Claude Code profile",
      status: a.status === "needs_login" ? "Login needs refresh"
        : !fresh ? (a.status === "unavailable" ? "Usage unavailable" : "Waiting for fresh usage")
        : remaining === 0 ? "Usage exhausted" : "Local login",
      enrolled: false, priority: 1, remaining_percent: remaining,
      hours_left: usable && window.resets_at > 0 ? (window.resets_at - now) / 3_600_000 : null,
      score: null, windows: a.windows, active_requests: 0, conversations: 0,
    };
  });
}
