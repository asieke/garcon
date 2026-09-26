<script lang="ts">
  import type { LimitAccount } from '$lib/limits';
  import { orderedUsageWindows, usageFreshness, usageWindowDisplay, usageWindowLabel } from './account-usage';
  let { quota, provider, now }: { quota?: LimitAccount; provider: string; now: number } = $props();
  const windows = $derived(orderedUsageWindows(quota?.windows ?? []));
  const freshness = $derived(usageFreshness(quota, now));
</script>

<div class="account-usage-summary" aria-label={`${provider === 'claude' ? 'Claude' : provider === 'codex' ? 'Codex' : provider} usage limits`}>
  {#if windows.length}
    <div class="usage-windows">
      {#each windows as window (window.id)}
        {@const display = usageWindowDisplay(window, now)}
        {@const label = usageWindowLabel(window, provider)}
        <div class="usage-window" class:stale={freshness.stale || display.ended}>
          <span class="window-label">{label}</span>
          <div class="window-value"><strong>{display.percent}</strong><span>{display.known ? 'used' : 'not reported'}</span>{#if display.exhausted && !display.ended}<span class="exhausted">Limit reached</span>{/if}</div>
          <div class="usage-track" role="img" aria-label={`${label}: ${display.known ? `${display.percent} used` : 'usage not reported'}${freshness.stale || display.ended ? ' (last reported)' : ''}`}>
            {#if display.known}<span class:exhausted={display.exhausted} style:width={`${display.fill}%`}></span>{/if}
          </div>
          <span class="window-reset" title={display.resetDescription}>{display.reset}</span>
          {#if display.ended}<span class="last-reported">Last reported usage</span>{/if}
        </div>
      {/each}
    </div>
    <span class="usage-freshness" class:stale={freshness.stale} title={freshness.detail}>{freshness.text}{#if freshness.stale} · last reported values{/if}</span>
  {:else}
    <div class="usage-missing"><strong>Usage unavailable</strong><span>{provider === 'openrouter' ? 'API usage is not reported here.' : quota?.error || 'No limits reported for this account.'}</span></div>
    {#if quota}<span class="usage-freshness stale" title={freshness.detail}>{freshness.text}</span>{/if}
  {/if}
</div>

<style>
  .account-usage-summary { min-width: 0; width: 100%; }
  .usage-windows { display: grid; grid-template-columns: repeat(auto-fit, minmax(130px, 1fr)); gap: 12px 18px; }
  .usage-window { min-width: 0; }
  .usage-window:only-child { max-width: 200px; }
  .window-label { display: block; font-size: 10px; color: var(--desk-muted); margin-bottom: 7px; overflow-wrap: anywhere; }
  .window-value { display: flex; align-items: baseline; flex-wrap: wrap; gap: 5px; font-size: 10px; color: var(--desk-muted); }
  .window-value strong { font: 500 18px var(--desk-mono); letter-spacing: -.04em; color: var(--desk-ink); }
  .usage-track { height: 4px; margin: 8px 0; background: var(--desk-hover); border-radius: 2px; overflow: hidden; }
  .usage-track > span { display: block; height: 100%; background: var(--desk-fill); }
  .window-reset, .last-reported { display: block; font: 10px/1.5 var(--desk-mono); color: var(--desk-muted); }
  .usage-freshness { display: block; margin-top: 10px; font-size: 9px; color: var(--desk-muted); }
  .usage-freshness.stale, .last-reported, .exhausted { color: var(--desk-ink); }
  .usage-window.stale .usage-track { opacity: .45; }
  .usage-window.stale .usage-track > span { background: repeating-linear-gradient(90deg,var(--desk-fill) 0 3px,transparent 3px 5px); }
  .usage-track > span.exhausted { background: #be7044; }
  .usage-missing { display: grid; gap: 4px; color: var(--desk-muted); font-size: 11px; }
  .usage-missing strong { color: var(--desk-ink); font-weight: 500; }
</style>
