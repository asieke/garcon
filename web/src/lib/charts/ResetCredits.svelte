<script lang="ts">
	import { resetCreditInfo, type LimitAccount } from '../limits';
	let { account, now, showProvider = false, compact = false }: { account: LimitAccount; now: number; showProvider?: boolean; compact?: boolean } = $props();
	const info = $derived(resetCreditInfo(account, now));
</script>

<div class="reset-credits" class:compact class:stale={info.stale} title={account.reset_credits?.error}>
	{#if showProvider}<span class="provider">{account.provider === 'codex' ? 'Codex' : 'Claude'}</span>{/if}
	{#if compact}
		{#if info.count == null}<span title="Reset credits unavailable" aria-label="Reset credits unavailable">—</span>
		{:else}
			{#each Array.from({ length: info.count }) as _, i}
				{@const expiry = info.expirations[i]}
				{@const description = expiry ? `Codex reset credit expires ${new Date(expiry).toLocaleString()}` : 'Codex reset credit; expiration not reported'}
				<span class="expiry" title={description} aria-label={description}>{expiry ? new Date(expiry).toLocaleDateString([], { month: 'short', day: 'numeric' }) : '?'}</span>
			{/each}
		{/if}
	{:else if info.count == null}<span>Reset credits unavailable</span>
	{:else}
		<strong>{info.count} reset {info.count === 1 ? 'credit' : 'credits'}</strong>
		{#each info.expirations as expiry}
			<span class="expiry" title={expiry ? new Date(expiry).toLocaleString() : undefined}>{expiry ? `Expires ${new Date(expiry).toLocaleDateString([], { month: 'short', day: 'numeric' })}` : 'Expiry not reported'}</span>
		{/each}
		{#if info.count > info.expirations.length}<span>Other expirations not reported</span>{/if}
	{/if}
	{#if info.stale}<span class="stale-label">Stale · awaiting refresh</span>{/if}
</div>

<style>
	.reset-credits { display: flex; flex-wrap: wrap; align-items: center; gap: 5px 10px; color: var(--text-muted); font-size: 11px; line-height: 1.5; }
	strong { color: var(--text-primary); font-size: inherit; font-weight: 600; }
	.provider { font-weight: 600; }
	.expiry { padding: 1px 6px; background: var(--nav-hover); border-radius: 4px; }
	.compact { gap: 4px; font-size: inherit; color: inherit; }
	.compact:empty { display: none; }
	.compact .expiry { white-space: nowrap; }
	.stale-label { color: var(--status-serious); }
</style>
