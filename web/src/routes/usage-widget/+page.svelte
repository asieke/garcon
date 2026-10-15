<script lang="ts">
	import { onMount } from 'svelte';
	import type { Routing } from '$lib/console/model';
	import RequestTicker from '$lib/RequestTicker.svelte';
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
		needsAttention,
		resetCreditInfo,
		widgetGroups,
		widgetProviderGroups,
		widgetReset,
		widgetShortLabel,
		widgetUsage,
		type LimitAccount,
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
	const staleAccounts = $derived(snapshot?.accounts.filter(a => needsAttention(a, now)).length ?? 0);
	const clock = (ms: number) => new Date(ms).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false });
	const day = (ms: number) => new Date(ms).toLocaleDateString([], { month: 'short', day: 'numeric' });
	// Text for a secondary window: "5h 15% · 3h 14m". Near-exhausted windows are flagged,
	// since a 5-hour or model limit can block work while the weekly bar looks fine.
	function extraText(window: LimitAccount['windows'][number]) {
		const used = window.used_percent;
		const reset = widgetReset(window, now);
		return {
			value: used == null ? '—' : `${Math.round(used)}%`,
			reset: reset.text === '—' ? '' : reset.text,
			description: `${window.label}: ${used == null ? 'usage not reported' : `${Number(used.toFixed(1))}% used`}; ${reset.description}`,
			hot: used != null && used >= 80
		};
	}
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

<svelte:head><title>Usage widget · Garcon</title><meta name="description" content="Live Codex, Claude, Google AGY, and Facebook Muse account usage at a glance." /></svelte:head>
<svelte:window onkeydown={shortcut} />

