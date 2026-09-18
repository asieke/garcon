<script lang="ts">
	import LimitMeter from '../charts/LimitMeter.svelte';
	import ResetCredits from '../charts/ResetCredits.svelte';
	import { accountStatus, overviewWindow, type LimitsSnapshot } from '../limits';
	let { snapshot, now, error = '', refreshing = false, onrefresh, compact = false }: {
		snapshot: LimitsSnapshot | null; now: number; error?: string; refreshing?: boolean; onrefresh: () => void; compact?: boolean;
	} = $props();
	const accounts = $derived(snapshot?.accounts ?? []);
	const busy = $derived(refreshing || snapshot?.refreshing);
	function duplicateEmail(provider: string, email: string) { return accounts.filter(a => a.provider === provider && a.email === email).length > 1; }
</script>

<section class="limits" class:compact aria-label="Provider account limits">
	<div class="toolbar">
		<div>{#if compact}<h2>Account limits</h2>{/if}<p>Account-wide subscription usage · refreshes every 5 minutes</p></div>
		<div class="actions">{#if compact}<a href="?view=limits">All limits →</a>{:else}<a href="/usage-widget/">Compact widget ↗</a>{/if}<button onclick={onrefresh} disabled={busy}>{busy ? 'Refreshing…' : 'Refresh'}</button></div>
	</div>
	{#if error || snapshot?.error}<p class="notice" role="status">{error || snapshot?.error}</p>{/if}
	{#if !snapshot && !error}<p class="empty" role="status">Loading account limits…</p>
	{:else if accounts.length === 0}
		<p class="empty" role="status">{busy ? 'Discovering local logins…' : 'No subscription logins found. Sign in with a local Codex or Claude profile, then refresh.'}</p>
	{:else}
		{#each ['codex', 'claude'] as provider}
			{@const providerAccounts = accounts.filter(a => a.provider === provider).sort((a, b) => a.email.localeCompare(b.email))}
			{#if providerAccounts.length}
				<h3>{provider === 'codex' ? 'Codex' : 'Claude'}</h3>
				<div class="cards">
					{#each providerAccounts as account (account.id)}
						{@const status = accountStatus(account, now)}
						{@const summary = overviewWindow(account)}
						<article class="card">
							<div class="head"><h4>{account.email || 'Account identity unavailable'}</h4>{#if account.plan}<span class="plan">{account.plan}</span>{/if}</div>
							{#if account.workspace && duplicateEmail(provider, account.email)}<p class="workspace">{account.workspace}</p>{/if}
							<div class="meters">
								{#each compact ? (summary ? [summary] : []) : account.windows as window (window.id)}<LimitMeter {window} {now} {compact} />{:else}<p class="empty">No subscription limits reported.</p>{/each}
							</div>
							{#if account.provider === 'codex'}<div class="credits"><ResetCredits {account} {now} /></div>{/if}
							<div class="status" class:attention={status !== 'Up to date'}><span>{status}</span><span>{account.fetched_at ? `Updated ${new Date(account.fetched_at).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })}` : 'No snapshot yet'}</span></div>
							{#if account.error && account.error !== status}<p class="error">{account.error}</p>{/if}
						</article>
					{/each}
				</div>
			{/if}
		{/each}
		<p class="legend"><span aria-hidden="true">│</span> Marker = elapsed time in the provider’s reset period, an even-use reference. Percentages come from the provider and include usage outside Garcon.</p>
	{/if}
</section>

<style>
	.limits { margin-bottom: 28px; }
	.toolbar, .actions, .head, .status { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
	.toolbar { margin-bottom: 20px; flex-wrap: wrap; }
	h2 { font-size: 15px; margin: 0 0 6px; }
	.toolbar p, .empty { font-size: 12px; color: var(--text-secondary); margin: 0; }
	.actions a { font-size: 12px; color: var(--accent); white-space: nowrap; }
	button { font-size: 12px; color: var(--text-primary); background: var(--surface); border: 1px solid var(--border); border-radius: 6px; padding: 7px 12px; line-height: 1.3; }
	button:hover:not(:disabled) { background: var(--nav-hover); }
	button:disabled { opacity: .6; }
	h3 { font-size: 12px; margin: 22px 0 10px; color: var(--text-secondary); }
	.cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 290px), 1fr)); gap: 14px; }
	.card { background: var(--surface); border: 1px solid var(--border); border-radius: 10px; padding: 18px; min-width: 0; }
	h4 { font-size: 13px; font-weight: 600; overflow-wrap: anywhere; margin: 0; }
	.plan { font-size: 10px; color: var(--text-muted); flex-shrink: 0; }
	.workspace { font-size: 11px; color: var(--text-muted); overflow-wrap: anywhere; margin: 6px 0 0; }
	.meters { display: grid; gap: 24px; margin: 22px 0; }
	.status { font-size: 10px; color: var(--text-muted); flex-wrap: wrap; }
	.credits { margin-bottom: 14px; }
	.attention, .error { color: var(--status-critical); }
	.error { font-size: 11px; margin: 8px 0 0; }
	.legend { color: var(--text-muted); font-size: 11px; line-height: 1.6; margin: 18px 0 0; }
	.legend span { color: var(--text-primary); font-weight: 700; }
	.compact .card { padding: 14px; }
	.compact .meters { margin: 16px 0; }
</style>
