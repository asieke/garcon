<script lang="ts">
	import { n, when, fullTimestamp, ms as msFmt, us as usFmt, tps, pct } from '../format';
	import { harnessLabel, harnessVar, shortModel, contextOf, genSpeed, providerOf, type Row } from '../usage';
	import { toCsv, download } from '../csv';

	let { rows }: { rows: Row[] } = $props();

	const LIMIT = 300;
	type SortKey = 'time' | 'account' | 'harness' | 'model' | 'status' | 'ms' | 'connect' | 'input' | 'cache_read' | 'cache_write' | 'output';
	const COLUMNS: { key: SortKey; label: string; num?: boolean }[] = [
		{ key: 'time', label: 'Time' },
		{ key: 'account', label: 'Account' },
		{ key: 'harness', label: 'Harness' },
		{ key: 'model', label: 'Model' },
		{ key: 'status', label: 'Status', num: true },
		{ key: 'ms', label: 'Latency', num: true },
		{ key: 'connect', label: 'Connect', num: true },
		{ key: 'input', label: 'Input', num: true },
		{ key: 'cache_read', label: 'Cache read', num: true },
		{ key: 'cache_write', label: 'Cache write', num: true },
		{ key: 'output', label: 'Output', num: true }
	];

	let status = $state<'all' | 'ok' | 'error'>('all');
	let modelFilter = $state('all');
	let search = $state('');
	let sortKey = $state<SortKey>('time');
	let sortDir = $state<'asc' | 'desc'>('desc');
	let openId = $state<string | null>(null);

	const models = $derived([...new Set(rows.map((r) => r.model))].filter(Boolean).sort());

	function connectDisplay(r: Row): string {
		if (r.connect_ms === undefined) return '—';
		if (r.reused === true) return `${r.connect_ms}ms (reused)`;
		if (r.reused === false) return `${r.connect_ms}ms (new)`;
		return `${r.connect_ms}ms`; // logged before the reused flag existed
	}

	type Keyed = { r: Row; id: string };
	const filtered = $derived.by((): Keyed[] => {
		const q = search.trim().toLowerCase();
		const out: Keyed[] = [];
		rows.forEach((r, i) => {
			if (status === 'error' && r.status < 400) return;
			if (status === 'ok' && r.status >= 400) return;
			if (modelFilter !== 'all' && r.model !== modelFilter) return;
			if (q && !r.model.toLowerCase().includes(q) && !r.account.toLowerCase().includes(q) && !String(r.status).includes(q)) return;
			out.push({ r, id: `${r.time}-${i}` });
		});
		return out;
	});

	function sortValue(r: Row, key: SortKey): number | string {
		switch (key) {
			case 'connect':
				return r.connect_ms ?? -1;
			default:
				return r[key];
		}
	}
	const sorted = $derived.by(() => {
		const dir = sortDir === 'asc' ? 1 : -1;
		return [...filtered].sort((a, b) => {
			const va = sortValue(a.r, sortKey);
			const vb = sortValue(b.r, sortKey);
			if (va === vb) return (a.r.time - b.r.time) * -1;
			return (va < vb ? -1 : 1) * dir;
		});
	});
	const shown = $derived(sorted.slice(0, LIMIT));

	function sortBy(key: SortKey) {
		if (sortKey === key) sortDir = sortDir === 'asc' ? 'desc' : 'asc';
		else {
			sortKey = key;
			sortDir = key === 'time' || key === 'ms' || key === 'input' || key === 'cache_read' || key === 'cache_write' || key === 'output' || key === 'connect' ? 'desc' : 'asc';
		}
	}
	function exportCsv() {
		const date = new Date().toISOString().slice(0, 10);
		download(`garcon-usage-${date}.csv`, toCsv(sorted.map((k) => k.r)));
	}
	function toggle(id: string) {
		openId = openId === id ? null : id;
	}
	function onKey(e: KeyboardEvent, id: string) {
		if (e.key === 'Enter' || e.key === ' ') {
			e.preventDefault();
			toggle(id);
		}
	}
	function cacheShare(r: Row): string {
		const base = r.input + r.cache_read;
		return base ? pct((100 * r.cache_read) / base) : '—';
	}
	function connectionDetail(r: Row): string {
		if (r.connect_ms === undefined) return 'not measured';
		const kind = r.reused === true ? 'reused pooled connection' : r.reused === false ? 'new dial' : 'connection';
		const phases = [r.dns_ms !== undefined ? `DNS ${r.dns_ms}ms` : null, r.tcp_ms !== undefined ? `TCP ${r.tcp_ms}ms` : null, r.tls_ms !== undefined ? `TLS ${r.tls_ms}ms` : null].filter(Boolean);
		return `${kind}, ${r.connect_ms}ms${phases.length ? ` (${phases.join(', ')})` : ''}`;
	}
