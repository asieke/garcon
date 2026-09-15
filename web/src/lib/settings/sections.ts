/** The Settings view is several pages behind one sidebar entry; ?section= picks one. */
export const SETTINGS_SECTIONS = [
	{ id: 'instance', label: 'Instance', description: 'Where this proxy listens, what it has recorded, and how long it has been up.' },
	{ id: 'providers', label: 'Providers', description: 'The upstream APIs the proxy can relay to, and how much has gone to each.' },
	{ id: 'models', label: 'Models', description: 'Every model that has answered through this proxy, with the price used to cost it.' },
	{ id: 'harnesses', label: 'Harnesses', description: 'The coding tools that have connected, and how to point another one at the proxy.' },
	{ id: 'sync', label: 'Sync', description: 'Share the usage log across machines through a Supabase project you own.' },
	{ id: 'prices', label: 'Prices', description: 'The list prices behind the cost estimates, fetched from a public catalogue.' }
] as const;
export type SettingsSectionId = (typeof SETTINGS_SECTIONS)[number]['id'];
export function settingsSectionFromParam(value: string | null): SettingsSectionId {
	return SETTINGS_SECTIONS.find((s) => s.id === value)?.id ?? 'instance';
}

/** GET /api/config: how this instance is wired. */
export type Config = {
	listen: string;
	data: string;
	rows: number;
	bytes: number;
	started: number;
	version: string;
	providers: Record<string, string>;
	implicit_harnesses: Record<string, string>;
};
