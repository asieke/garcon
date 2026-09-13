<script lang="ts">
	import { afterNavigate } from '$app/navigation';
	import { page } from '$app/state';
	import { browser } from '$app/environment';
	import Sidebar from '$lib/navigation/Sidebar.svelte';
	import { views, viewFromParam } from '$lib/navigation/views';
	import OverviewSection from '$lib/sections/OverviewSection.svelte';
	import UsageSection from '$lib/sections/UsageSection.svelte';
	import ModelsSection from '$lib/sections/ModelsSection.svelte';
	import AccountsSection from '$lib/sections/AccountsSection.svelte';
	import LatencySection from '$lib/sections/LatencySection.svelte';
	import LogsSection from '$lib/sections/LogsSection.svelte';
	import CostSection from '$lib/sections/CostSection.svelte';
	import SessionsSection from '$lib/sections/SessionsSection.svelte';
	import ActivitySection from '$lib/sections/ActivitySection.svelte';
	import PerformanceSection from '$lib/sections/PerformanceSection.svelte';
	import SettingsSection from '$lib/sections/SettingsSection.svelte';
	import { when } from '$lib/format';
	import {
		type Row,
		sum,
		tokensOf,
		percentile,
		granularityFor,
		bucketRange,
		groupByBucket,
		seriesFor,
		accountSlots,
		hasLatencyDetail,
		proxyOverheadMs,
		meanDefined,
		minTime,
		sortHarnesses,
		harnessLabel,
		harnessVar
	} from '$lib/usage';

	const WINDOWS = [
		['Today', 1],
		['7 days', 7],
		['30 days', 30],
		['All', 0]
	] as const;
	const POLL_MS = 10_000;

	let rows = $state<Row[]>([]);
	let days = $state(7);
	const tab = $derived(viewFromParam(browser ? page.url.searchParams.get('view') : null));
	let menuOpen = $state(false);
	let heading: HTMLHeadingElement;
	const currentView = $derived(views.find(view => view.id === tab)!);
	function navigate() { menuOpen = false; }
	afterNavigate(() => {
		menuOpen = false;
		heading?.focus();
	});
	let harnessFilter = $state('all');
	let accountFilter = $state('all');
	let now = $state(Date.now());
	let lastSync = $state<number | null>(null);
	let stale = $state(false);

	async function refresh() {
		try {
			const res = await fetch('/api/usage');
			if (!res.ok) throw new Error(String(res.status));
			rows = await res.json();
			now = Date.now();
			lastSync = now;
			stale = false;
		} catch {
			stale = true;
		}
	}
	$effect(() => {
		refresh();
		const timer = setInterval(refresh, POLL_MS);
		return () => clearInterval(timer);
	});

	const accountsAll = $derived([...new Set(rows.map((r) => r.account))].sort());
	const harnessesAll = $derived(sortHarnesses(rows.map((r) => r.harness)));
	const slots = $derived(accountSlots(accountsAll));
	function colorForAccount(a: string): string {
		const slot = slots.get(a);
		return slot ? `var(--series-${slot})` : 'var(--text-muted)';
	}

	const dimFiltered = $derived(
		rows.filter((r) => {
			if (harnessFilter !== 'all' && r.harness !== harnessFilter) return false;
			if (accountFilter !== 'all' && r.account !== accountFilter) return false;
			return true;
		})
	);

	const since = $derived(days === 1 ? new Date().setHours(0, 0, 0, 0) : days ? now - days * 86_400_000 : 0);
	const visible = $derived(dimFiltered.filter((r) => r.time >= since));

	// "All" has no well-defined prior period to compare against, so deltas are hidden there.
	const previous = $derived.by(() => {
		if (!days) return null;
		const span = now - since;
		const prevSince = since - span;
		return dimFiltered.filter((r) => r.time >= prevSince && r.time < since);
	});

	const totals = $derived(sum(visible));
	const prevTotals = $derived(previous ? sum(previous) : null);

	const granularity = $derived(granularityFor(days));
	const chartFrom = $derived(since || minTime(rows, now));
	const buckets = $derived(bucketRange(chartFrom, now, granularity));
	const grouped = $derived(groupByBucket(visible, granularity));

	// One series per harness present in the window, in registry order so colours and stacking stay put.
	const harnessesVisible = $derived(sortHarnesses(visible.map((r) => r.harness)));
	const harnessSeries = $derived(
		harnessesVisible.map((h) => ({
			key: h,
			label: harnessLabel(h),
			color: harnessVar(h),
			values: seriesFor(buckets, grouped, (rs) => rs.filter((r) => r.harness === h).length)
		}))
	);
	const harnessTokenSeries = $derived(
		harnessesVisible.map((h) => ({
			key: h,
			label: harnessLabel(h),
			color: harnessVar(h),
			values: seriesFor(buckets, grouped, (rs) => rs.filter((r) => r.harness === h).reduce((s, r) => s + tokensOf(r), 0))
		}))
	);
	const tokensTrend = $derived(seriesFor(buckets, grouped, (rs) => rs.reduce((s, r) => s + tokensOf(r), 0)));
	const latencyTrend = $derived(
		seriesFor(buckets, grouped, (rs) => (rs.length ? rs.reduce((s, r) => s + r.ms, 0) / rs.length : 0))
	);
	const errorCounts = $derived(seriesFor(buckets, grouped, (rs) => rs.filter((r) => r.status >= 400).length));

	const modelGroups = $derived(
		[...Map.groupBy(visible, (r) => r.model || '(unknown)')]
			.map(([model, rs]) => ({ model, totals: sum(rs) }))
			.sort((a, b) => tokensOf(b.totals) - tokensOf(a.totals))
	);

	const accountGroups = $derived(
		[...Map.groupBy(visible, (r) => r.account)].map(([account, rs]) => ({
			account,
			harnesses: [...new Set(rs.map((r) => r.harness))].sort(),
			totals: sum(rs),
			trend: seriesFor(buckets, groupByBucket(rs, granularity), (b) => b.reduce((s, r) => s + tokensOf(r), 0)),
			color: colorForAccount(account)
		}))
	);

	// Latency: percentiles only mean something for buckets that actually had traffic, so empty
	// buckets are dropped from these two series rather than rendered as a misleading zero.
	const nonEmptyIdx = $derived(buckets.map((_, i) => i).filter((i) => (grouped.get(buckets[i]) ?? []).length > 0));
	const latencyBuckets = $derived(nonEmptyIdx.map((i) => buckets[i]));
	const p50Series = $derived(nonEmptyIdx.map((i) => percentile((grouped.get(buckets[i]) ?? []).map((r) => r.ms), 50)));
	const p95Series = $derived(nonEmptyIdx.map((i) => percentile((grouped.get(buckets[i]) ?? []).map((r) => r.ms), 95)));

	const latencyRows = $derived(visible.filter(hasLatencyDetail));
	// queue_us/reused/connect_ms/dns_ms/tcp_ms/tls_ms were added in a later pass than
	// first_byte_ms, so a row can have first_byte_ms without any of them (logged in the gap
	// between the two deploys). Every use below treats each field as independently optional —
	// via meanDefined, an explicit `!== undefined` filter, or a strict `=== true`/`=== false`
	// check — rather than assuming hasLatencyDetail guarantees all of them together.
	const withReusedKnown = $derived(latencyRows.filter((r) => r.reused !== undefined));

	// Proxy overhead over time: dispatch (queue_us, converted to ms) stacked under connect
	// (connect_ms), averaged per bucket so the stack represents "typical per-request cost" each period.
	const overheadGrouped = $derived(groupByBucket(latencyRows, granularity));
	const nonEmptyOverheadIdx = $derived(buckets.map((_, i) => i).filter((i) => (overheadGrouped.get(buckets[i]) ?? []).length > 0));
	const overheadBuckets = $derived(nonEmptyOverheadIdx.map((i) => buckets[i]));
	const dispatchSeries = $derived(
		nonEmptyOverheadIdx.map((i) => meanDefined((overheadGrouped.get(buckets[i]) ?? []).map((r) => r.queue_us)) / 1000)
	);
	const connectSeries = $derived(
		nonEmptyOverheadIdx.map((i) => meanDefined((overheadGrouped.get(buckets[i]) ?? []).map((r) => r.connect_ms)))
	);

	const p50Ms = $derived(percentile(visible.map((r) => r.ms), 50));
	const p95Ms = $derived(percentile(visible.map((r) => r.ms), 95));
	const overheadP50 = $derived(percentile(latencyRows.map(proxyOverheadMs), 50));
	const overheadP95 = $derived(percentile(latencyRows.map(proxyOverheadMs), 95));
	const dispatchP50Us = $derived(
		percentile(
			latencyRows.map((r) => r.queue_us).filter((v): v is number => v !== undefined),
			50
		)
	);
	const reusedPct = $derived(
		withReusedKnown.length ? (100 * withReusedKnown.filter((r) => r.reused === true).length) / withReusedKnown.length : 0
	);
	const avgFirstByte = $derived(
		latencyRows.length ? latencyRows.reduce((s, r) => s + r.first_byte_ms, 0) / latencyRows.length : 0
	);

	// Strict === false (not just falsy) so a row from before `reused` existed — where the field
	// is undefined, not false — is excluded rather than miscounted as a fresh dial.
	const freshDials = $derived(withReusedKnown.filter((r) => r.reused === false));
	const freshDialN = $derived(freshDials.length);
	const connectionBreakdown = $derived(
		freshDialN
			? {
					dns: percentile(freshDials.map((r) => r.dns_ms ?? 0), 50),
					tcp: percentile(freshDials.map((r) => r.tcp_ms ?? 0), 50),
					tls: percentile(freshDials.map((r) => r.tls_ms ?? 0), 50)
				}
			: null
	);

	const byHarness = $derived(
		[...new Set(visible.map((r) => r.harness))].sort().map((harness) => {
			const rs = visible.filter((r) => r.harness === harness);
			const lat = rs.filter(hasLatencyDetail);
			const reusedKnown = lat.filter((r) => r.reused !== undefined);
			return {
				harness,
				count: rs.length,
				p50: percentile(rs.map((r) => r.ms), 50),
				p95: percentile(rs.map((r) => r.ms), 95),
				p99: percentile(rs.map((r) => r.ms), 99),
				measuredN: lat.length,
				reusedPct: reusedKnown.length ? (100 * reusedKnown.filter((r) => r.reused === true).length) / reusedKnown.length : 0,
				avgDispatchUs: meanDefined(lat.map((r) => r.queue_us)),
				avgConnect: meanDefined(lat.map((r) => r.connect_ms)),
				avgFirstByte: lat.length ? lat.reduce((s, r) => s + r.first_byte_ms, 0) / lat.length : 0
			};
		})
	);

	const loading = $derived(lastSync === null && !stale);
