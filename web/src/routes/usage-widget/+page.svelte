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
		resetCreditInfo,
		widgetAccountCells,
		widgetGroups,
		widgetProviderGroups,
		widgetReset,
		type LimitAccount,
		type LimitWindow,
		type LimitsSnapshot
	} from '$lib/limits';

	const providerLogos: Record<LimitAccount['provider'], string> = {
		codex: codexLogo,
		claude: claudeLogo,
		agy: googleLogo,
		muse: facebookLogo
	};

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
	// Only providers with a discovered login get a section; empty placeholders waste rows.
	const sections = $derived(
		widgetProviderGroups(snapshot?.accounts ?? []).map(group => ({
			...group,
			logo: providerLogos[group.key as LimitAccount['provider']]
		}))
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

{#snippet windowCell(account: LimitAccount, window: LimitWindow | null, scope = '')}
	{@const isExpired = window ? expired(window, now) : false}
	{@const elapsed = window ? elapsedPercent(window, now) : null}
	{@const used = window?.used_percent ?? null}
	{@const reset = widgetReset(window, now)}
	<div class="cell window-cell {account.provider}" class:expired={isExpired}>
		{#if window}
			<div class="cell-top">
				<span class="figure">
					{#if scope}<span class="scope-tag" title={window.label}>{scope}</span>{/if}
					{#if used == null}<span class="cell-dash" role="img" aria-label="Usage not reported"></span>{:else}<strong>{Math.round(used)}%</strong>{/if}
				</span>
				<span class="reset" class:quiet={reset.text === '—'} title={reset.description} aria-label={reset.description}>{reset.text}</span>
			</div>
			<div class="track" role="img" aria-label="{account.email}, {account.provider}, {window.label}: {used == null ? 'usage unavailable' : `${used}% used`}{elapsed == null ? '' : `; ${Math.round(elapsed)}% of period elapsed`}{isExpired ? '; expired snapshot' : ''}">
				<span class="fill" style:width={`${barPercent(used)}%`}></span>
				{#if elapsed != null}<span class="pace" style:left={`${elapsed}%`} title="{Math.round(elapsed)}% of period elapsed"></span>{/if}
			</div>
		{:else}
			<span class="cell-dash" role="img" aria-label="Not reported" title="Not reported for this account"></span>
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
			<div class="table-scroll" style:--rows={sections.reduce((n, s) => n + s.accounts.length, 0)} style:--sections={sections.length}>
				<div class="table-head" aria-hidden="true">
					<span class="col-account">Account</span>
					<span>5-hour</span>
					<span>Weekly</span>
					<span><span class="wide-label">Scoped · Credits</span><span class="narrow-label">Other</span></span>
				</div>
				{#each sections as section (section.key)}
					<section class="provider-group" aria-label={section.label}>
						<h2 class="section-heading">
							<img class="section-logo" src={section.logo} width="16" height="16" alt="" aria-hidden="true" />
							<span>{section.label}</span>
							<small>{section.accounts.length} {section.accounts.length === 1 ? 'account' : 'accounts'}</small>
						</h2>
						{#each section.accounts as account (account.id)}
							{@const status = accountStatus(account, now)}
							{@const route = account.provider === 'codex' && account.workspace ? routing?.accounts.find(a => a.id === account.workspace) : undefined}
							{@const isPinned = !!route && routing?.pinned_account === route.id}
							{@const isNext = !!route && routing?.next_account === route.id}
							{@const color = accountColors[account.email.toLowerCase() || account.id]}
							{@const cells = widgetAccountCells(account)}
							<div class="account-row" class:up-next={isNext} style:--account-color={color} aria-label={account.email || 'Account identity unavailable'}>
								<div class="cell account-cell">
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
												<svg viewBox="0 0 16 16" width="10" height="10" aria-hidden="true"><path d="m5 2 6 0-1 4 2 3H4l2-3-1-4ZM8 9v5" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" /></svg>
												{isPinned ? 'Pinned' : 'Pin'}
											</button>
										{/if}
									</div>
									{#if status !== 'Up to date'}
										<span class="account-meta account-status" role="status" title={account.error || status}>{status}</span>
									{:else if isNext && !isPinned}
										<span class="account-meta up-next-label" title="Automatic routing would choose this account for a new Codex conversation">Up next</span>
									{/if}
								</div>

								{@render windowCell(account, cells.fiveHour)}
								{@render windowCell(account, cells.weekly)}

								{#if cells.extra}
									{@render windowCell(account, cells.extra, cells.extraLabel)}
								{:else if account.provider === 'codex'}
									<div class="cell extra-cell">
										<span class="scope-tag">Credits</span>
										{#if resetCreditInfo(account, now).count === 0}
											<span class="quiet" title="No Codex reset credits available">none</span>
										{:else}
											<ResetCredits {account} {now} compact />
										{/if}
									</div>
								{:else}
									<div class="cell extra-cell">
										{#if account.plan}<span class="plan-pill" title="Plan">{account.plan}</span>{:else}<span class="cell-dash" role="img" aria-label="Not reported"></span>{/if}
									</div>
								{/if}
							</div>
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
	.widget-shell { --request-ticker-height: clamp(40px, 6svh, 56px); --page-gap: clamp(6px, 1.2svh, 14px); }
	.widget-page { padding: var(--page-gap) clamp(6px, 1.2vw, 20px); padding-bottom: calc(var(--request-ticker-height) + var(--page-gap) + env(safe-area-inset-bottom)); }
	.widget {
		--widget-bg: #fff;
		--widget-head: #f8fafc;
		--widget-track: #e6ebf0;
		--widget-border: #dce2e9;
		--widget-rule: #edf0f4;
		--widget-text: #26313e;
		--widget-muted: #68778a;
		--widget-quiet: #b3bdc9;
		--widget-warn: #b95030;
		--codex-color: #475569;
		--claude-color: #c0603e;
		--google-color: #2563eb;
		--facebook-color: #1877f2;
		/* One column template for the header and every row keeps cells aligned. */
		--cols: minmax(170px, 1.3fr) repeat(3, minmax(0, 1fr));
		--col-gap: clamp(12px, 2.2vw, 34px);
		--pad-x: clamp(10px, 1.6vw, 22px);
		/* The card fills the window above the ticker; the table scrolls when it is short. */
		height: calc(100svh - var(--request-ticker-height) - 2 * var(--page-gap) - env(safe-area-inset-bottom));
		min-height: 220px;
		max-width: 1600px;
		margin: 0 auto;
		display: flex;
		flex-direction: column;
		container: widget / inline-size;
		background: var(--widget-bg);
		color: var(--widget-text);
		border: 1px solid var(--widget-border);
		border-radius: 12px;
		overflow: hidden;
		box-shadow: 0 2px 8px #0000000d;
		font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
		font-size: 13px;
		font-variant-numeric: tabular-nums;
	}
	header {
		display: flex;
		flex-shrink: 0;
		justify-content: space-between;
		align-items: center;
		flex-wrap: wrap;
		gap: 4px 16px;
		padding: clamp(6px, 1.1svh, 12px) var(--pad-x);
		background: var(--widget-head);
		border-bottom: 1px solid var(--widget-border);
	}
	h1 { margin: 0; font-size: 13px; text-transform: uppercase; letter-spacing: .18em; }
	.dashboard-link { color: var(--widget-text); text-decoration: underline; text-underline-offset: 3px; white-space: nowrap; }
	.dashboard-link:focus-visible { outline: 2px solid var(--account-color, #3c82eb); outline-offset: 2px; }
	.summary { display: flex; align-items: center; flex-wrap: wrap; gap: 4px 10px; color: var(--widget-muted); font-size: 12px; }
	.summary > :not(:first-child)::before { content: '·'; display: inline-block; margin-right: 10px; color: var(--widget-muted); }
	button { border: 0; border-radius: 4px; background: transparent; padding: 1px 0; color: inherit; font: inherit; cursor: pointer; }
	button:hover:not(:disabled) { color: var(--widget-text); }
	button:disabled { opacity: .65; cursor: default; }
	kbd { font: inherit; }

	.table-scroll { container: table / size; flex: 1 1 auto; min-height: 0; overflow: auto; overscroll-behavior: contain; display: flex; flex-direction: column; }
	.table-head, .account-row {
		display: grid;
		grid-template-columns: var(--cols);
		column-gap: var(--col-gap);
		padding-inline: calc(var(--pad-x) - 4px) var(--pad-x);
		border-left: 4px solid transparent;
	}
	.table-head {
		position: sticky;
		top: 0;
		z-index: 2;
		flex-shrink: 0;
		padding-block: 7px;
		background: var(--widget-head);
		border-bottom: 1px solid var(--widget-border);
		color: var(--widget-muted);
		font-size: 11px;
		letter-spacing: .1em;
		text-transform: uppercase;
		white-space: nowrap;
	}
	.table-head > span { overflow: hidden; text-overflow: ellipsis; }
	.narrow-label { display: none; }

	/* Rows sit directly in the scrolling column so they share spare height evenly. */
	.provider-group { display: contents; }
	.section-heading {
		flex-shrink: 0;
		margin: 0;
		display: flex;
		align-items: center;
		gap: 8px;
		padding: clamp(5px, .8svh, 9px) var(--pad-x);
		background: var(--widget-head);
		border-bottom: 1px solid var(--widget-border);
		font-size: 12px;
		font-weight: 600;
		letter-spacing: .04em;
	}
	.provider-group:not(:first-of-type) .section-heading { border-top: 1px solid var(--widget-border); margin-top: -1px; }
	.section-logo { display: block; width: 16px; height: 16px; border-radius: 3px; flex-shrink: 0; }
	.section-heading small { margin-left: auto; color: var(--widget-muted); font-weight: 400; font-size: 11px; letter-spacing: 0; }

	.account-row {
		/* --row-h approximates each row's share of the table height (the scroller is a
		   size container); --grow is how far past compact it is. Type, bars and gaps scale
		   with --grow, flex fills the remainder, and the cap stops a lone row ballooning. */
		--row-min: 56px;
		--row-max: 200px;
		--row-h: clamp(var(--row-min), calc((100cqh - 40px - var(--sections, 1) * 38px) / var(--rows, 1)), var(--row-max));
		--grow: calc(var(--row-h) - var(--row-min));
		--pct-cap: 3.4cqw;
		--reset-cap: 2.2cqw;
		flex: 1 0 auto;
		min-height: var(--row-h);
		max-height: var(--row-max);
		align-items: center;
		align-content: center;
		row-gap: calc(6px + var(--grow) * .04);
		padding-block: 8px;
		border-left-color: var(--account-color, var(--codex-color));
		border-bottom: 1px solid var(--widget-rule);
	}
	.account-row:last-child { border-bottom-color: var(--widget-border); }
	.account-row.up-next { background: color-mix(in srgb, var(--account-color) 7%, var(--widget-bg)); }

	.cell { min-width: 0; }
	.account-cell { display: flex; flex-direction: column; justify-content: center; gap: 3px; }
	.account-line { display: flex; align-items: center; gap: 8px; min-width: 0; font-size: clamp(12px, calc(12px + var(--grow) * .035), 14.5px); }
	.account-line :global(.nickname) { flex: 0 1 auto; flex-wrap: nowrap; min-width: 0; }
	.account-line :global(.nickname .label) { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; overflow-wrap: normal; }
	.account-meta { font-size: clamp(11px, calc(11px + var(--grow) * .015), 12px); line-height: 1.3; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; padding-left: 16px; }
	.account-status { color: var(--widget-warn); }
	.up-next-label { color: var(--widget-muted); }
	.up-next-label::before { content: ''; display: inline-block; width: 6px; height: 6px; margin-right: 6px; border-radius: 50%; background: var(--account-color); vertical-align: 1px; }

	.pin-control {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		padding: 2px 7px;
		border: 1px solid var(--widget-border);
		border-radius: 999px;
		color: var(--widget-muted);
		font-size: 11px;
		line-height: 1.2;
		white-space: nowrap;
		flex-shrink: 0;
	}
	.pin-control:focus-visible { outline: 2px solid var(--account-color, #3c82eb); outline-offset: 2px; }
	.pin-control.pinned {
		color: var(--widget-text);
		background: color-mix(in srgb, var(--account-color) 18%, var(--widget-bg));
		border-color: var(--account-color);
	}

	.window-cell { --fill: var(--codex-color); display: flex; flex-direction: column; gap: calc(5px + var(--grow) * .035); }
	.window-cell.claude { --fill: var(--claude-color); }
	.window-cell.agy { --fill: var(--google-color); }
	.window-cell.muse { --fill: var(--facebook-color); }
	.cell-top { display: flex; justify-content: space-between; align-items: baseline; gap: 6px; line-height: 1.1; min-width: 0; }
	.figure { display: inline-flex; align-items: baseline; gap: 6px; min-width: 0; white-space: nowrap; }
	.figure strong { font-size: clamp(13px, min(calc(14px + var(--grow) * .13), var(--pct-cap)), 27px); font-weight: 650; letter-spacing: -.01em; }
	.scope-tag { color: var(--widget-muted); font-size: clamp(10px, calc(10px + var(--grow) * .02), 12px); font-weight: 500; letter-spacing: .04em; text-transform: uppercase; overflow: hidden; text-overflow: ellipsis; }
	.reset { color: var(--widget-muted); font-size: clamp(11px, min(calc(11.5px + var(--grow) * .035), var(--reset-cap)), 16px); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; flex-shrink: 1; }
	.reset.quiet { color: var(--widget-quiet); }
	.track { position: relative; height: clamp(6px, calc(7px + var(--grow) * .075), 16px); border-radius: 999px; background: var(--widget-track); }
	.fill { display: block; height: 100%; border-radius: inherit; background: var(--fill); }
	.pace { position: absolute; top: -3px; bottom: -3px; width: 2px; transform: translateX(-1px); border-radius: 1px; background: var(--widget-text); box-shadow: 0 0 0 1.5px var(--widget-bg); }
	.expired .track { opacity: .4; }
	.expired .reset { color: var(--widget-warn); }

	.extra-cell { display: flex; align-items: center; flex-wrap: wrap; gap: 4px 8px; font-size: clamp(11px, min(calc(11.5px + var(--grow) * .025), var(--reset-cap)), 14px); }
	.extra-cell :global(.reset-credits) { gap: 4px; }
	.plan-pill { padding: 2px 9px; border: 1px solid var(--widget-border); border-radius: 999px; color: var(--widget-muted); font-size: 11px; text-transform: capitalize; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 100%; }
	.quiet { color: var(--widget-quiet); }
	.window-cell > .cell-dash { align-self: flex-start; margin-block: 6px; }
	.cell-dash { display: inline-block; flex-shrink: 0; align-self: center; width: 16px; height: 2px; border-radius: 1px; background: var(--widget-quiet); vertical-align: middle; }

	.notice { color: var(--widget-warn); font-size: 12px; padding: 6px var(--pad-x); margin: 0; border-bottom: 1px solid var(--widget-border); }
	.stale { color: var(--widget-warn); }
	.empty { padding: 24px var(--pad-x); color: var(--widget-muted); margin: 0; }

	/* Narrow windows: the account spans the row and its three usage cells sit below it. */
	@container widget (max-width: 600px) {
		.table-head, .account-row { --cols: repeat(3, minmax(0, 1fr)); --col-gap: 12px; }
		.col-account { display: none; }
		.wide-label { display: none; }
		.narrow-label { display: inline; }
		.account-row { --row-min: 80px; --row-max: 216px; --pct-cap: 5.4cqw; --reset-cap: 3.4cqw; }
		.account-cell { grid-column: 1 / -1; flex-direction: row; align-items: center; justify-content: space-between; gap: 8px; }
		.account-meta { padding-left: 0; flex-shrink: 0; }
	}
	@container widget (max-width: 420px) {
		.summary { font-size: 11px; gap: 3px 12px; }
		.summary > :not(:first-child)::before { content: none; }
		.table-head { font-size: 10px; letter-spacing: .06em; }
	}

	@media (prefers-color-scheme: dark) {
		.widget {
			--widget-bg: #0e1218;
			--widget-head: #131821;
			--widget-track: #232c38;
			--widget-border: #293341;
			--widget-rule: #1c232e;
			--widget-text: #e5e8ef;
			--widget-muted: #929eaf;
			--widget-quiet: #4a5566;
			--widget-warn: #df805d;
			--codex-color: #d5dbe4;
			--claude-color: #df805d;
			--google-color: #60a5fa;
			--facebook-color: #4599ff;
			box-shadow: none;
		}
	}
</style>
