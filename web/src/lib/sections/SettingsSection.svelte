<script lang="ts">
	import '../settings/settings.css';
	import { SETTINGS_SECTIONS, type SettingsSectionId, type Config } from '../settings/sections';
	import InstancePanel from '../settings/InstancePanel.svelte';
	import ProvidersPanel from '../settings/ProvidersPanel.svelte';
	import ModelsPanel from '../settings/ModelsPanel.svelte';
	import HarnessesPanel from '../settings/HarnessesPanel.svelte';
	import SyncPanel from '../settings/SyncPanel.svelte';
	import PricesPanel from '../settings/PricesPanel.svelte';
	import type { Row } from '../usage';

	let { rows, pollMs, now, section }: { rows: Row[]; pollMs: number; now: number; section: SettingsSectionId } = $props();

	let config = $state<Config | null>(null);
	let configError = $state(false);
	$effect(() => {
		fetch('/api/config')
			.then((r) => (r.ok ? r.json() : Promise.reject(r.status)))
			.then((c: Config) => (config = c))
			.catch(() => (configError = true));
	});
	const current = $derived(SETTINGS_SECTIONS.find((s) => s.id === section)!);
</script>

<div class="settings">
	<nav class="subnav" aria-label="Settings sections">
		{#each SETTINGS_SECTIONS as s (s.id)}
			<a href={`?view=settings&section=${s.id}`} aria-current={s.id === section ? 'page' : undefined}>{s.label}</a>
		{/each}
	</nav>
	<div class="panel">
		<header>
			<h2 class="title">{current.label}</h2>
			<p class="lede">{current.description}</p>
		</header>
		{#if section === 'instance'}
			<InstancePanel {config} {configError} {pollMs} {now} />
		{:else if section === 'providers'}
			<ProvidersPanel {rows} {config} />
		{:else if section === 'models'}
			<ModelsPanel {rows} />
		{:else if section === 'harnesses'}
			<HarnessesPanel {rows} {config} />
		{:else if section === 'sync'}
			<SyncPanel {pollMs} />
		{:else if section === 'prices'}
			<PricesPanel {rows} />
		{/if}
	</div>
</div>

<style>
	/* Horizontal tabs: the main sidebar already owns the left edge, a second column there reads as clutter. */
	.subnav {
		display: flex;
		gap: 4px;
		border-bottom: 1px solid var(--border);
		margin-bottom: 24px;
		overflow-x: auto;
		scrollbar-width: none;
	}
	.subnav a {
		flex-shrink: 0;
		padding: 10px 14px;
		margin-bottom: -1px;
		color: var(--text-secondary);
		text-decoration: none;
		font-size: 13px;
		border-bottom: 2px solid transparent;
		border-radius: 6px 6px 0 0;
	}
	.subnav a:hover {
		color: var(--text-primary);
		background: var(--nav-hover);
	}
	.subnav a[aria-current] {
		color: var(--accent);
		border-bottom-color: var(--accent);
		font-weight: 600;
	}
	.panel {
		min-width: 0;
	}
	header {
		margin-bottom: 20px;
	}
	.title {
		font-size: 20px;
		letter-spacing: -0.3px;
		margin: 0;
		opacity: 1;
	}
	.lede {
		color: var(--text-secondary);
		font-size: 13px;
		margin: 6px 0 0;
		max-width: 80ch;
	}
</style>
