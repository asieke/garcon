<script lang="ts">
	import { onMount } from 'svelte';
	import { RequestQueue, requestProvider, requestTokens, requestMachine, type RecentRequest } from '$lib/ticker';
	import { n, exact } from '$lib/format';

	let { accountColors = {} }: { accountColors?: Record<string, string> } = $props();
	let items = $state<{ key: number; row: RecentRequest; shownAt: number; leadingSpace: number }[]>([]);
	let connected = $state(false);
	let totalRequests = $state<number | null>(null);
	let reducedMotion = $state(false);
	let lane = $state<HTMLDivElement>();
	let belt = $state<HTMLDivElement>();

	onMount(() => {
		const queue = new RequestQueue();
		const controller = new AbortController();
		const media = matchMedia('(prefers-reduced-motion: reduce)');
		const motion = () => { reducedMotion = media.matches; };
		motion(); media.addEventListener('change', motion);
		let serial = 0, offset = 0, frame = 0, previous = 0;
		let poll: ReturnType<typeof setTimeout>;
		const append = () => {
			const row = queue.next();
			if (row) {
				let leadingSpace = 0;
				if (!items.length) {
					// A quiet ticker's next request enters from the right, so it gets
					// a full pass across the screen instead of disappearing at the left.
					offset = lane?.clientWidth ?? 0;
					if (belt) belt.style.transform = `translateX(${offset}px)`;
				} else {
					// Sparse arrivals also enter at the right edge, leaving an idle
					// gap rather than popping into the middle behind an older chip.
					leadingSpace = Math.max(0, (lane?.clientWidth ?? 0) - ((belt?.scrollWidth ?? 0) + offset + 10));
				}
				items = [...items, { key: serial++, row, shownAt: performance.now(), leadingSpace }];
			}
		};
		async function load() {
			try {
				const res = await fetch('/api/usage/recent', { signal: controller.signal });
				if (!res.ok) throw new Error();
				const feed = await res.json();
				// Accept the previous response shape while a dev backend is restarting.
				const rows: RecentRequest[] = Array.isArray(feed) ? feed : feed.requests;
				if (controller.signal.aborted) return;
				if (!Array.isArray(feed)) totalRequests = feed.total_requests;
				if (queue.update(rows)) { items = []; offset = 0; }
				if (reducedMotion) {
					let row: RecentRequest | undefined;
					while ((row = queue.next())) items = [...items, { key: serial++, row, shownAt: performance.now(), leadingSpace: 0 }].slice(-8);
				}
				else if (!items.length) append();
				connected = true;
			} catch { if (!controller.signal.aborted) connected = false; }
			finally { if (!controller.signal.aborted) poll = setTimeout(load, 2000); }
		}
		function animate(time: number) {
			const elapsed = previous ? Math.min(time - previous, 64) : 0;
			previous = time;
			if (belt && lane && items.length && !reducedMotion) {
				// Keep only the visible belt plus one incoming chip, in arrival order.
				if (belt.scrollWidth + offset < lane.clientWidth + 360 && items.length < 16) append();
				offset -= elapsed * .045;
				const width = ((belt.firstElementChild as HTMLElement)?.offsetWidth ?? 0) + items[0].leadingSpace;
				if (width && offset <= -(width + 10)) {
					offset += width + 10; items = items.slice(1);
					if (!items.length) offset = 0;
				}
				belt.style.transform = `translateX(${offset}px)`;
			} else if (belt && reducedMotion) {
				belt.style.transform = '';
				// Static chips also clear during quiet periods instead of becoming history.
				if (items.length && time - items[0].shownAt >= 12_000) {
					items = items.filter(item => time - item.shownAt < 12_000);
				}
			}
			frame = requestAnimationFrame(animate);
		}
		load(); frame = requestAnimationFrame(animate);
		return () => { controller.abort(); clearTimeout(poll); cancelAnimationFrame(frame); media.removeEventListener('change', motion); };
	});
</script>