</script>

<svelte:head><title>{currentView.label} · Garcon</title></svelte:head>
<svelte:window onkeydown={(event) => { if (event.key === 'Escape' && menuOpen) { menuOpen = false; document.getElementById('menu-toggle')?.focus(); } }} />

<a class="skip-link" href="#main-content" onclick={(event) => { event.preventDefault(); document.getElementById('main-content')?.focus(); }}>Skip to content</a>
<div class="app-shell">
	<aside class:open={menuOpen}>
		<a class="brand" href="?view=overview" onclick={navigate}><span class="brand-mark" aria-hidden="true">g.</span><span>Garcon<small>AGENT OBSERVABILITY</small></span></a>
		<div class="navigation" id="primary-navigation"><Sidebar active={tab} onnavigate={navigate} /></div>
		<div class="sidebar-footer"><span class="status-dot" class:offline={stale || !lastSync}></span>Local instance</div>
	</aside>
	<div class="workspace">
		<header class="topbar">
			<button id="menu-toggle" class="menu-toggle" aria-expanded={menuOpen} aria-controls="primary-navigation" onclick={() => { menuOpen = !menuOpen; if (menuOpen) window.scrollTo({ top: 0, behavior: 'instant' }); }}>{menuOpen ? 'Close menu' : 'Menu'}</button>
			<div class="breadcrumb">{currentView.group}<span aria-hidden="true">/</span><strong>{currentView.label}</strong></div>
			<span class="sync" class:stale title={lastSync ? new Date(lastSync).toLocaleString() : undefined}>
				<span class="status-dot" class:offline={stale || !lastSync}></span>
				{#if stale}Connection lost{:else if lastSync}Synced {when(lastSync)}{:else}Connecting…{/if}
			</span>
		</header>
		<main id="main-content" tabindex="-1">
			<div class="page-heading"><div><p class="eyebrow">{tab === 'settings' ? 'Workspace administration' : 'Usage intelligence'}</p><h1 bind:this={heading} tabindex="-1">{currentView.label}</h1><p class="description">{currentView.description}</p></div></div>
			{#if tab !== 'settings'}
				<div class="filters" aria-label="Usage filters">
					<div class="period"><span class="filter-label" id="period-label">Time range</span><div class="seg" role="group" aria-labelledby="period-label">
						{#each WINDOWS as [label, d]}<button class:active={days === d} aria-pressed={days === d} onclick={() => days = d}>{label}</button>{/each}
					</div></div>
					<label><span id="harness-label">Harness</span><select aria-labelledby="harness-label" bind:value={harnessFilter}><option value="all">All harnesses</option>{#each harnessesAll as h}<option value={h}>{harnessLabel(h)}</option>{/each}</select></label>
					<label><span id="account-label">Account</span><select aria-labelledby="account-label" bind:value={accountFilter}><option value="all">All accounts</option>{#each accountsAll as a}<option value={a}>{a}</option>{/each}</select></label>
					{#if days !== 7 || harnessFilter !== 'all' || accountFilter !== 'all'}<button class="reset" onclick={() => { days = 7; harnessFilter = 'all'; accountFilter = 'all'; }}>Reset filters</button>{/if}
				</div>
			{/if}
			{#if stale}<p class="notice" role="status">Unable to refresh usage. {lastSync ? 'Showing the last available data.' : 'Usage data is unavailable.'} Retrying every 10 seconds.</p>{/if}
			<div class="view-content">
{#if tab === 'settings'}
	<SettingsSection {rows} pollMs={POLL_MS} {now} />
{:else if loading}
	<p class="loading" role="status">Loading usage data…</p>
{:else if !lastSync}
	<p class="empty-state">Waiting for the local instance to reconnect.</p>
{:else if !visible.length}
	<div class="empty-state"><h2>{rows.length ? 'No requests match these filters' : 'Your usage story starts here'}</h2><p>{rows.length ? 'Try a wider time range or choose another harness or account.' : 'Connect a coding agent to Garcon to start exploring requests, tokens, and performance.'}</p>{#if rows.length}<button onclick={() => { days = 0; harnessFilter = 'all'; accountFilter = 'all'; }}>Show all usage</button>{:else}<a href="?view=settings">Set up a connection →</a>{/if}</div>
{:else}
	{#if tab === 'overview'}
		<OverviewSection {totals} {prevTotals} {buckets} {granularity} {harnessSeries} {errorCounts} {tokensTrend} {latencyTrend} />
	{:else if tab === 'usage'}
		<UsageSection {buckets} {granularity} {harnessTokenSeries} {totals} />
	{:else if tab === 'models'}
		<ModelsSection {modelGroups} />
	{:else if tab === 'accounts'}
		<AccountsSection {accountGroups} />
	{:else if tab === 'latency'}
		<LatencySection
			{granularity}
			{latencyBuckets}
			{p50Series}
			{p95Series}
			{overheadBuckets}
			{dispatchSeries}
			{connectSeries}
			measuredN={latencyRows.length}
			totalN={visible.length}
			{p50Ms}
			{p95Ms}
			{overheadP50}
			{overheadP95}
			{dispatchP50Us}
			{reusedPct}
			{avgFirstByte}
			{connectionBreakdown}
			{freshDialN}
			{byHarness}
		/>
	{:else if tab === 'cost'}
		<CostSection rows={visible} prevRows={previous} {buckets} {granularity} {days} {now} {since} />
	{:else if tab === 'sessions'}
		<SessionsSection rows={visible} {days} {now} {since} {granularity} />
	{:else if tab === 'activity'}
		<ActivitySection rows={visible} {days} {now} {since} />
	{:else if tab === 'performance'}
		<PerformanceSection rows={visible} />
	{:else if tab === 'logs'}
		<LogsSection rows={visible} />
	{/if}
{/if}

			</div>
		</main>
	</div>
</div>

<style>
	.app-shell { display: grid; grid-template-columns: 224px minmax(0, 1fr); min-height: 100dvh; }
	aside { position: sticky; top: 0; height: 100dvh; display: flex; flex-direction: column; background: var(--sidebar); border-right: 1px solid var(--border); }
	.brand { display: flex; align-items: center; gap: 11px; padding: 26px 24px; color: var(--text-primary); text-decoration: none; font-size: 20px; font-weight: 650; letter-spacing: -.5px; }
	.brand-mark { display: grid; place-items: center; width: 34px; height: 38px; background: var(--accent); color: var(--surface); border-radius: 10px; font-size: 26px; }
	.brand small { display: block; font-size: 8px; color: var(--text-muted); letter-spacing: .12em; margin-top: 2px; }
	.navigation { padding: 12px; flex: 1; overflow-y: auto; }
	.sidebar-footer { padding: 20px 26px; font-size: 11px; color: var(--text-muted); display: flex; gap: 8px; align-items: center; }
	.workspace { min-width: 0; }
	.topbar { position: sticky; top: 0; z-index: 10; height: 65px; border-bottom: 1px solid var(--border); display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 0 36px; background: var(--surface); }
	.breadcrumb { display: flex; align-items: center; gap: 14px; color: var(--text-muted); font-size: 12px; }
	.breadcrumb strong { color: var(--text-secondary); font-weight: 500; }
	.sync { display: flex; gap: 7px; align-items: center; font-size: 11px; color: var(--text-muted); }
	.sync.stale { color: var(--status-critical); }
	.status-dot { display: inline-block; width: 6px; height: 6px; flex-shrink: 0; border-radius: 50%; background: var(--status-good); }
	.status-dot.offline { background: var(--text-muted); }
	main { max-width: 1600px; margin: 0 auto; padding: 34px 36px 64px; }
	.page-heading { margin-bottom: 26px; }
	.eyebrow { text-transform: uppercase; letter-spacing: .12em; color: var(--accent); font-size: 10px; font-weight: 650; margin: 0 0 8px; }
	h1 { font-size: 30px; line-height: 1.2; letter-spacing: -.8px; }
	.description { color: var(--text-secondary); font-size: 13px; margin: 10px 0 0; }
	.filters { display: flex; flex-wrap: wrap; align-items: end; gap: 16px; padding: 16px; border: 1px solid var(--border); border-radius: 10px; background: var(--surface); margin-bottom: 28px; }
	label, .filter-label { display: flex; flex-direction: column; gap: 6px; color: var(--text-secondary); font-size: 11px; font-weight: 550; }
	.filter-label { margin-bottom: 6px; }
	label { flex: 1; min-width: 140px; max-width: 280px; }
	.seg { display: flex; gap: 3px; background: var(--page); border: 1px solid var(--border); border-radius: 7px; padding: 3px; }
	button, select { font: inherit; color: var(--text-primary); border: 1px solid var(--border); background: var(--surface); border-radius: 6px; min-height: 36px; padding: 7px 12px; }
	button { cursor: pointer; }
	.seg button { min-height: 28px; padding: 4px 12px; border: 0; background: transparent; font-size: 12px; color: var(--text-secondary); }
	.seg button.active { background: var(--accent); color: var(--surface); }
	select { width: 100%; font-size: 12px; }
	.reset { color: var(--accent); background: none; border-color: transparent; font-size: 12px; }
	.view-content { min-width: 0; }
	.notice { padding: 12px 16px; color: var(--status-critical); border: 1px solid var(--border); border-radius: 8px; }
	.loading, .empty-state { padding: 48px 24px; text-align: center; color: var(--text-secondary); border: 1px dashed var(--border); border-radius: 12px; }
	.empty-state h2 { font-size: 18px; color: var(--text-primary); }
	.empty-state a { color: var(--accent); }
	.menu-toggle { display: none; }
	.skip-link { position: fixed; left: 16px; top: -100px; z-index: 20; background: var(--surface); color: var(--accent); padding: 12px; }
	.skip-link:focus { top: 8px; }
	@media (max-width: 1100px) { main { padding: 28px 24px 48px; } .topbar { padding: 0 24px; } .app-shell { grid-template-columns: 200px minmax(0, 1fr); } }
	@media (max-width: 760px) {
		.app-shell { display: flex; flex-direction: column; }
		aside { position: static; height: auto; border-right: 0; border-bottom: 1px solid var(--border); }
		.brand { padding: 14px 20px; }
		.navigation, .sidebar-footer { display: none; }
		aside.open .navigation { display: block; max-height: 55dvh; padding: 12px 20px; }
		.menu-toggle { display: block; font-size: 12px; }
		.topbar { height: auto; min-height: 60px; padding: 12px 20px; flex-wrap: wrap; gap: 10px; }
		.breadcrumb { margin-right: auto; gap: 8px; }
		main { padding: 24px 16px 48px; }
		h1 { font-size: 26px; }
		.filters { gap: 12px; }
		.period { width: 100%; }
		.seg button { flex: 1; }
		label { min-width: 0; max-width: none; width: 100%; flex-basis: 100%; }
	}
</style>
