<script lang="ts">
	import { onMount } from 'svelte';
	import type { Routing } from '$lib/console/model';
	import RequestTicker from '$lib/RequestTicker.svelte';
	import ResetCredits from '$lib/charts/ResetCredits.svelte';
	import AccountNickname from '$lib/charts/AccountNickname.svelte';
	import codexLogo from '$lib/assets/providers/openai-app.png';
	import claudeLogo from '$lib/assets/providers/claude-app.png';
	import googleLogo from '$lib/assets/providers/google.svg';
	import facebookLogo from '$lib/assets/providers/facebook.svg';
	import {
		accountStatus,
		barPercent,
		elapsedPercent,
		expired,
		widgetAccountCells,
		widgetGroups,
		widgetProviderGroups,
		widgetReset,
		type LimitAccount,
		type LimitWindow,
		type LimitsSnapshot
	} from '$lib/limits';

	const sectionDefs: { key: LimitAccount['provider']; label: string; logo: string }[] = [
		{ key: 'codex', label: 'Codex', logo: codexLogo },
		{ key: 'claude', label: 'Claude', logo: claudeLogo },
		{ key: 'agy', label: 'Google', logo: googleLogo },
		{ key: 'muse', label: 'Facebook', logo: facebookLogo }
	];

	let routing = $state<Routing | null>(null);
	let routingError = $state('');
	let pinning = $state(false);
	let routingGeneration = 0;

	async function loadRouting() {
		const generation = ++routingGeneration;
		try {
			const res = await fetch('/api/routing/codex');
			if (!res.ok) throw new Error();
			const result: Routing = await res.json();
			if (generation === routingGeneration) { routing = result; routingError = ''; }
		} catch {
			if (generation === routingGeneration) { routing = null; routingError = 'Codex routing unavailable. Retrying automatically.'; }
		}
	}
	async function pinAccount(id: string) {
		if (pinning) return;
		pinning = true;
		++routingGeneration;
		try {
			const res = await fetch('/api/routing/codex', {
				method: 'PUT', headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ pinned_account: id })
			});
			const result = await res.json();
			if (!res.ok) throw new Error(result.error?.message || 'Could not change the Codex pin.');
			routing = result; routingError = '';
		} catch (e) { routingError = e instanceof Error ? e.message : 'Could not change the Codex pin.'; }
		finally { pinning = false; }
	}

	let snapshot = $state<LimitsSnapshot | null>(null);
	let error = $state('');
	let requesting = $state(false);
	let now = $state(Date.now());
	const providerMap = $derived(new Map(widgetProviderGroups(snapshot?.accounts ?? []).map(g => [g.key, g.accounts])));
	const sections = $derived(
		snapshot?.accounts.length
			? sectionDefs.map(s => ({ ...s, accounts: providerMap.get(s.key) ?? [] }))
			: []
	);
	const accountGroups = $derived(widgetGroups(snapshot?.accounts ?? []));
	const busy = $derived(requesting || snapshot?.refreshing);
	const groupColors = ['#22b66b', '#3c82eb', '#db4b9e', '#a67add', '#d99b30', '#21a2b5'];
	const accountColors = $derived(Object.fromEntries(accountGroups.map((group, i) => [group.key, groupColors[i % groupColors.length]])));
	const checkedAt = $derived(snapshot?.updated_at ?? 0);
	const staleAccounts = $derived(snapshot?.accounts.filter(a => accountStatus(a, now) !== 'Up to date').length ?? 0);
	const stale = $derived(Boolean(error || snapshot?.error || staleAccounts || (checkedAt > 0 && now - checkedAt > 600_000)));

	async function load() {
		try {
			const res = await fetch('/api/limits');
			if (!res.ok) throw new Error();
			snapshot = await res.json(); error = '';
		} catch { error = 'Cannot reach Garcon. Retrying automatically.'; }
	}
	async function refresh() {
		if (busy) return;
		requesting = true;
		try {
			const res = await fetch('/api/limits/refresh', { method: 'POST' });
			if (!res.ok) throw new Error();
			snapshot = await res.json(); error = '';
		} catch { error = 'Could not refresh usage. Retrying automatically.'; }
		finally { requesting = false; if (!pinning) loadRouting(); }
	}
	function shortcut(event: KeyboardEvent) {
		if (event.key.toLowerCase() !== 'r' || event.repeat || event.ctrlKey || event.metaKey || event.altKey) return;
		const target = event.target as HTMLElement | null;
		if (target?.closest('input, textarea, select, [contenteditable="true"]')) return;
		event.preventDefault(); refresh();
	}
	onMount(() => {
		load(); loadRouting();
		const poll = setInterval(() => { load(); if (!pinning) loadRouting(); }, 10_000);
		const clock = setInterval(() => { now = Date.now(); }, 1000);
		return () => { clearInterval(poll); clearInterval(clock); };
	});