</script>

<div class="filters">
	<div class="seg">
		<button class:active={status === 'all'} onclick={() => (status = 'all')}>All</button>
		<button class:active={status === 'ok'} onclick={() => (status = 'ok')}>OK</button>
		<button class:active={status === 'error'} onclick={() => (status = 'error')}>Errors</button>
	</div>
	<select bind:value={modelFilter} aria-label="Model">
		<option value="all">All models</option>
		{#each models as m (m)}<option value={m}>{shortModel(m)}</option>{/each}
	</select>
	<input type="search" placeholder="Search model, account or status…" bind:value={search} />
	<button class="export" onclick={exportCsv} disabled={!sorted.length}>Export CSV ({n(sorted.length)})</button>
</div>

<div class="scroll">
	<table>
		<thead>
			<tr>
				{#each COLUMNS as c (c.key)}
					<th class:num={c.num} aria-sort={sortKey === c.key ? (sortDir === 'asc' ? 'ascending' : 'descending') : 'none'}>
						<button class="sort" class:active={sortKey === c.key} onclick={() => sortBy(c.key)}>
							{c.label}{#if sortKey === c.key}<span class="arrow">{sortDir === 'asc' ? '▲' : '▼'}</span>{/if}
						</button>
					</th>
				{/each}
			</tr>
		</thead>
		<tbody>
			{#each shown as { r, id } (id)}
				{@const open = openId === id}
				<tr class="row" class:error={r.status >= 400} class:open tabindex="0" role="button" aria-expanded={open} title={fullTimestamp(r.time)} onclick={() => toggle(id)} onkeydown={(e) => onKey(e, id)}>
					<td>{when(r.time)}</td><td>{r.account}</td>
					<td><i class="dot" style="background: {harnessVar(r.harness)}"></i>{harnessLabel(r.harness)}</td>
					<td>{r.model || '—'}</td>
					<td class="num">{r.status}</td>
					<td class="num">{r.ms}ms</td>
					<td class="num">{connectDisplay(r)}</td>
					<td class="num">{n(r.input)}</td><td class="num">{n(r.cache_read)}</td>
					<td class="num">{n(r.cache_write)}</td><td class="num">{n(r.output)}</td>
				</tr>
				{#if open}
					{@const speed = genSpeed(r)}
					<tr class="detail">
						<td colspan="11">
							<div class="kv">
								<div><small>Timestamp</small><b>{fullTimestamp(r.time)}</b></div>
								<div><small>Model</small><b>{r.model || '—'}</b></div>
								<div><small>Provider</small><b>{providerOf(r)}</b></div>
								<div><small>Status</small><b>{r.status}</b></div>
								<div><small>Context</small><b>{n(contextOf(r))}</b><span class="sub">{n(r.input)} input, {n(r.cache_read)} cache read, {n(r.cache_write)} cache write</span></div>
								<div><small>Output</small><b>{n(r.output)}</b></div>
								<div><small>Cache hit share</small><b>{cacheShare(r)}</b></div>
								<div><small>Total latency</small><b>{msFmt(r.ms)}</b></div>
								<div><small>Time to first byte</small><b>{r.first_byte_ms === undefined ? 'not measured' : msFmt(r.first_byte_ms)}</b></div>
								<div><small>Streaming time</small><b>{r.first_byte_ms === undefined ? '—' : msFmt(Math.max(0, r.ms - r.first_byte_ms))}</b></div>
								<div><small>Generation speed</small><b>{speed === null ? '—' : tps(speed)}</b></div>
								<div><small>Proxy dispatch</small><b>{r.queue_us === undefined ? '—' : usFmt(r.queue_us)}</b></div>
								<div><small>Connection</small><b>{connectionDetail(r)}</b></div>
							</div>
						</td>
					</tr>
				{/if}
			{:else}
				<tr><td colspan="11">No requests match these filters.</td></tr>
			{/each}
		</tbody>
	</table>
</div>
<p class="note">
	{#if sorted.length > LIMIT}
		Showing 1 to {LIMIT} of {n(sorted.length)} matching requests. Narrow the time range or filters to see the rest, or export everything as CSV.
	{:else if sorted.length}
		Showing {n(sorted.length)} of {n(rows.length)} requests in this window. Click a row for detail.
	{/if}
</p>

<style>
	.filters {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 10px;
		margin-bottom: 14px;
	}
	.seg {
		display: flex;
		gap: 4px;
	}
	.filters button {
		font: inherit;
		padding: 4px 12px;
		border-radius: 6px;
		cursor: pointer;
		border: 1px solid var(--border);
		background: none;
		color: inherit;
	}
	.filters button.active {
		background: var(--text-primary);
		color: var(--page);
	}
	.filters button:disabled {
		opacity: 0.5;
		cursor: default;
	}
	.export {
		margin-left: auto;
	}
	input[type='search'],
	select {
		font: inherit;
		padding: 4px 10px;
		border-radius: 6px;
		border: 1px solid var(--border);
		background: var(--surface);
		color: inherit;
		min-width: 160px;
	}
	.scroll {
		overflow-x: auto;
	}
	table {
		border-collapse: collapse;
		width: 100%;
		white-space: nowrap;
	}
	th,
	td {
		text-align: left;
		padding: 6px 10px;
		border-bottom: 1px solid var(--border);
		font-variant-numeric: tabular-nums;
	}
	th {
		font-weight: 500;
		color: var(--text-secondary);
		font-variant-numeric: normal;
		padding: 0;
	}
	.sort {
		font: inherit;
		color: inherit;
		background: none;
		border: none;
		padding: 6px 10px;
		cursor: pointer;
		width: 100%;
		text-align: inherit;
	}
	.sort.active {
		color: var(--text-primary);
	}
	.arrow {
		font-size: 9px;
		margin-left: 4px;
	}
	.num {
		text-align: right;
	}
	.num .sort {
		text-align: right;
	}
	.row {
		cursor: pointer;
	}
	.row:hover td,
	.row:focus-visible td {
		background: color-mix(in srgb, var(--text-primary) 4%, transparent);
	}
	.row:focus-visible {
		outline: 2px solid var(--series-1);
		outline-offset: -2px;
	}
	.row.open td {
		border-bottom-color: transparent;
	}
	.error td {
		color: var(--status-critical);
	}
	.dot {
		display: inline-block;
		width: 8px;
		height: 8px;
		border-radius: 2px;
		margin-right: 6px;
		vertical-align: middle;
	}
	.detail td {
		background: color-mix(in srgb, var(--text-primary) 3%, transparent);
		white-space: normal;
	}
	.kv {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(170px, 1fr));
		gap: 10px 16px;
		padding: 4px 0;
	}
	.kv small {
		display: block;
		color: var(--text-secondary);
		font-size: 10px;
	}
	.kv b {
		font-size: 13px;
		font-weight: 600;
	}
	.kv .sub {
		display: block;
		color: var(--text-muted);
		font-size: 11px;
	}
	.note {
		color: var(--text-muted);
		font-size: 11px;
		margin: 8px 0 0;
		min-height: 1em;
	}
</style>
