<script lang="ts">
  import { onMount, untrack } from "svelte";
  import Icon from "./Icon.svelte";
  import HarnessLogo from "./HarnessLogo.svelte";
  import { harnessName } from "./harness";
  import { LiveRequests, hasTokens } from "./live-requests";
  import type { RequestRow } from "./model";

  let { rows, connected, active, hideZeroTokens, taskName, onselect }: {
    rows: RequestRow[] | null;
    connected: boolean;
    active: number;
    hideZeroTokens: boolean;
    taskName: (id?: string, harness?: string) => string;
    onselect: (row: RequestRow) => void;
  } = $props();
  let items = $state<{ row: RequestRow; shownAt: number; leadingSpace: number }[]>([]);
  let paused = $state(false);
  let reducedMotion = $state(false);
  let lane: HTMLDivElement;
  let belt: HTMLDivElement;
  const queue = new LiveRequests();
  let offset = 0;

  $effect(() => {
    const snapshot = rows;
    const hide = hideZeroTokens;
    if (snapshot === null) return;
    untrack(() => {
      if (queue.update(snapshot, hide, performance.now())) { items = []; offset = 0; }
      const latest = new Map(snapshot.map(row => [row.sequence, row]));
      items = items.map(item => ({ ...item, row: latest.get(item.row.sequence) ?? item.row }));
      // Already visible items finish their one pass, even when their state changes.
    });
  });

  onMount(() => {
    const media = matchMedia('(prefers-reduced-motion: reduce)');
    const motion = () => { reducedMotion = media.matches; };
    motion();
    media.addEventListener('change', motion);
    let frame = 0, previous = 0;
    function animate(time: number) {
      const elapsed = previous ? Math.min(time - previous, 64) : 0;
      previous = time;
      const stopped = paused || belt.matches(':hover, :focus-within') || document.hidden;
      if (!stopped) {
        if (reducedMotion) {
          belt.style.transform = '';
          items = items.filter(item => time - item.shownAt < 12_000);
          const row = queue.next(time);
          if (row && (!hideZeroTokens || hasTokens(row))) items = [...items, { row, shownAt: time, leadingSpace: 0 }].slice(-3);
        } else {
          if (!items.length || (belt.scrollWidth + offset < lane.clientWidth + 360 && items.length < 8)) {
            const row = queue.next(time);
            if (row && (!hideZeroTokens || hasTokens(row))) {
              let leadingSpace = 0;
              if (!items.length) offset = lane.clientWidth;
              else leadingSpace = Math.max(0, lane.clientWidth - (belt.scrollWidth + offset));
              items = [...items, { row, shownAt: time, leadingSpace }];
            }
          }
          if (items.length) {
            offset -= elapsed * 0.09;
            const width = (belt.firstElementChild as HTMLElement)?.offsetWidth ?? 0;
            const occupied = width + items[0].leadingSpace;
            if (width && offset <= -occupied) {
              offset += occupied;
              items = items.slice(1);
              if (!items.length) offset = 0;
            }
          }
          belt.style.transform = `translateX(${offset}px)`;
        }
      }
      frame = requestAnimationFrame(animate);
    }
    frame = requestAnimationFrame(animate);
    return () => { cancelAnimationFrame(frame); media.removeEventListener('change', motion); };
  });
</script>

  <footer class="ticker" aria-label="Live request activity">
    <div class="ticker-brand">
      <span class="status-dot" class:offline={!connected}></span>LIVE ROUTES
    </div>
    <div class="ticker-window" bind:this={lane}>
      <div class="ticker-track" bind:this={belt}>
        {#each items as item (item.row.sequence)}
          {@const r = item.row}
          <button
                  class="ticker-item"
                  style:margin-left={`${reducedMotion ? 0 : item.leadingSpace}px`}
                  title={taskName(r.session_id, r.harness)}
                  onclick={() => onselect(r)}
                  ><span
                    class="ticker-status"
                    class:bad={r.status >= 400 ||
                      r.state === "interrupted" ||
                      r.state === "failed"}
                    >{r.state === "streaming"
                      ? "●"
                      : r.status >= 400 ||
                          r.state === "interrupted" ||
                          r.state === "failed"
                        ? "↘"
                        : "↗"}</span
                  ><HarnessLogo harness={r.harness} size={18} />{#if r.session_id}<span class="ticker-task">{taskName(r.session_id, r.harness)}</span>{/if}<strong>{r.model || `${harnessName(r.harness)} request`}</strong><Icon
                    name="arrow"
                    size={12}
                  /><span>{r.account || "Unassigned"}</span><small
                    >{r.state === "streaming"
                      ? "IN FLIGHT"
                      : `${(r.ms / 1000).toFixed(1)}s`}</small
                  ><span class="ticker-divider">/</span></button
                >
        {/each}
      </div>
      {#if !items.length}<div class="ticker-waiting">
        Listening for requests <span>·</span> Your clients → Garcon → your accounts
      </div>{/if}
    </div>
    <button
      class="ticker-pause"
      aria-label={paused ? "Resume ticker" : "Pause ticker"}
      onclick={() => (paused = !paused)}
      >{paused ? "▶" : "Ⅱ"}</button
    >
    <div class="ticker-count">{active}<span>IN FLIGHT</span></div>
  </footer>
