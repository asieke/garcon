/** Settings → Sync: types for /api/settings. Setup instructions live in the documentation site
 * (docs/ in the repository), not in the dashboard. */
export const DOCS_URL = 'https://asieke.github.io/garcon/sync.html';

export type SyncSettings = { sync_enabled: boolean; device_name: string; url: string; key_set: boolean };

export type SyncStatus = {
	device_id: string;
	pushed: number;
	pending: number;
	last_push_ok: number;
	last_push_error: string;
	last_push_error_at: number;
	last_pull_ok: number;
	last_pull_error: string;
	last_pull_error_at: number;
	remote_rows: number;
	devices: { device_id: string; device: string; rows: number; last_time: number }[];
};

export type SettingsResponse = { settings: SyncSettings; status: SyncStatus };

/** Client-side hint for an obviously wrong key. The proxy re-checks on save (including the role
 * inside a legacy JWT), so this only exists to explain the mistake before the round trip. */
export function keyProblem(key: string): string | null {
	const k = key.trim();
	if (!k || k.startsWith('sb_secret_') || k.startsWith('eyJ')) return null;
	if (k.startsWith('sb_publishable_'))
		return 'That is a publishable key. Sync needs the secret key (sb_secret_…), the only key that can read or write the table.';
	return 'Secret keys start with sb_secret_ (legacy service_role keys start with eyJ).';
}
