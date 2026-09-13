// Navigation, page headings, and URL validation share one registry.
export const groups = [
	{ label: 'Analytics', items: [
		{ id: 'overview', label: 'Overview', description: 'A pulse on your coding agents, usage, and reliability.' },
		{ id: 'usage', label: 'Usage', description: 'Understand token volume and how your agents use context.' },
		{ id: 'cost', label: 'Cost', description: 'Explore estimated spend, cache savings, and model pricing.' }
	] },
	{ label: 'Explore', items: [
		{ id: 'models', label: 'Models', description: 'Compare usage across the models powering your work.' },
		{ id: 'accounts', label: 'Accounts', description: 'See how usage is distributed across your accounts.' },
		{ id: 'sessions', label: 'Sessions', description: 'Follow coding sessions from first request to last.' },
		{ id: 'activity', label: 'Activity', description: 'Discover patterns in when and how you work.' }
	] },
	{ label: 'Diagnostics', items: [
		{ id: 'performance', label: 'Performance', description: 'Inspect generation speed, response times, and context sizes.' },
		{ id: 'latency', label: 'Latency', description: 'Separate proxy overhead from upstream response time.' },
		{ id: 'logs', label: 'Logs', description: 'Inspect individual requests and export the details.' }
	] },
	{ label: 'Administration', items: [
		{ id: 'settings', label: 'Settings', description: 'Manage connections, instance configuration, and browser preferences.' }
	] }
] as const;
export const views = groups.flatMap(group => group.items.map(item => ({ ...item, group: group.label })));
export type ViewId = (typeof views)[number]['id'];
export function viewFromParam(value: string | null): ViewId {
	return views.find(view => view.id === value)?.id ?? 'overview';
}
