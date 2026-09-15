// Navigation, page headings, and URL validation share one registry.
export const groups = [
	{ label: 'Analytics', items: [
		{ id: 'overview', label: 'Overview', description: 'Requests, tokens, errors and latency.' },
		{ id: 'usage', label: 'Usage', description: 'Token volume and composition.' },
		{ id: 'cost', label: 'Cost', description: 'Estimated spend at list prices.' }
	] },
	{ label: 'Explore', items: [
		{ id: 'models', label: 'Models', description: 'Usage by model.' },
		{ id: 'accounts', label: 'Accounts', description: 'Usage by account.' },
		// Only meaningful once rows can come from several machines, so it stays out of the nav until sync is on.
		{ id: 'machines', label: 'Machines', description: 'Usage by machine.', syncOnly: true },
		{ id: 'sessions', label: 'Sessions', description: 'Requests grouped into coding sessions.' },
		{ id: 'activity', label: 'Activity', description: 'When the work happens.' }
	] },
	{ label: 'Diagnostics', items: [
		{ id: 'performance', label: 'Performance', description: 'Generation speed and context sizes.' },
		{ id: 'latency', label: 'Latency', description: 'Proxy overhead vs upstream time.' },
		{ id: 'logs', label: 'Logs', description: 'Every request, with CSV export.' }
	] },
	{ label: 'Administration', items: [
		{ id: 'settings', label: 'Settings', description: 'Instance, providers, models, harnesses, sync and prices.' }
	] }
] as const;
export const views = groups.flatMap(group => group.items.map(item => ({ ...item, group: group.label })));
export type ViewId = (typeof views)[number]['id'];
export function requiresSync(id: ViewId): boolean {
	return views.some(view => view.id === id && 'syncOnly' in view && view.syncOnly);
}
/** The navigation groups with sync-only views removed while sync is off. */
export function groupsFor(syncEnabled: boolean) {
	return groups.map(group => ({ ...group, items: group.items.filter(item => syncEnabled || !('syncOnly' in item && item.syncOnly)) }));
}
export function viewFromParam(value: string | null): ViewId {
	return views.find(view => view.id === value)?.id ?? 'overview';
}