<footer class="request-ticker" aria-label="New LLM requests across machines">
	<div class="ticker-lane" class:still={reducedMotion} bind:this={lane} role="region" aria-label="Request chips">
		<div class="ticker-belt" bind:this={belt} role="list">
			{#each items as item (item.key)}
				{@const provider = requestProvider(item.row)}
				{@const tokens = requestTokens(item.row)}
				<span class="request-chip" class:failed={item.row.status >= 400} style:margin-left={`${reducedMotion ? 0 : item.leadingSpace}px`} style:--chip-color={accountColors[item.row.account.toLowerCase()] || provider.color} role="listitem" title="{requestMachine(item.row)} · {new Date(item.row.time).toLocaleString()} · {item.row.model} · {exact(tokens)} total tokens (input, cache, output) · HTTP {item.row.status}"><span class="account">{item.row.account || 'Unknown account'}</span><span class="tool" style:--provider-color={provider.color}>{provider.label}</span><strong>{n(tokens).toLowerCase()}</strong><span class="machine">{requestMachine(item.row)}</span>{#if item.row.status >= 400}<span class="failure">{item.row.status}</span>{/if}</span>
			{/each}
		</div>
	</div>
	<div class="total-requests" class:stale={!connected} title={connected ? 'All recorded requests across this machine and synced machines' : 'Waiting for the latest all-machine request count'}><span>Total requests</span><strong>{totalRequests == null ? '—' : exact(totalRequests)}</strong></div>
</footer>

<style>
	.request-ticker { position: fixed; z-index: 30; inset: auto 0 0; display: flex; align-items: center; gap: 12px; height: calc(var(--request-ticker-height, 62px) + env(safe-area-inset-bottom)); padding: 3px 14px calc(3px + env(safe-area-inset-bottom)); background: var(--surface); border-top: 1px solid var(--border); box-shadow: 0 -3px 18px #0000000c; font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-variant-numeric: tabular-nums; }
	.ticker-lane { overflow: hidden; min-width: 0; flex: 1; height: 32px; display: flex; align-items: center; mask-image: linear-gradient(to right, transparent, #000 8px, #000 calc(100% - 12px), transparent); }
	.ticker-belt { display: flex; gap: 10px; flex: none; width: max-content; will-change: transform; }
	.request-chip { flex: none; display: inline-flex; align-items: center; white-space: nowrap; height: 32px; gap: 0; border: 1px solid color-mix(in srgb, var(--chip-color) 35%, var(--border)); border-left: 3px solid var(--chip-color); border-radius: 5px; background: color-mix(in srgb, var(--chip-color) 8%, var(--surface)); color: var(--text-primary); padding: 0 10px; font-size: 12px; }
	.request-chip > :not(:first-child) { border-left: 1px solid color-mix(in srgb, var(--chip-color) 30%, transparent); margin-left: 10px; padding-left: 10px; }
	.tool { color: color-mix(in srgb, var(--provider-color) 65%, var(--text-primary)); }
	.failure { color: var(--status-critical); }
	.machine { max-width: 150px; overflow: hidden; text-overflow: ellipsis; color: var(--text-muted); }
	.failed { border-style: dashed; }
	.total-requests { flex: none; display: flex; align-items: center; gap: 10px; height: 32px; padding: 0 11px; border: 1px solid var(--border); border-radius: 5px; color: var(--text-muted); background: var(--nav-hover); font-size: 11px; }
	.total-requests strong { color: var(--text-primary); font-size: 12px; }
	.total-requests.stale { opacity: .6; }
	.still { overflow-x: auto; mask-image: none; }
	.still .ticker-belt { will-change: auto; }
	@media (max-width: 620px) {
		.request-ticker { padding-left: 8px; padding-right: 8px; gap: 6px; }
		.total-requests { flex-direction: column; justify-content: center; gap: 0; height: 32px; padding: 0 8px; font-size: 9px; }
		.request-chip { font-size: 11px; padding: 0 8px; }
		.request-chip > :not(:first-child) { margin-left: 8px; padding-left: 8px; }
	}
</style>
