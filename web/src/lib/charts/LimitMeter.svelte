<script lang="ts">
	import { barPercent, countdown, elapsedPercent, expired, type LimitWindow } from '../limits';
	let { window, now, compact = false }: { window: LimitWindow; now: number; compact?: boolean } = $props();
	const elapsed = $derived(elapsedPercent(window, now));
	const isExpired = $derived(expired(window, now));
	const used = $derived(window.used_percent);
	const percentage = $derived(used == null ? 'Unavailable' : `${Number(used.toFixed(1))}% used`);
</script>

<div class="meter" class:expired={isExpired}>
	<div class="label-row"><span>{window.label}</span><strong>{percentage}</strong></div>
	<div class="track" role="img" aria-label="{window.label}: {percentage}{elapsed == null ? '' : `; ${Math.round(elapsed)}% of period elapsed`}{isExpired ? '; expired snapshot' : ''}">
		<div class="fill" class:full={used != null && used >= 100} style:width={`${barPercent(used)}%`}></div>
		{#if elapsed != null}<span class="elapsed" style:left={`${elapsed}%`} title="{Math.round(elapsed)}% of period elapsed — even-use reference"></span>{/if}
	</div>
	<div class="detail"><span>{countdown(window.resets_at, now)}</span>{#if elapsed != null}<span>{Math.round(elapsed)}% of period elapsed</span>{/if}</div>
	{#if !compact && window.resets_at > 0}<div class="date">{new Date(window.resets_at).toLocaleString([], { weekday: 'short', month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit', timeZoneName: 'short' })}{#if isExpired} · Last reported usage; awaiting refresh{/if}</div>{/if}
</div>

<style>
	.meter { display: grid; gap: 7px; }
	.label-row, .detail { display: flex; justify-content: space-between; gap: 12px; }
	.label-row { font-size: 12px; color: var(--text-secondary); }
	strong { color: var(--text-primary); font-variant-numeric: tabular-nums; font-size: 13px; }
	.track { position: relative; height: 12px; background: var(--grid); border-radius: 4px; }
	.fill { height: 100%; border-radius: 4px; background: var(--accent); }
	.fill.full { background: var(--status-critical); }
	.elapsed { position: absolute; width: 2px; top: -3px; bottom: -3px; background: var(--text-primary); transform: translateX(-1px); box-shadow: 0 0 0 1px var(--surface); }
	.detail, .date { font-size: 10px; color: var(--text-muted); }
	.detail { flex-wrap: wrap; gap: 4px 12px; }
	.expired .track { opacity: .4; }
	.expired strong { color: var(--text-muted); }
</style>