</script>

<svelte:head><title>Usage widget · Garcon</title><meta name="description" content="Live Codex, Claude, Google AGY, and Facebook Muse account usage in a compact table." /></svelte:head>
<svelte:window onkeydown={shortcut} />

{#snippet windowCell(account: LimitAccount, window: LimitWindow | null, badgeLabel = '')}
	{@const isExpired = window ? expired(window, now) : false}
	{@const elapsed = window ? elapsedPercent(window, now) : null}
	{@const used = window?.used_percent ?? null}
	{@const reset = widgetReset(window, now)}
	<div class="cell-window" class:claude={account.provider === 'claude'} class:google={account.provider === 'agy'} class:facebook={account.provider === 'muse'} class:expired={isExpired} class:empty-cell={!window}>
		{#if window}
			<div class="cell-top">
				<strong>
					{#if badgeLabel}<span class="scope-tag">{badgeLabel}</span>{/if}{used == null ? '—' : `${Number(used.toFixed(0))}%`}
				</strong>
				<span class="reset" title={reset.description} aria-label={reset.description}>{reset.text}</span>
			</div>
			<div class="track" role="img" aria-label="{account.email}, {account.provider}, {window.label}: {used == null ? 'usage unavailable' : `${used}% used`}{elapsed == null ? '' : `; ${Math.round(elapsed)}% of period elapsed`}{isExpired ? '; expired snapshot' : ''}">
				<span class="fill" style:width={`${barPercent(used)}%`}></span>
				{#if elapsed != null}<span class="pace" style:left={`${elapsed}%`} title="{Math.round(elapsed)}% of period elapsed"></span>{/if}
			</div>
		{:else}
			<span class="cell-dash">—</span>
		{/if}
	</div>
{/snippet}

<div class="widget-shell">
<main class="widget-page">
	<section class="widget" aria-label="Account usage widget">
		<header>
			<h1>Usage</h1>
			<div class="summary">
				<span>{snapshot?.accounts.length ?? 0} profiles</span>
				<span class:stale title={checkedAt > 0 ? `Last provider check: ${new Date(checkedAt).toLocaleString()}` : undefined}>
					{busy ? 'refreshing…' : !snapshot ? 'connecting…' : checkedAt > 0 ? `checked ${new Date(checkedAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false })}` : 'awaiting usage'}
				</span>
				{#if staleAccounts}<span class="stale">{staleAccounts} {staleAccounts === 1 ? 'needs' : 'need'} attention</span>{/if}
				<button onclick={refresh} disabled={busy} aria-keyshortcuts="R"><kbd>R</kbd> refresh</button>
				<a class="dashboard-link" href="/" target="_blank" rel="noopener noreferrer" title="Open the main Garcon dashboard in your browser">Dashboard ↗</a>
			</div>
		</header>
		{#if error || snapshot?.error}<p class="notice" role="status">{error || snapshot?.error}</p>{/if}
		{#if routingError || routing?.error}<p class="notice" role="status">{routingError || routing?.error}</p>{/if}

		{#if sections.length}
			<div class="table-columns" aria-hidden="true">
				<span>Account</span>
				<span>5-hour</span>
				<span>Weekly</span>
				<span>Scoped / Credits</span>
			</div>
			<div class="sections-body">
				{#each sections as section (section.key)}
					<section class="provider-group" aria-label={section.label}>
						<h2 class="section-heading">
							<img class="section-logo" src={section.logo} width="16" height="16" alt="" aria-hidden="true" />
							<span>{section.label}</span>
							<small>{section.accounts.length}</small>
						</h2>
						{#each section.accounts as account (account.id)}
							{@const status = accountStatus(account, now)}
							{@const route = account.provider === 'codex' && account.workspace ? routing?.accounts.find(a => a.id === account.workspace) : undefined}
							{@const isPinned = !!route && routing?.pinned_account === route.id}
							{@const isNext = !!route && routing?.next_account === route.id}
							{@const color = accountColors[account.email.toLowerCase() || account.id]}
							{@const cells = widgetAccountCells(account)}
							<div class="account-row" class:up-next={isNext} style:--account-color={color} aria-label={account.email || 'Account identity unavailable'}>
								<div class="account-cell">
									<div class="account-line">
										<AccountNickname email={account.email || 'Account identity unavailable'} accountKey={account.provider === 'codex' && account.workspace ? `codex:${account.workspace}` : undefined} {color} />
										{#if account.provider === 'codex' && route && routing?.pinned_account !== undefined}
											<button
												class="pin-control" class:pinned={isPinned}
												aria-pressed={isPinned}
												aria-label={`${isPinned ? 'Unpin' : 'Pin'} ${account.email} for new Codex conversations`}
												title={isPinned ? 'Unpin to resume automatic routing for new conversations' : !route.enrolled ? 'Add this account to the Codex pool to pin it' : isNext ? 'Next for a new conversation; click to pin' : 'Pin new Codex conversations to this account'}
												disabled={pinning || (!isPinned && (!route.enrolled || route.status === 'Saved login missing'))}
												onclick={() => pinAccount(isPinned ? '' : route.id)}
											>
												<svg viewBox="0 0 16 16" width="9" height="9" aria-hidden="true"><path d="m5 2 6 0-1 4 2 3H4l2-3-1-4ZM8 9v5" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" /></svg>
												{isPinned ? 'Pinned' : 'Pin'}
											</button>
										{/if}
									</div>
									{#if status !== 'Up to date'}
										<span class="account-status" role="status" title={account.error || status}>{status}</span>
									{/if}
								</div>

								{@render windowCell(account, cells.fiveHour)}
								{@render windowCell(account, cells.weekly)}

								<div class="extra-cell">
									{#if cells.extra}
										{@render windowCell(account, cells.extra, cells.extraLabel)}
									{:else if account.provider === 'codex'}
										<ResetCredits {account} {now} compact />
									{:else if account.plan}
										<span class="plan-pill">{account.plan}</span>
									{:else}
										<span class="cell-dash">—</span>
									{/if}
								</div>
							</div>
						{:else}
							<div class="empty-section-row">No local {section.label} login discovered</div>
						{/each}
					</section>
				{/each}
			</div>
		{:else}
			<p class="empty" role="status">{!snapshot || busy ? 'Loading account usage…' : 'No local subscription logins found. Sign in with Codex, Claude, AGY, or Muse, then refresh.'}</p>
		{/if}
	</section>
</main>
<RequestTicker {accountColors} />
</div>

<style>
	.widget-shell { --request-ticker-height: clamp(36px, 6svh, 52px); --page-gap: clamp(4px, 0.8svh, 10px); }
	.widget-page { padding: var(--page-gap) clamp(6px, 1vw, 16px); padding-bottom: calc(var(--request-ticker-height) + var(--page-gap) + env(safe-area-inset-bottom)); min-height: 100svh; }
	.widget {
		--widget-bg: #fff;
		--widget-head: #f8fafc;
		--widget-track: #e4e9ef;
		--widget-border: #dce2e9;
		--widget-text: #26313e;
		--widget-muted: #68778a;
		--codex-color: #475569;
		--claude-color: #b95030;
		--google-color: #2563eb;
		--facebook-color: #1877f2;
		background: var(--widget-bg);
		color: var(--widget-text);
		max-width: 1400px;
		margin: 0 auto;
		border: 1px solid var(--widget-border);
		border-radius: 10px;
		overflow: hidden;
		box-shadow: 0 2px 6px #0000000a;
		font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
		font-size: clamp(10px, 0.85vw, 13px);
		font-variant-numeric: tabular-nums;
		display: flex;
		flex-direction: column;
	}
	header {
		display: flex;
		flex-shrink: 0;
		justify-content: space-between;
		align-items: center;
		flex-wrap: wrap;
		gap: 4px 14px;
		padding: 6px 14px;
		background: var(--widget-head);
		border-bottom: 1px solid var(--widget-border);
	}
	h1 { margin: 0; font-size: 13px; text-transform: uppercase; letter-spacing: .16em; }
	.dashboard-link { color: var(--widget-text); text-decoration: underline; text-underline-offset: 2px; white-space: nowrap; }
	.dashboard-link:focus-visible { outline: 2px solid var(--account-color, #3c82eb); outline-offset: 2px; }
	.summary { display: flex; align-items: center; flex-wrap: wrap; gap: 4px 10px; color: var(--widget-muted); font-size: 11px; }
	.summary > :not(:first-child)::before { content: '·'; margin-right: 10px; color: var(--widget-muted); }
	button { border: 0; border-radius: 4px; background: transparent; padding: 1px 0; color: inherit; font: inherit; cursor: pointer; }
	button:hover:not(:disabled) { color: var(--widget-text); }
	button:disabled { opacity: .65; cursor: default; }
	kbd { font: inherit; }

	.table-columns, .account-row {
		display: grid;
		grid-template-columns: minmax(120px, 1.35fr) minmax(82px, 1fr) minmax(82px, 1fr) minmax(88px, 1.05fr);
		gap: 10px;
		align-items: center;
		padding: 3px 12px;
	}
	.table-columns {
		flex-shrink: 0;
		color: var(--widget-muted);
		font-size: 9px;
		letter-spacing: .1em;
		text-transform: uppercase;
		background: var(--widget-head);
		border-bottom: 1px solid var(--widget-border);
		padding-top: 4px;
		padding-bottom: 4px;
	}

	.sections-body { display: flex; flex-direction: column; }
	.provider-group { display: flex; flex-direction: column; }
	.section-heading {
		margin: 0;
		padding: 4px 12px;
		background: var(--widget-head);
		border-bottom: 1px solid var(--widget-border);
		font-size: 11px;
		letter-spacing: .05em;
		display: flex;
		align-items: center;
		gap: 6px;
	}
	.section-logo { display: block; width: 15px; height: 15px; border-radius: 3px; flex-shrink: 0; }
	.section-heading small { color: var(--widget-muted); font-weight: 400; font-size: 10px; margin-left: auto; }

	.account-row {
		border-left: 4px solid var(--account-color, var(--codex-color));
		border-bottom: 1px solid var(--widget-border);
		padding-left: 8px;
		min-height: 28px;
	}
	.account-row.up-next { background: color-mix(in srgb, var(--account-color) 7%, var(--widget-bg)); }
	.account-cell { min-width: 0; display: flex; flex-direction: column; gap: 1px; }
	.account-line { display: flex; align-items: center; gap: 5px; min-width: 0; flex-wrap: nowrap; }
	.account-line :global(.account-nickname) { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

	.pin-control {
		display: inline-flex;
		align-items: center;
		gap: 2px;
		padding: 1px 4px;
		border: 1px solid var(--widget-border);
		border-radius: 4px;
		color: var(--widget-muted);
		font-size: 9px;
		line-height: 1.1;
		white-space: nowrap;
		flex-shrink: 0;
	}
	.pin-control.pinned {
		color: var(--widget-text);
		background: color-mix(in srgb, var(--account-color) 18%, var(--widget-bg));
		border-color: var(--account-color);
	}

	.cell-window {
		--fill: var(--codex-color);
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
	}
	.cell-window.claude { --fill: var(--claude-color); }
	.cell-window.google { --fill: var(--google-color); }
	.cell-window.facebook { --fill: var(--facebook-color); }
	.cell-top { display: flex; justify-content: space-between; align-items: baseline; gap: 4px; font-size: 10px; line-height: 1.15; }
	.cell-top strong { font-size: 11px; white-space: nowrap; display: inline-flex; align-items: baseline; gap: 3px; }
	.scope-tag { color: var(--widget-muted); font-weight: 500; font-size: 9px; }
	.reset { color: var(--widget-muted); font-size: 9.5px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
	.track { height: 5px; border-radius: 3px; background: var(--widget-track); position: relative; }
	.fill { display: block; height: 100%; border-radius: 3px; background: var(--fill); }
	.pace { position: absolute; top: -2px; bottom: -2px; width: 1.5px; transform: translateX(-0.75px); background: var(--widget-text); box-shadow: 0 0 0 1px var(--widget-bg); }
	.expired .track { opacity: .4; }
	.expired .reset { color: var(--claude-color); }

	.extra-cell { min-width: 0; display: flex; align-items: center; }
	.extra-cell .cell-window { width: 100%; }
	.extra-cell :global(.reset-credits) { font-size: 10px; }
	.plan-pill { color: var(--widget-muted); font-size: 10px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
	.cell-dash { color: var(--widget-muted); font-size: 10px; }
	.empty-section-row { padding: 4px 12px; color: var(--widget-muted); font-size: 10px; border-bottom: 1px solid var(--widget-border); }

	.account-status { color: var(--claude-color); font-size: 9.5px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
	.notice { color: var(--claude-color); font-size: 10px; padding: 4px 12px; margin: 0; border-bottom: 1px solid var(--widget-border); }
	.stale { color: var(--claude-color); }
	.empty { padding: 20px; color: var(--widget-muted); margin: 0; }

	@media (prefers-color-scheme: dark) {
		.widget {
			--widget-bg: #0e1218;
			--widget-head: #11151d;
			--widget-track: #232c38;
			--widget-border: #293341;
			--widget-text: #e5e8ef;
			--widget-muted: #929eaf;
			--codex-color: #e5e8ef;
			--claude-color: #df805d;
			--google-color: #60a5fa;
			--facebook-color: #4599ff;
		}
	}
	@media (max-width: 520px) {
		header { padding: 5px 10px; }
		.summary { font-size: 9.5px; gap: 3px 6px; }
		.summary > :not(:first-child)::before { margin-right: 6px; }
		.table-columns, .account-row {
			grid-template-columns: minmax(90px, 1.2fr) minmax(62px, 1fr) minmax(62px, 1fr) minmax(66px, 1fr);
			gap: 6px;
			padding: 3px 8px;
		}
		.section-heading { padding: 3px 8px; font-size: 10px; }
		.cell-top strong { font-size: 10px; }
		.reset { font-size: 8.5px; }
	}
</style>
