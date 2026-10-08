<script lang="ts">
	import { onMount } from 'svelte';
	import type { Routing } from '$lib/console/model';
	import RequestTicker from '$lib/RequestTicker.svelte';
	import ResetCredits from '$lib/charts/ResetCredits.svelte';
	import AccountNickname from '$lib/charts/AccountNickname.svelte';
	import { accountStatus, barPercent, elapsedPercent, expired, widgetGroups, widgetProviderGroups, widgetWindows, widgetWindowLabel, widgetReset, type LimitsSnapshot } from '$lib/limits';

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
	const groups = $derived(widgetProviderGroups(snapshot?.accounts ?? []));
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

<svelte:head><title>Usage widget · Garcon</title><meta name="description" content="Live Codex and Claude account usage, reset countdowns, and elapsed-period markers." /></svelte:head>
<svelte:window onkeydown={shortcut} />

<div class="widget-shell">
<main class="widget-page">
	<section class="widget" aria-label="Account usage widget">
		<header>
			<h1>Usage</h1>
			<div class="summary"><span>{snapshot?.accounts.length ?? 0} profiles</span><span class:stale title={checkedAt > 0 ? `Last provider check: ${new Date(checkedAt).toLocaleString()}` : undefined}>{busy ? 'refreshing…' : !snapshot ? 'connecting…' : checkedAt > 0 ? `checked ${new Date(checkedAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false })}` : 'awaiting usage'}</span>{#if staleAccounts}<span class="stale">{staleAccounts} {staleAccounts === 1 ? 'profile needs' : 'profiles need'} attention</span>{/if}<button onclick={refresh} disabled={busy} aria-keyshortcuts="R"><kbd>R</kbd> refresh</button><a class="dashboard-link" href="/" target="_blank" rel="noopener noreferrer" title="Open the main Garcon dashboard in your browser">Dashboard ↗</a></div>
		</header>
		{#if error || snapshot?.error}<p class="notice" role="status">{error || snapshot?.error}</p>{/if}
		{#if routingError || routing?.error}<p class="notice" role="status">{routingError || routing?.error}</p>{/if}
		{#each groups as group (group.key)}
			<section class="provider-group" style:--group-rows={group.accounts.reduce((n, a) => n + widgetWindows(a).length, 0)} aria-label={group.label}>
				<h2>{group.label}</h2>
				<div class="columns" aria-hidden="true"><span>Window</span><span>Used</span><span>Resets in</span></div>
				{#each group.accounts as account (account.id)}
					{@const status = accountStatus(account, now)}
					{@const route = account.provider === 'codex' && account.workspace ? routing?.accounts.find(a => a.id === account.workspace) : undefined}
					{@const isPinned = !!route && routing?.pinned_account === route.id}
					{@const isNext = !!route && routing?.next_account === route.id}
					{@const color = accountColors[account.email.toLowerCase() || account.id]}
					<section class="account-group" class:up-next={isNext} style:--account-color={color} style:--group-rows={widgetWindows(account).length} aria-label={account.email || 'Account identity unavailable'}>
						<div class="account-heading">
							<AccountNickname email={account.email || 'Account identity unavailable'} accountKey={account.provider === 'codex' && account.workspace ? `codex:${account.workspace}` : undefined} {color} />
							{#if account.provider === 'codex'}
								{#if route && routing?.pinned_account !== undefined}
									<button
										class="pin-control" class:pinned={isPinned}
										aria-pressed={isPinned}
										aria-label={`${isPinned ? 'Unpin' : 'Pin'} ${account.email} for new Codex conversations`}
										title={isPinned ? 'Unpin to resume automatic routing for new conversations' : !route.enrolled ? 'Add this account to the Codex pool to pin it' : isNext ? 'Next for a new conversation (model access may change the choice); click to pin new Codex conversations here' : 'Pin new Codex conversations to this account; existing conversations keep their original account'}
										disabled={pinning || (!isPinned && (!route.enrolled || route.status === 'Saved login missing'))}
										onclick={() => pinAccount(isPinned ? '' : route.id)}
									>
										<svg viewBox="0 0 16 16" width="10" height="10" aria-hidden="true"><path d="m5 2 6 0-1 4 2 3H4l2-3-1-4ZM8 9v5" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" /></svg>
										{isPinned ? 'Pinned' : 'Pin'}
									</button>
								{/if}
								<ResetCredits {account} {now} compact />
							{/if}
						</div>
					{#each widgetWindows(account) as window (window?.id ?? 'unavailable')}
						{@const isExpired = window ? expired(window, now) : false}
						{@const elapsed = window ? elapsedPercent(window, now) : null}
						{@const used = window?.used_percent ?? null}
						{@const label = widgetWindowLabel(window, account.provider)}
						{@const reset = widgetReset(window, now)}
						<div class="usage-row" class:claude={account.provider === 'claude'} class:expired={isExpired}>
							<span class="window" title={label}>{label}</span>
							<div class="usage"><strong>{used == null ? '—' : `${Number(used.toFixed(1))}%`}</strong><div class="track" role="img" aria-label="{account.email}, {account.provider}, {label}: {used == null ? 'usage unavailable' : `${used}% used`}{elapsed == null ? '' : `; ${Math.round(elapsed)}% of period elapsed`}{isExpired ? '; expired snapshot' : ''}"><span class="fill" style:width={`${barPercent(used)}%`}></span>{#if elapsed != null}<span class="pace" style:left={`${elapsed}%`} title="{Math.round(elapsed)}% of period elapsed"></span>{/if}</div></div>
							<span class="reset" title={reset.description} aria-label={reset.description}>{reset.text}</span>
						</div>
					{/each}
					{#if status !== 'Up to date'}<p class="account-status" role="status">{status}{account.error && account.error !== status ? ` · ${account.error}` : ''}{account.fetched_at > 0 ? ` · last success ${new Date(account.fetched_at).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false })}` : ''}</p>{/if}
					</section>
				{/each}
			</section>
		{:else}
			<p class="empty" role="status">{!snapshot || busy ? 'Loading account usage…' : 'No local subscription logins found. Sign in with Codex or Claude, then refresh.'}</p>
		{/each}
	</section>
</main>
<RequestTicker {accountColors} />
</div>

<style>
	.pin-control { display: inline-flex; align-items: center; gap: 3px; padding: 1px 4px; border: 1px solid var(--widget-border); border-radius: 4px; color: var(--widget-muted); font-size: 10px; line-height: 1.2; cursor: pointer; white-space: nowrap; }
	.pin-control:focus-visible { outline: 2px solid var(--account-color, #3c82eb); outline-offset: 2px; }
	.pin-control.pinned { color: var(--widget-text); background: color-mix(in srgb, var(--account-color) 18%, var(--widget-bg)); border-color: var(--account-color); }
	.account-group.up-next { background: color-mix(in srgb, var(--account-color) 7%, var(--widget-bg)); }
	.widget-shell { --request-ticker-height: clamp(40px, 7svh, 62px); --page-gap: clamp(4px, 1svh, 12px); }
	.widget-page { padding: var(--page-gap) clamp(8px, 1.2vw, 20px); padding-bottom: calc(var(--request-ticker-height) + var(--page-gap) + env(safe-area-inset-bottom)); min-height: 100svh; }
	.widget { --widget-bg: #fff; --widget-head: #f8fafc; --widget-track: #e4e9ef; --widget-border: #dce2e9; --widget-text: #26313e; --widget-muted: #68778a; --codex-color: #475569; --claude-color: #b95030; background: var(--widget-bg); color: var(--widget-text); max-width: 1800px; margin: 0 auto; border: 1px solid var(--widget-border); border-radius: 12px; overflow: hidden; box-shadow: 0 2px 6px #0000000a; font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: clamp(11px, .95vw, 15px); font-variant-numeric: tabular-nums; }
	.widget { display: flex; flex-direction: column; height: calc(100svh - var(--request-ticker-height) - 2 * var(--page-gap) - env(safe-area-inset-bottom)); min-height: min-content; }
	header { display: flex; flex-shrink: 0; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 4px 20px; padding: clamp(2px, calc(2svh - 6px), 12px) 22px; background: var(--widget-head); border-bottom: 1px solid var(--widget-border); }
	h1 { font-size: 16px; text-transform: uppercase; letter-spacing: .2em; }
	.dashboard-link { color: var(--widget-text); text-decoration: underline; text-underline-offset: 3px; white-space: nowrap; }
	.dashboard-link:focus-visible { outline: 2px solid var(--account-color, #3c82eb); outline-offset: 2px; }
	.summary { display: flex; align-items: center; flex-wrap: wrap; gap: 6px 12px; color: var(--widget-muted); font-size: 12px; }
	.summary > :not(:first-child)::before { content: '·'; margin-right: 12px; color: var(--widget-muted); }
	button { border: 0; border-radius: 4px; background: transparent; padding: 3px 0; color: inherit; font: inherit; }
	button:hover:not(:disabled) { color: var(--widget-text); }
	button:disabled { opacity: .65; }
	kbd { font: inherit; }
	.columns, .usage-row { display: grid; grid-template-columns: 150px minmax(120px, 1fr) 105px; gap: 20px; align-items: center; padding: 0 24px 0 28px; }
	.columns { flex-shrink: 0; color: var(--widget-muted); font-size: 10px; letter-spacing: .12em; text-transform: uppercase; border-bottom: 1px solid var(--widget-border); padding-top: clamp(1px, calc(1svh - 3px), 7px); padding-bottom: clamp(1px, calc(1svh - 3px), 7px); }
	.columns > :last-child { text-align: right; white-space: nowrap; letter-spacing: .06em; }
	.provider-group { display: flex; flex-direction: column; flex: var(--group-rows) 0 auto; }
	h2 { margin: 0; padding: 7px 12px; background: var(--widget-head); border-bottom: 1px solid var(--widget-border); font-size: 12px; letter-spacing: .08em; }
	.account-heading { display: flex; align-items: center; flex-wrap: wrap; gap: 3px 8px; padding: 2px 24px 2px 12px; font-size: 11px; }
	.account-heading :global(.reset-credits) { margin-left: auto; justify-content: flex-end; }
	.account-group { display: flex; flex-direction: column; flex: var(--group-rows) 0 auto; border-left: 5px solid var(--account-color); border-bottom: 1px solid var(--widget-border); }
	.usage-row { --fill: var(--codex-color); flex: 1 0 18px; padding-left: 23px; min-height: 18px; border-top: 1px solid color-mix(in srgb, var(--widget-border) 60%, transparent); }
	.usage-row:first-child { border-top: 0; }
	.usage-row.claude { --fill: var(--claude-color); }
	.window { color: var(--widget-text); min-width: 0; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
	.usage { display: grid; grid-template-columns: 44px minmax(0, 1fr); align-items: center; gap: 16px; }
	.usage strong { text-align: right; font-size: clamp(11px, 1.7svh, 15px); }
	.track { height: clamp(8px, calc(2.5svh - 2px), 23px); border-radius: 5px; background: var(--widget-track); position: relative; }
	.fill { display: block; height: 100%; border-radius: 5px; background: var(--fill); }
	.pace { position: absolute; top: -3px; bottom: -3px; width: 2px; transform: translateX(-1px); background: var(--widget-text); box-shadow: 0 0 0 1px var(--widget-bg); }
	.reset { text-align: right; color: var(--widget-muted); font-size: 12px; white-space: nowrap; }
	.expired .track { opacity: .4; }
	.expired .reset { color: var(--claude-color); }
	.account-status, .notice { color: var(--claude-color); font-size: 11px; padding: 0 23px 8px; margin: 0; }
	.notice { padding-top: 12px; }
	.stale { color: var(--claude-color); }
	.empty { padding: 28px; color: var(--widget-muted); }
	@media (prefers-color-scheme: dark) {
		.widget { --widget-bg: #0e1218; --widget-head: #11151d; --widget-track: #232c38; --widget-border: #293341; --widget-text: #e5e8ef; --widget-muted: #929eaf; --codex-color: #e5e8ef; --claude-color: #df805d; }
	}
	@media (max-width: 850px) {
		.account-heading { padding-right: 12px; }
		.columns, .usage-row { grid-template-columns: 100px minmax(60px,1fr) 64px; gap: 10px; padding-right: 12px; padding-left: 17px; }
		.usage-row { padding-left: 12px; }
		.usage { grid-template-columns: 34px minmax(0,1fr); gap: 12px; }
		.reset { font-size: 11px; }
	}
	@media (max-width: 480px) {
		.account-heading { padding-right: 8px; }
		header { padding-left: 12px; padding-right: 12px; }
		.summary { font-size: 10px; gap: 6px 8px; }
		.summary > :not(:first-child)::before { margin-right: 8px; }
		h1 { font-size: 14px; }
		.columns, .usage-row { grid-template-columns: minmax(54px, 78px) minmax(44px, 1fr) 56px; gap: 6px; padding-right: 8px; padding-left: 13px; }
		.columns { font-size: 8px; letter-spacing: .04em; }
		.usage-row { flex-basis: 24px; min-height: 24px; padding-left: 8px; font-size: 10px; }
		.usage { grid-template-columns: 30px minmax(0, 1fr); gap: 5px; }
		.usage strong { font-size: 10px; }
		.reset { font-size: 9px; }
		.track { height: clamp(8px, 1.8svh, 14px); }
		.pace { top: -3px; bottom: -3px; }
	}
</style>