<div class="widget-shell">
<main class="widget-page">
	<section class="widget" aria-label="Account usage widget">
		<header>
			<h1>Usage</h1>
			<div class="summary">
				<span>{snapshot?.accounts.length ?? 0} profiles</span>
				<span class:stale title={checkedAt > 0 ? `Last provider check: ${new Date(checkedAt).toLocaleString()}` : undefined}>
					{busy ? 'refreshing…' : !snapshot ? 'connecting…' : checkedAt > 0 ? `checked ${clock(checkedAt)}` : 'awaiting usage'}
				</span>
				{#if staleAccounts}<span class="stale">{staleAccounts} {staleAccounts === 1 ? 'needs' : 'need'} attention</span>{/if}
				<button onclick={refresh} disabled={busy} aria-keyshortcuts="R"><kbd>R</kbd> refresh</button>
				<a class="dashboard-link" href="/" target="_blank" rel="noopener noreferrer" title="Open the main Garcon dashboard in your browser">Dashboard ↗</a>
			</div>
		</header>
		{#if error || snapshot?.error}<p class="notice" role="status">{error || snapshot?.error}</p>{/if}
		{#if routingError || routing?.error}<p class="notice" role="status">{routingError || routing?.error}</p>{/if}

		{#if sections.length}
			<div class="accounts">
				{#each sections as section (section.key)}
					<section class="provider-group" aria-label={section.label}>
						<h2>
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
							{@const usage = widgetUsage(account)}
							{@const primary = usage.primary}
							{@const credits = account.provider === 'codex' ? resetCreditInfo(account, now) : null}
							<article class="account {account.provider}" class:up-next={isNext} style:--account-color={color} aria-label={account.email || 'Account identity unavailable'}>
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
									{#if status !== 'Up to date' && status !== 'Last reading'}
										<span class="account-meta account-status" role="status" title={account.error || status}>{status}</span>
									{:else if isNext && !isPinned}
										<span class="account-meta up-next-label" title="Automatic routing would choose this account for a new Codex conversation">Up next</span>
									{/if}
								</div>

								{#if primary}
									{@const isExpired = expired(primary, now)}
									{@const elapsed = elapsedPercent(primary, now)}
									{@const used = primary.used_percent}
									{@const reset = widgetReset(primary, now)}
									<div class="bar-line" class:expired={isExpired}>
										<strong class="pct">{#if primary.label !== 'Weekly'}<span class="bar-label">{widgetShortLabel(primary.label)}</span>{/if}{used == null ? '—' : `${Math.round(used)}%`}</strong>
										<div class="track" role="img" aria-label="{account.email}, {primary.label}: {used == null ? 'usage unavailable' : `${used}% used`}{elapsed == null ? '' : `; ${Math.round(elapsed)}% of period elapsed`}{isExpired ? '; expired snapshot' : ''}">
											<span class="fill" style:width={`${barPercent(used)}%`}></span>
											{#if elapsed != null}<span class="pace" style:left={`${elapsed}%`} title="{Math.round(elapsed)}% of the week elapsed"></span>{/if}
										</div>
										<span class="reset" class:quiet={reset.text === '—'} title={reset.description}>{reset.text}</span>
									</div>
								{:else}
									<p class="bar-line empty-line" title={account.error}>{account.error || 'No usage reported yet'}</p>
								{/if}

								{#if usage.extras.length || credits?.count || (account.status === 'idle' && account.observed_at)}
									<p class="extras">
										{#each usage.extras as extra (extra.window.id)}
											{@const text = extraText(extra.window)}
											<span class="extra" class:hot={text.hot} title={text.description}><span class="extra-label">{extra.label}</span> {text.value}{#if text.reset}<span class="extra-reset">{` · ${text.reset}`}</span>{/if}</span>
										{/each}
										{#if credits?.count}
											<span class="extra" class:quiet={credits.stale} title={credits.expirations.map(e => e ? `Reset credit expires ${new Date(e).toLocaleString()}` : 'Reset credit; expiration not reported').join('\n')}><span class="extra-label">{credits.count === 1 ? 'credit' : 'credits'}</span> {credits.expirations.length ? credits.expirations.map(e => e ? day(e) : '?').join(' · ') : credits.count}</span>
										{/if}
										{#if account.status === 'idle' && account.observed_at}
											<span class="extra quiet" title={account.error}>as of {now - account.observed_at > 86_400_000 ? day(account.observed_at) : clock(account.observed_at)}</span>
										{/if}
									</p>
								{/if}
							</article>
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
		--widget-track: #e4e9ef;
		--widget-border: #dce2e9;
		--widget-rule: #edf0f4;
		--widget-text: #26313e;
		--widget-muted: #68778a;
		--widget-quiet: #a3aebb;
		--widget-warn: #b95030;
		--codex-color: #475569;
		--claude-color: #c0603e;
		--google-color: #2563eb;
		--facebook-color: #1877f2;
		--pad-x: clamp(10px, 1.6vw, 22px);
		/* The card fills the window above the ticker; accounts scroll only when it is short. */
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

	.accounts { flex: 1 1 auto; min-height: 0; overflow: auto; overscroll-behavior: contain; display: flex; flex-direction: column; }
	/* Account blocks sit directly in the scrolling column so they share spare height. */
	.provider-group { display: contents; }
	h2 {
		flex-shrink: 0;
		margin: 0;
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 5px var(--pad-x);
		background: var(--widget-head);
		border-bottom: 1px solid var(--widget-border);
		font-size: 12px;
		font-weight: 600;
		letter-spacing: .04em;
	}
	.provider-group:not(:first-child) h2 { border-top: 1px solid var(--widget-border); margin-top: -1px; }
	.section-logo { display: block; width: 16px; height: 16px; border-radius: 3px; flex-shrink: 0; }
	h2 small { margin-left: auto; color: var(--widget-muted); font-weight: 400; font-size: 11px; letter-spacing: 0; }

	.account {
		--fill: var(--codex-color);
		flex: 1 0 auto;
		max-height: 120px;
		display: flex;
		flex-direction: column;
		justify-content: center;
		gap: 3px;
		padding: 5px var(--pad-x) 5px calc(var(--pad-x) - 4px);
		border-left: 4px solid var(--account-color, var(--codex-color));
		border-bottom: 1px solid var(--widget-rule);
	}
	.account.claude { --fill: var(--claude-color); }
	.account.agy { --fill: var(--google-color); }
	.account.muse { --fill: var(--facebook-color); }
	.account.up-next { background: color-mix(in srgb, var(--account-color) 7%, var(--widget-bg)); }

	.account-line { display: flex; align-items: center; gap: 8px; min-width: 0; font-size: 13px; line-height: 1.25; }
	.account-line :global(.nickname) { flex: 0 1 auto; flex-wrap: nowrap; min-width: 0; }
	.account-line :global(.nickname .label) { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; overflow-wrap: normal; }
	.account-meta { margin-left: auto; font-size: 11px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; flex-shrink: 1; min-width: 0; }
	.account-status { color: var(--widget-warn); }
	.up-next-label { color: var(--widget-muted); }
	.up-next-label::before { content: ''; display: inline-block; width: 6px; height: 6px; margin-right: 6px; border-radius: 50%; background: var(--account-color); vertical-align: 1px; }

	.pin-control {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		padding: 1px 7px;
		border: 1px solid var(--widget-border);
		border-radius: 999px;
		color: var(--widget-muted);
		font-size: 11px;
		line-height: 1.2;
		white-space: nowrap;
		flex-shrink: 0;
	}
	.pin-control:focus-visible { outline: 2px solid var(--account-color, #3c82eb); outline-offset: 2px; }
	.pin-control.pinned { color: var(--widget-text); background: color-mix(in srgb, var(--account-color) 18%, var(--widget-bg)); border-color: var(--account-color); }

	.bar-line { display: grid; grid-template-columns: 4.5ch minmax(0, 1fr) 7.5ch; align-items: center; gap: 10px; margin: 0; padding-left: 16px; line-height: 1.25; }
	.pct { font-size: 14px; font-weight: 650; white-space: nowrap; }
	.bar-label { margin-right: 4px; color: var(--widget-muted); font-size: 10px; font-weight: 500; letter-spacing: .04em; text-transform: uppercase; }
	.track { position: relative; height: 9px; border-radius: 999px; background: var(--widget-track); }
	.fill { display: block; height: 100%; border-radius: inherit; background: var(--fill); }
	.pace { position: absolute; top: -3px; bottom: -3px; width: 2px; transform: translateX(-1px); border-radius: 1px; background: var(--widget-text); box-shadow: 0 0 0 1.5px var(--widget-bg); }
	.reset { color: var(--widget-muted); font-size: 12px; text-align: right; white-space: nowrap; }
	.reset.quiet { color: var(--widget-quiet); }
	.expired .track { opacity: .4; }
	.expired .reset { color: var(--widget-warn); }
	.empty-line { display: block; color: var(--widget-muted); font-size: 12px; line-height: 1.35; }

	.extras { display: flex; flex-wrap: wrap; gap: 0 14px; margin: 0; padding-left: 16px; color: var(--widget-muted); font-size: 12px; line-height: 1.3; }
	.extra { white-space: nowrap; }
	.extra-label { color: var(--widget-quiet); }
	.extra.hot, .extra.hot .extra-label { color: var(--widget-warn); }
	.quiet, .extra.quiet { color: var(--widget-quiet); }

	.notice { color: var(--widget-warn); font-size: 12px; padding: 6px var(--pad-x); margin: 0; border-bottom: 1px solid var(--widget-border); }
	.stale { color: var(--widget-warn); }
	.empty { padding: 24px var(--pad-x); color: var(--widget-muted); margin: 0; }

	/* Wide windows: name, bar and the other limits share one line per account. */
	@container widget (min-width: 720px) {
		.account { display: grid; grid-template-columns: minmax(180px, 1fr) minmax(220px, 1.6fr) minmax(200px, 1.4fr); align-items: center; column-gap: clamp(16px, 2.4vw, 36px); max-height: 96px; }
		.bar-line, .extras { padding-left: 0; }
		.account-line { flex-wrap: wrap; row-gap: 2px; }
		.account-meta { flex-basis: 100%; margin-left: 0; padding-left: 16px; }
		.pct { font-size: 15px; }
		.track { height: 10px; }
	}
	@container widget (max-width: 420px) {
		.summary { font-size: 11px; gap: 3px 12px; }
		.summary > :not(:first-child)::before { content: none; }
		.extras { gap: 2px 10px; }
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
			--widget-quiet: #5d6878;
			--widget-warn: #df805d;
			--codex-color: #d5dbe4;
			--claude-color: #df805d;
			--google-color: #60a5fa;
			--facebook-color: #4599ff;
			box-shadow: none;
		}
	}
</style>
