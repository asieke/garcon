<script lang="ts">
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
	const TABS = [
		['overview', 'Overview'],
		['usage', 'Usage'],
		['cost', 'Cost'],
		['models', 'Models'],
		['accounts', 'Accounts'],
		['sessions', 'Sessions'],
		['activity', 'Activity'],
		['performance', 'Performance'],
		['latency', 'Latency'],
		['logs', 'Logs'],
		['settings', 'Settings']
	] as const;
	const POLL_MS = 10_000;

	let rows = $state<Row[]>([]);
	let days = $state(7);
	let tab = $state<(typeof TABS)[number][0]>('overview');
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

<svelte:head><title>Garcon</title></svelte:head>

<header>
	<h1>Garcon</h1>
	<span class="sync" class:stale>
		{#if stale}Connection lost — retrying…{:else if lastSync}Synced {when(lastSync)}{:else}Connecting…{/if}
	</span>
</header>

<div class="filters">
	<nav class="seg">
		{#each WINDOWS as [label, d]}
			<button class:active={days === d} onclick={() => (days = d)}>{label}</button>
		{/each}
	</nav>
	<nav class="seg">
		<button class:active={harnessFilter === 'all'} onclick={() => (harnessFilter = 'all')}>All harnesses</button>
		{#each harnessesAll as h (h)}
			<button class:active={harnessFilter === h} onclick={() => (harnessFilter = h)}>{harnessLabel(h)}</button>
		{/each}
	</nav>
	<select bind:value={accountFilter}>
		<option value="all">All accounts</option>
		{#each accountsAll as a}<option value={a}>{a}</option>{/each}
	</select>
</div>

<nav class="tabs">
	{#each TABS as [key, label]}
		<button class:active={tab === key} onclick={() => (tab = key)}>{label}</button>
	{/each}
</nav>

{#if loading}
	<p class="loading">Loading…</p>
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
	{:else if tab === 'settings'}
		<SettingsSection {rows} pollMs={POLL_MS} {now} />
	{/if}
{/if}

<style>
	:global(body) {
		margin: 0 auto;
		max-width: 1200px;
		padding: 24px 16px 64px;
	}
	header {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		justify-content: space-between;
		gap: 12px;
		margin-bottom: 20px;
	}
	h1 {
		font-size: 20px;
	}
	.sync {
		font-size: 12px;
		color: var(--text-muted);
	}
	.sync.stale {
		color: var(--status-critical);
	}
	.filters {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 10px;
		margin-bottom: 16px;
		padding-bottom: 16px;
		border-bottom: 1px solid var(--border);
	}
	.seg {
		display: flex;
		gap: 4px;
	}
	button {
		font: inherit;
		padding: 4px 12px;
		border-radius: 6px;
		cursor: pointer;
		border: 1px solid var(--border);
		background: none;
		color: inherit;
	}
	button.active {
		background: var(--text-primary);
		color: var(--page);
		border-color: var(--text-primary);
	}
	select {
		font: inherit;
		padding: 4px 10px;
		border-radius: 6px;
		border: 1px solid var(--border);
		background: var(--surface);
		color: inherit;
		max-width: 220px;
	}
	.tabs {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		margin-bottom: 24px;
	}
	.tabs button {
		border: none;
		border-radius: 0;
		padding: 8px 4px;
		border-bottom: 2px solid transparent;
		color: var(--text-secondary);
	}
	.tabs button.active {
		background: none;
		color: var(--text-primary);
		border-bottom-color: var(--series-1);
		font-weight: 600;
	}
	.loading {
		color: var(--text-muted);
	}
</style>
