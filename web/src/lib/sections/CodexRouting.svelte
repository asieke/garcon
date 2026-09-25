<script lang="ts">
	import { onMount } from 'svelte';
	type Account = { id: string; email: string; plan: string; enrolled: boolean; status: string; remaining_percent: number | null; active_requests: number; conversations: number };
	type Status = { enabled: boolean; accounts: Account[]; error?: string };
	let status = $state<Status | null>(null);
	let error = $state('');
	let saving = $state(false);
	async function load() {
		try {
			const response = await fetch('/api/routing/codex');
			if (!response.ok) throw new Error();
			status = await response.json(); error = '';
		} catch { error = 'Could not load Codex routing status.'; }
	}
	async function toggle() {
		saving = true;
		try {
			const response = await fetch('/api/routing/codex', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ enabled: !status?.enabled }) });
			if (!response.ok) throw new Error();
			status = await response.json(); error = '';
		} catch { error = 'Could not change Codex routing. Check that saved Codex logins are available.'; }
		finally { saving = false; }
	}
	onMount(() => { load(); const timer = setInterval(load, 10_000); return () => clearInterval(timer); });
</script>

<section aria-label="Codex account routing" class="routing">
	<div class="heading"><div><h2>Codex account routing</h2><p>{status?.enabled ? 'New conversations choose the eligible account with the most available usage. Each conversation keeps its account.' : 'Enable to route Codex conversations across the saved accounts shown below.'}</p></div><button onclick={toggle} disabled={saving || !status}>{saving ? 'Saving…' : status?.enabled ? 'Disable routing' : 'Enable these accounts'}</button></div>
	{#if error || status?.error}<p role="status">{error || status?.error}</p>{/if}
	{#if status}
		<div class="accounts">
			{#each status.accounts as account (account.id)}
				<article><strong>{account.email || 'Saved login missing'}</strong><span>{account.plan}</span><p>{status.enabled ? account.status : 'Routing off'}{account.enrolled && status.enabled ? ' · Enrolled' : ''}</p><p>{account.remaining_percent == null ? 'Available usage unknown' : `${account.remaining_percent}% remaining in the tightest usage window`}</p><small>{account.active_requests} active requests · {account.conversations} assigned conversations</small></article>
			{:else}<p>No saved Codex OAuth accounts found.</p>{/each}
		</div>
		<p class="note">Uses each account’s reported quota and model access. Percentages compare remaining allowance, not absolute token capacity. Expired logins require renewal; exhausted conversations keep their account. Newly discovered accounts join only when routing is enabled again.</p>
	{/if}
</section>

<style>
	.routing { border: 1px solid var(--border, #8884); border-radius: 12px; padding: 20px; margin-bottom: 28px; }
	.heading { display: flex; justify-content: space-between; gap: 20px; align-items: flex-start; }
	h2 { font-size: 16px; margin: 0 0 8px; } p { font-size: 12px; line-height: 1.5; margin: 8px 0; }
	button { flex-shrink: 0; cursor: pointer; border: 1px solid var(--border, #8884); background: transparent; color: inherit; padding: 8px 12px; border-radius: 6px; }
	button:disabled { opacity: .5; cursor: default; }
	.accounts { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 12px; margin-top: 16px; }
	article { border: 1px solid var(--border, #8884); border-radius: 8px; padding: 14px; overflow-wrap: anywhere; }
	strong { display: block; font-size: 13px; } span, small, .note { opacity: .7; font-size: 11px; }
	@media (max-width: 600px) { .heading { flex-direction: column; } }
</style>
