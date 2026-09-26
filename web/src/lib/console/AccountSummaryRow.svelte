<script lang="ts">
  import AccountUsage from './AccountUsage.svelte';
  import CodexLogo from './CodexLogo.svelte';
  import type { UsageAccount } from './account-usage';
  import claudeLogo from '$lib/assets/providers/claude-app.png';
  let { account, now }: { account: UsageAccount; now: number } = $props();
</script>

<div class="account-summary-row" data-account-id={account.key}>
  <div class="summary-identity">
    {#if account.provider === 'codex'}<CodexLogo size={27}/>{:else}<img src={claudeLogo} width="27" height="27" alt=""/>{/if}
    <div>
      <strong>{account.email || 'Account identity unavailable'}</strong>
      <span>{account.provider === 'codex' ? 'Codex' : 'Claude'} OAuth{#if account.plan} · {account.plan}{/if}</span>
      <small>{account.detail}</small>
    </div>
  </div>
  <AccountUsage quota={account.quota} provider={account.provider} {now}/>
</div>

<style>
  .account-summary-row { display: grid; grid-template-columns: minmax(180px, .8fr) minmax(0, 1.5fr); align-items: center; gap: 28px; padding: 22px; border-bottom: 1px solid var(--desk-line); }
  .account-summary-row:last-child { border-bottom: 0; }
  .summary-identity { display: flex; align-items: center; gap: 12px; min-width: 0; }
  .summary-identity > div { min-width: 0; }
  .summary-identity img { flex-shrink: 0; }
  .summary-identity strong { display: block; font-size: 12px; overflow-wrap: anywhere; }
  .summary-identity span { display: block; color: var(--desk-muted); font-size: 10px; margin-top: 5px; text-transform: capitalize; }
  .summary-identity small { display: block; color: var(--desk-muted); font: 10px var(--desk-mono); margin-top: 5px; overflow-wrap: anywhere; }
  @media(max-width: 850px) { .account-summary-row { grid-template-columns: 1fr; gap: 16px; padding: 18px; } }
</style>
