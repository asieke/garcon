<script lang="ts">
	import StatTile from '../charts/StatTile.svelte';
	import { n, when, duration } from '../format';
	import type { Config } from './sections';

	let { config, configError, pollMs, now }: { config: Config | null; configError: boolean; pollMs: number; now: number } = $props();
	const base = $derived(config ? `http://${config.listen}` : 'http://127.0.0.1:4141');
	const versionLabel = $derived(config ? (/^\d/.test(config.version) ? `v${config.version}` : config.version) : '');
</script>

{#if configError}
	<p class="empty">Could not read /api/config; rebuild with <code>scripts/install.sh</code>.</p>
{:else if !config}
	<p class="caption">Loading…</p>
{:else}
	<section class="tiles">
		<StatTile label="Requests on record" value={n(config.rows)} accent="var(--series-1)" />
		<StatTile label="Usage log size" value={config.bytes >= 1_048_576 ? `${(config.bytes / 1_048_576).toFixed(1)} MB` : `${Math.round(config.bytes / 1024)} KB`} accent="var(--series-3)" />
		<StatTile label="Running for" value={duration(now - config.started)} accent="var(--series-3)" />
		<StatTile label="Dashboard refresh" value={`${Math.round(pollMs / 1000)}s`} accent="var(--series-1)" />
	</section>
	<div class="card">
		<div class="kv">
			<div><small>Version</small><span>{versionLabel}</span></div>
			<div><small>Listening on</small><code>{base}</code></div>
			<div><small>Started</small><span>{when(config.started)}</span></div>
			<div><small>Usage log</small><code>{config.data}</code></div>
			<div><small>Raw data</small><code>{base}/api/usage</code></div>
		</div>
	</div>
	<p class="caption">
		The proxy listens on loopback only and answers requests addressed to this machine; there is no login. Stuck? Run <code>garcon doctor</code>.
	</p>
{/if}
