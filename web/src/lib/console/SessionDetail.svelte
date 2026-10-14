<script lang="ts">
  import Icon from "./Icon.svelte";
  import HarnessLogo from "./HarnessLogo.svelte";
  import { harnessName, sessionKey } from "./harness";
  import { count, type RequestRow, type Session } from "./model";
  import {
    summarize, modelBreakdown, toolBreakdown, toolChips, subagents, groupTurns, inScope, laneOf,
    relativeTime, span, modelSlots, subagentLabel, agentLabel, isModelCall, failed, rowTokens,
    type Timeline, type Turn,
  } from "./session-detail";

  let { harness, sessionId, sessions, onselect, onlogs, onopen, onback }: {
    harness: string;
    sessionId: string;
    sessions: Session[];
    onselect: (row: RequestRow) => void;
    onlogs: () => void;
    onopen: (id: string, harness: string) => void;
    onback: () => void;
  } = $props();

  let data = $state<Timeline | null>(null);
  let loadError = $state("");
  let scope = $state("all");
  let open = $state<Record<string, boolean>>({});
  let limit = $state(400);
  let showAllTools = $state(false);
  let copied = $state(false);
  let hover = $state<{ row: RequestRow; x: number; y: number } | null>(null);
  let generation = 0;

  const index = $derived(new Map(sessions.map((s) => [sessionKey(s.session_id, s.harness), s])));
  const info = $derived(index.get(sessionKey(sessionId, harness)));
  const title = (id: string) => index.get(sessionKey(id, harness))?.task?.title || `Session ${id.slice(0, 8)}…${id.slice(-6)}`;
  const rows = $derived(data?.requests ?? []);
  const whole = $derived(summarize(rows, sessionId));
  const scoped = $derived(inScope(rows, sessionId, scope));
  const summary = $derived(scope === "all" ? whole : summarize(scoped, sessionId));
  const models = $derived(modelBreakdown(scoped));
  const tools = $derived(toolBreakdown(scoped));
  const distinctTools = $derived(scope === "all" ? tools.length : toolBreakdown(rows).length);
  const subs = $derived(subagents(rows, sessionId));
  const turns = $derived(groupTurns(scoped, sessionId));
  const slots = $derived(modelSlots(rows));
  const maxTool = $derived(Math.max(1, ...tools.map((t) => t.count)));
  const totalCalls = $derived(Math.max(1, models.reduce((n, m) => n + m.calls, 0)));
  const scopeLabel = $derived(scope === "all" ? "" : scope === "main" ? "Main thread" : subs.find((s) => s.key === scope)?.label ?? "Subagent");
  const parent = $derived(data?.parent_session_id || info?.parent_session_id || "");
  const lanes = $derived([{ key: "main", label: "Main" }, ...subs.map((s) => ({ key: s.key, label: s.label }))]);
  // Large sessions start with turns collapsed; the latest turn stays open.
  const collapseByDefault = $derived(scoped.length > 300 || turns.length > 30);
  const isOpen = (t: Turn) => open[t.key] ?? (!collapseByDefault || t === turns[turns.length - 1]);
  const visible = $derived.by(() => {
    let budget = limit;
    const out: { turn: Turn; rows: RequestRow[]; hidden: number }[] = [];
    for (const turn of turns) {
      if (!isOpen(turn)) { out.push({ turn, rows: [], hidden: 0 }); continue; }
      const shown = turn.rows.slice(0, Math.max(0, budget));
      budget -= shown.length;
      out.push({ turn, rows: shown, hidden: turn.rows.length - shown.length });
    }
    return out;
  });
  const hiddenRows = $derived(visible.reduce((n, v) => n + v.hidden, 0));

  async function load() {
    const g = ++generation;
    try {
      const response = await fetch(`/api/sessions/timeline?harness=${encodeURIComponent(harness)}&session_id=${encodeURIComponent(sessionId)}`);
      if (!response.ok) throw new Error(`Garcon returned ${response.status}. Try again.`);
      const result = await response.json();
      if (g === generation) { data = result; loadError = ""; }
    } catch (e) {
      if (g === generation) loadError = (e as Error).message;
    }
  }
  $effect(() => {
    harness; sessionId;
    data = null; scope = "all"; open = {}; limit = 400;
    let timer: ReturnType<typeof setTimeout>;
    let stopped = false;
    const tick = async () => {
      await load();
      // Refresh quickly while the session is working, slowly otherwise.
      if (!stopped) timer = setTimeout(tick, whole.inFlight > 0 || (info?.active ?? 0) > 0 ? 4000 : 20000);
    };
    void tick();
    return () => { stopped = true; clearTimeout(timer); generation++; };
  });

  function clock(t: number) {
    return t ? new Date(t).toLocaleTimeString("en-US", { hour12: false, hour: "2-digit", minute: "2-digit" }) : "—";
  }
  function day(t: number) {
    return new Date(t).toLocaleDateString("en-US", { month: "short", day: "numeric" });
  }
  function turnTitle(t: Turn) {
    if (t.lane !== "main") {
      const sub = subs.find((s) => s.key === t.lane);
      const kind = subagentLabel(t.subagent || sub?.kind) || "Subagent";
      return sub?.label && sub.label !== kind ? `${kind} · ${sub.label}` : kind;
    }
    if (t.tracked) return `Turn ${t.index}`;
    return "Activity";
  }
  function pick(s: (typeof subs)[number]) {
    if (s.key.startsWith("session:")) onopen(s.sessionId, harness);
    else { scope = s.key; open = {}; limit = 400; }
  }
  function badge(r: RequestRow) {
    const lane = laneOf(r, sessionId);
    if (lane !== "main") {
      const sub = subs.find((s) => s.key === lane);
      return sub?.label ?? subagentLabel(r.subagent);
    }
    return subagentLabel(r.subagent);
  }
  function status(r: RequestRow) {
    if (r.source === "claude-local") return "Recorded";
    if (r.state === "streaming") return "Streaming";
    if (r.state === "interrupted") return "Interrupted";
    if (r.state === "failed" && r.status < 400) return "Failed";
    return String(r.status || "—");
  }
  function color(model: string) {
    const slot = slots.get(model) ?? 0;
    return slot ? `var(--series-${[1, 2, 3, 7, 5][slot - 1]})` : "var(--desk-muted)";
  }
  async function copyId() {
    try { await navigator.clipboard.writeText(sessionId); copied = true; setTimeout(() => (copied = false), 2000); } catch {}
  }
  function place(r: RequestRow) {
    const start = whole.first, total = Math.max(1, whole.span);
    const left = ((r.time - start) / total) * 100;
    const width = (Math.max(0, r.ms || 0) / total) * 100;
    return `left:${left.toFixed(3)}%;width:max(3px,${width.toFixed(3)}%)`;
  }
  const mainTurnStarts = $derived(groupTurns(inScope(rows, sessionId, "main"), sessionId).filter((t) => t.tracked));
</script>

<div class="detail">
  <div class="detail-nav">
    <button class="back" onclick={onback}><span class="flip"><Icon name="arrow" size={15} /></span>All sessions</button>
    {#if parent}<button class="back parent" onclick={() => onopen(parent, harness)} title={parent}>Started by <strong>{title(parent)}</strong><Icon name="arrow" size={14} /></button>{/if}
    <button class="button secondary" onclick={onlogs}><Icon name="logs" size={15} />Open in Logs</button>
  </div>

  <header class="detail-head">
    <HarnessLogo {harness} size={36} />
    <div>
      <h2>{info?.task?.title || title(sessionId)}</h2>
      <p>
        {harnessName(harness)}{info?.task?.cwd ? ` · ${info.task.cwd.split(/[\\/]/).filter(Boolean).at(-1)}` : ""}
        · <span class="mono" title={sessionId}>{sessionId.slice(0, 8)}…{sessionId.slice(-6)}</span>
        <button class="row-action" aria-label={copied ? "Session ID copied" : "Copy session ID"} onclick={copyId}><Icon name={copied ? "check" : "copy"} size={12} /></button>
        {#if info?.account}· {info.account}{/if}
      </p>
    </div>
  </header>

  {#if loadError && !data}<div class="alert" role="alert"><Icon name="pulse" />{loadError}</div>{/if}
  {#if !data}<div class="loading-state"><span class="loading-line"></span>Loading session…</div>
  {:else}
    <div class="detail-stats">
      <div><span>MODEL CALLS</span><strong>{count(whole.calls)}</strong><small>{whole.failures ? `${whole.failures} failed or interrupted` : whole.inFlight ? `${whole.inFlight} in flight` : "All completed"}</small></div>
      <div><span>TOKENS</span><strong>{count(whole.tokens)}</strong><small>in {count(whole.input + whole.cacheWrite)} · out {count(whole.output)} · cache {count(whole.cacheRead)}</small></div>
      <div><span>TOOL CALLS</span><strong>{count(whole.toolCalls)}</strong><small>{whole.detailsSince === null ? "Not recorded for these calls" : `${distinctTools} distinct ${distinctTools === 1 ? "tool" : "tools"}`}</small></div>
      <div><span>SUBAGENTS</span><strong>{subs.length}</strong><small>{subs.length ? `${subs.reduce((n, s) => n + s.calls, 0)} calls by child agents` : "None recorded"}</small></div>
      <div><span>WALL CLOCK</span><strong>{span(whole.span)}</strong><small>{day(whole.first)} {clock(whole.first)} → {clock(whole.last)}</small></div>
    </div>

    {#if data.truncated}<p class="detail-note"><Icon name="clock" size={14} />Showing the newest {count(rows.length)} of {count(data.total)} requests.</p>{/if}
    {#if whole.detailsSince === null && whole.calls}<p class="detail-note"><Icon name="shield" size={14} />Tool details are recorded for calls after this update. These calls show model, tokens and timing only.</p>
    {:else if whole.untracked}<p class="detail-note"><Icon name="shield" size={14} />{count(whole.untracked)} earlier {whole.untracked === 1 ? "call predates" : "calls predate"} tool tracking and {whole.untracked === 1 ? "has" : "have"} no tool details.</p>{/if}

    {#if whole.span > 0 && rows.length}
      <section class="data-panel activity" aria-label="Activity over time">
        <div class="panel-title"><h2>Activity</h2><span class="legend">{#each models.slice(0, 6) as m}<span><i style:background={color(m.model)}></i>{m.model}</span>{/each}</span></div>
        <div class="map" role="img" aria-label={`${whole.calls} model calls over ${span(whole.span)}`}>
          {#each lanes as lane (lane.key)}
            <div class="map-lane" class:dim={scope !== "all" && scope !== lane.key}>
              <span class="map-label" title={lane.label}>{lane.label}</span>
              <div class="map-track">
                {#if lane.key === "main"}{#each mainTurnStarts as t (t.key)}<i class="turn-tick" style:left={`${((t.start - whole.first) / Math.max(1, whole.span)) * 100}%`}></i>{/each}{/if}
                {#each rows as r (r.sequence)}{#if laneOf(r, sessionId) === lane.key && isModelCall(r)}<button
                  class="mark" class:bad={failed(r)} class:live={r.state === "streaming"}
                  style={`${place(r)};background:${color(r.model)}`}
                  aria-label={`${relativeTime(r.time - whole.first)} ${r.model}`}
                  onmouseenter={(e) => { const box = (e.currentTarget as HTMLElement).closest(".map")!.getBoundingClientRect(); const m = (e.currentTarget as HTMLElement).getBoundingClientRect(); hover = { row: r, x: m.left - box.left + m.width / 2, y: m.top - box.top }; }}
                  onmouseleave={() => (hover = null)}
                  onclick={() => onselect(r)}></button>{/if}{/each}
              </div>
            </div>
          {/each}
          <div class="map-axis"><span></span><div><span>+00:00</span><span>{relativeTime(whole.span / 2)}</span><span>{relativeTime(whole.span)}</span></div></div>
          {#if hover}<div class="map-tip" style:left={`${hover.x}px`} style:top={`${hover.y}px`}>
            <strong>{relativeTime(hover.row.time - whole.first)} · {hover.row.model || "Unknown model"}{hover.row.effort ? ` · ${hover.row.effort}` : ""}</strong>
            <span>{count(rowTokens(hover.row))} tokens · {span(hover.row.ms || 0)}{hover.row.tools?.length ? ` · ${hover.row.tools.length} ${hover.row.tools.length === 1 ? "tool" : "tools"}` : ""}</span>
          </div>{/if}
        </div>
      </section>
    {/if}

    {#if subs.length}
      <div class="scope" role="group" aria-label="Show calls from">
        <button class:selected={scope === "all"} aria-pressed={scope === "all"} onclick={() => { scope = "all"; open = {}; }}>Whole session</button>
        <button class:selected={scope === "main"} aria-pressed={scope === "main"} onclick={() => { scope = "main"; open = {}; }}>Main thread</button>
        {#each subs as s (s.key)}{#if !s.key.startsWith("session:")}<button class:selected={scope === s.key} aria-pressed={scope === s.key} onclick={() => pick(s)}>{s.label}</button>{/if}{/each}
      </div>
    {/if}

    <div class="breakdown">
      <section class="data-panel">
        <div class="panel-title"><h2>Models</h2><span>{scopeLabel || `${models.length} ${models.length === 1 ? "model" : "models"}`}</span></div>
        {#each models as m (m.model)}<div class="rank-row">
          <i class="dot" style:background={color(m.model)}></i>
          <div class="rank-main">
            <strong class="mono">{m.model}</strong>
            <div class="rank-track"><span style:width={`${(m.calls / totalCalls) * 100}%`}></span></div>
            <small class="efforts">{#each m.efforts as e}<span class="chip">{e.effort} ×{e.calls}</span>{/each}</small>
          </div>
          <div class="rank-value"><strong>{count(m.calls)}</strong><small>{count(m.tokens)} tokens</small></div>
        </div>{/each}
        {#if !models.length}<div class="small-empty">No model calls in this view.</div>{/if}
      </section>
      <section class="data-panel">
        <div class="panel-title"><h2>Tools</h2><span>{count(summary.toolCalls)} calls</span></div>
        {#each showAllTools ? tools : tools.slice(0, 10) as t (t.name)}<div class="tool-row">
          <span class="tool-name mono" title={t.name}>{t.name}</span>
          <div class="tool-bar"><span style:width={`${(t.count / maxTool) * 100}%`}></span></div>
          <strong class="mono">{t.count}</strong>
        </div>{/each}
        {#if tools.length > 10}<button class="more" onclick={() => (showAllTools = !showAllTools)}>{showAllTools ? "Show fewer" : `Show all ${tools.length} tools`}</button>{/if}
        {#if !tools.length}<div class="small-empty">{summary.detailsSince === null ? "Tool details are recorded for calls after this update." : "No tool calls in this view."}</div>{/if}
      </section>
      <section class="data-panel">
        <div class="panel-title"><h2>Subagents</h2><span>{subs.length} recorded</span></div>
        {#each subs as s (s.key)}<button class="sub-row" class:selected={scope === s.key} onclick={() => pick(s)} title={s.key.startsWith("session:") ? "Open this child session" : "Show only this agent's calls"}>
          <div class="rank-main">
            <strong>{s.label}{#if s.inFlight}<i class="tiny-live"></i>{/if}</strong>
            <small>{#if (subagentLabel(s.kind) || "Child agent") !== s.label}<span class="chip">{subagentLabel(s.kind) || "Child agent"}</span>{/if}{s.models.join(", ") || "Unknown model"}</small>
          </div>
          <div class="rank-value"><strong>{count(s.calls)} calls</strong><small>{count(s.tokens)} tokens · {s.toolCalls} tools</small></div>
          <Icon name="chevron" size={14} />
        </button>{/each}
        {#if !subs.length}<div class="small-empty">No subagents or child sessions recorded.</div>{/if}
      </section>
    </div>

    <section class="data-panel timeline">
      <div class="panel-title">
        <h2>Timeline{scopeLabel ? ` · ${scopeLabel}` : ""}</h2>
        <span class="timeline-actions">
          {#if scope !== "all"}<button class="filter-button" onclick={() => { scope = "all"; open = {}; }}>Back to whole session</button>{/if}
          <button class="filter-button" onclick={() => (open = Object.fromEntries(turns.map((t) => [t.key, true])))}>Expand all</button>
          <button class="filter-button" onclick={() => (open = Object.fromEntries(turns.map((t) => [t.key, false])))}>Collapse all</button>
        </span>
      </div>
      {#each visible as v (v.turn.key)}
        {@const t = v.turn}
        <button class="turn" class:child={t.lane !== "main"} aria-expanded={isOpen(t)} onclick={() => (open = { ...open, [t.key]: !isOpen(t) })}>
          <span class="turn-chevron" class:open={isOpen(t)}><Icon name="chevron" size={13} /></span>
          <strong>{turnTitle(t)}</strong>
          <span class="mono muted">{relativeTime(t.start - whole.first)} → {relativeTime(t.end - whole.first)} · {span(t.end - t.start)}</span>
          {#each t.requestKinds as k}<span class="chip">{k}</span>{/each}
          <span class="turn-meta">{t.calls} {t.calls === 1 ? "call" : "calls"}{t.childCalls ? ` (${t.childCalls} by subagents)` : ""} · {t.toolCalls} tools · {count(t.tokens)} tokens{#if t.failures} · <b>{t.failures} failed</b>{/if}</span>
        </button>
        {#each v.rows as r (r.sequence)}
          <button class="call" class:bad={failed(r)} class:sub={laneOf(r, sessionId) !== t.lane} onclick={() => onselect(r)} aria-label={`Inspect call at ${relativeTime(r.time - whole.first)}`}>
            <span class="call-time mono" title={new Date(r.time).toLocaleString()}>{relativeTime(r.time - whole.first)}</span>
            <span class="call-model">
              <span class="model-id"><i class="dot" style:background={color(r.model)}></i><span class="mono model">{r.model || r.path || "Unknown model"}</span></span>
              {#if r.effort}<span class="chip">{r.effort}</span>{/if}
              {#if badge(r)}<span class="chip agent">{badge(r)}</span>{/if}
              {#if r.request_kind && r.request_kind !== "turn"}<span class="chip">{r.request_kind}</span>{/if}
            </span>
            <span class="call-tools">
              {#each toolChips(r) as c}<span class="tool-chip mono">{c.name}{#if c.count > 1}<em>×{c.count}</em>{/if}</span>{/each}
              {#if r.tool_results}<span class="muted results" title="Tool results this request returned to the model">{r.tool_results} {r.tool_results === 1 ? "tool result" : "tool results"} in</span>{/if}
              {#if !isModelCall(r)}<span class="muted">Metadata request</span>{/if}
            </span>
            <span class="call-num mono" title={`Input ${r.input} · cached ${r.cache_read} · cache write ${r.cache_write} · output ${r.output}${r.reasoning_tokens ? ` (reasoning ${r.reasoning_tokens})` : ""}`}>{count(rowTokens(r))}<small>{count(r.output)} out</small></span>
            <span class="call-num mono">{r.source === "claude-local" ? "—" : r.state === "streaming" ? "…" : span(r.ms || 0)}<small>{r.first_byte_ms !== undefined ? `TTFB ${span(r.first_byte_ms)}` : ""}</small></span>
            <span class="status-label" class:bad={failed(r)} class:streaming={r.state === "streaming"}><span></span>{status(r)}</span>
          </button>
        {/each}
        {#if v.hidden}<div class="turn-more muted">{v.hidden} more {v.hidden === 1 ? "call" : "calls"} in this turn</div>{/if}
      {/each}
      {#if !turns.length}<div class="small-empty">No requests recorded for this view.</div>{/if}
      {#if hiddenRows}<div class="panel-footer"><span>{count(hiddenRows)} calls not shown yet</span><button class="filter-button" onclick={() => (limit += 400)}>Show more calls</button></div>{/if}
    </section>
    <p class="footnote"><Icon name="shield" size={14} /> Garcon records tool names and counts only. Prompts, tool arguments, tool output and responses are never saved.</p>
  {/if}
</div>

<style>
  .detail { display: flex; flex-direction: column; gap: 18px; }
  .detail-nav { display: flex; align-items: center; gap: 14px; flex-wrap: wrap; }
  .detail-nav .button { margin-left: auto; }
  .back { display: inline-flex; align-items: center; gap: 6px; border: 0; background: none; padding: 4px 0; color: var(--desk-muted); font-size: 12px; }
  .back:hover { color: var(--desk-ink); }
  .back.parent strong { color: var(--desk-ink); font-weight: 500; max-width: 280px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .flip { display: inline-flex; transform: scaleX(-1); }
  .detail-head { display: flex; gap: 14px; align-items: center; }
  .detail-head h2 { font-size: 20px; font-weight: 600; letter-spacing: -0.3px; line-height: 1.3; overflow-wrap: anywhere; }
  .detail-head p { margin: 4px 0 0; color: var(--desk-muted); font-size: 12px; display: flex; align-items: center; gap: 4px; flex-wrap: wrap; }
  .mono { font-family: var(--desk-mono); font-variant-numeric: tabular-nums; }
  .detail-stats { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); border: 1px solid var(--desk-line); padding: 18px 0; }
  .detail-stats > div { padding: 0 18px; min-width: 0; }
  .detail-stats > div + div { border-left: 1px solid var(--desk-line); }
  .detail-stats span { display: block; font-size: 10px; color: var(--desk-muted); }
  .detail-stats strong { display: block; font-size: 23px; font-weight: 500; margin-top: 6px; font-variant-numeric: tabular-nums; }
  .detail-stats small { display: block; font-size: 10px; color: var(--desk-muted); margin-top: 3px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .detail-note { display: flex; gap: 8px; align-items: center; margin: -6px 0 0; color: var(--desk-muted); font-size: 11px; }
  .legend { display: flex; flex-wrap: wrap; gap: 4px 14px; justify-content: flex-end; }
  .legend span { display: inline-flex; align-items: center; gap: 6px; font: 10px var(--desk-mono); color: var(--desk-muted); }
  .legend i, .dot { width: 8px; height: 8px; border-radius: 2px; flex-shrink: 0; display: inline-block; }
  .map { position: relative; padding: 14px 16px 10px; }
  .map-lane { display: grid; grid-template-columns: 110px 1fr; align-items: center; gap: 12px; height: 22px; }
  .map-lane.dim { opacity: 0.35; }
  .map-label { font-size: 11px; color: var(--desk-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; text-align: right; }
  .map-track { position: relative; height: 14px; background: var(--desk-subtle); border-radius: 2px; }
  .turn-tick { position: absolute; top: -3px; bottom: -3px; width: 1px; background: var(--desk-line); }
  .mark { position: absolute; top: 2px; height: 10px; padding: 0; border: 0; border-radius: 2px; box-shadow: 0 0 0 1px var(--desk-bg); min-width: 3px; }
  .mark:hover { top: 0; height: 14px; z-index: 2; }
  .mark.bad { outline: 1.5px solid var(--desk-ink); outline-offset: 0; }
  .mark.live { opacity: 0.55; }
  .map-axis { display: grid; grid-template-columns: 110px 1fr; gap: 12px; margin-top: 6px; }
  .map-axis div { display: flex; justify-content: space-between; font: 10px var(--desk-mono); color: var(--desk-muted); }
  .map-tip { position: absolute; transform: translate(-50%, calc(-100% - 8px)); background: var(--desk-ink); color: var(--desk-inverse); padding: 6px 9px; border-radius: 3px; font-size: 11px; pointer-events: none; white-space: nowrap; z-index: 5; display: flex; flex-direction: column; gap: 1px; }
  .map-tip strong { font: 600 11px var(--desk-mono); }
  .scope { display: flex; gap: 6px; flex-wrap: wrap; }
  .scope button { border: 1px solid var(--desk-line); background: var(--desk-bg); border-radius: 3px; padding: 5px 10px; font-size: 11px; color: var(--desk-muted); }
  .scope button.selected { border-color: var(--desk-ink); color: var(--desk-ink); background: var(--desk-hover); }
  .breakdown { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 18px; align-items: start; }
  .breakdown .rank-row { padding: 13px 16px; align-items: flex-start; }
  .breakdown .rank-row .dot { margin-top: 5px; }
  .efforts { display: flex; flex-wrap: wrap; gap: 4px; margin-top: 8px; }
  .chip { display: inline-flex; align-items: center; border: 1px solid var(--desk-line); border-radius: 2px; padding: 0 5px; font: 10px/16px var(--desk-mono); color: var(--desk-muted); white-space: nowrap; }
  .chip.agent { border-style: dashed; color: var(--desk-ink); }
  .tool-row { display: grid; grid-template-columns: minmax(0, 0.9fr) minmax(0, 1fr) 34px; align-items: center; gap: 10px; padding: 7px 16px; }
  .tool-row:first-of-type { padding-top: 12px; }
  .tool-name { font-size: 11px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .tool-bar { height: 8px; background: var(--desk-hover); border-radius: 0 2px 2px 0; overflow: hidden; }
  .tool-bar span { display: block; height: 100%; background: var(--desk-fill); border-radius: 0 2px 2px 0; }
  .tool-row strong { font-size: 11px; font-weight: 500; text-align: right; }
  .more { display: block; width: 100%; border: 0; border-top: 1px solid var(--desk-line); background: none; padding: 10px 16px; margin-top: 6px; text-align: left; font-size: 11px; color: var(--desk-muted); }
  .more:hover { color: var(--desk-ink); }
  .sub-row { display: flex; width: 100%; align-items: center; gap: 12px; padding: 13px 16px; border: 0; border-bottom: 1px solid var(--desk-line); background: none; text-align: left; color: var(--desk-ink); }
  .sub-row:last-of-type { border-bottom: 0; }
  .sub-row:hover, .sub-row.selected { background: var(--desk-subtle); }
  .sub-row strong { display: flex; align-items: center; gap: 6px; }
  .sub-row small { display: flex; align-items: center; gap: 6px; font-size: 10px; color: var(--desk-muted); margin-top: 5px; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
  .sub-row .rank-value strong { justify-content: flex-end; font: 12px var(--desk-mono); }
  .timeline-actions { display: flex; gap: 6px; }
  .turn { display: flex; width: 100%; align-items: center; gap: 10px; flex-wrap: wrap; padding: 10px 16px; border: 0; border-bottom: 1px solid var(--desk-line); background: var(--desk-subtle); text-align: left; font-size: 12px; color: var(--desk-ink); }
  .turn.child { background: var(--desk-bg); padding-left: 30px; }
  .turn strong { font-weight: 600; }
  .turn .muted { font-size: 10px; }
  .turn-chevron { display: inline-flex; color: var(--desk-muted); transition: transform 0.12s; }
  .turn-chevron.open { transform: rotate(90deg); }
  .turn-meta { margin-left: auto; font-size: 11px; color: var(--desk-muted); }
  .turn-meta b { color: var(--desk-ink); }
  .call { display: grid; grid-template-columns: 70px minmax(220px, 1.1fr) minmax(0, 1.4fr) 76px 80px 92px; align-items: center; gap: 14px; width: 100%; padding: 9px 16px 9px 30px; border: 0; border-bottom: 1px solid var(--desk-line); background: none; text-align: left; color: var(--desk-ink); font-size: 12px; }
  .call:hover { background: var(--desk-subtle); }
  .call.sub { box-shadow: inset 2px 0 0 var(--desk-line); }
  .call.sub .call-time { padding-left: 10px; }
  .call.bad .model { font-weight: 600; }
  .call-time { font-size: 11px; color: var(--desk-muted); }
  .call-model { display: flex; align-items: center; gap: 6px; min-width: 0; flex-wrap: wrap; }
  .model-id { display: inline-flex; align-items: center; gap: 6px; min-width: 0; max-width: 100%; }
  .call-model .model { font-size: 11px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 100%; }
  .call-tools { display: flex; flex-wrap: wrap; gap: 4px; min-width: 0; align-items: center; }
  .tool-chip { display: inline-flex; gap: 3px; align-items: center; padding: 1px 6px; border-radius: 2px; background: var(--desk-hover); font-size: 10px; max-width: 220px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .tool-chip em { font-style: normal; color: var(--desk-muted); }
  .results { font-size: 10px; }
  .call-num { font-size: 11px; text-align: right; }
  .call-num small { display: block; font-size: 9px; color: var(--desk-muted); margin-top: 1px; }
  .call .status-label { justify-self: end; }
  .turn-more { padding: 8px 30px; font-size: 11px; border-bottom: 1px solid var(--desk-line); }
  .footnote { margin: 0; }
  @media (max-width: 1150px) {
    .detail-stats { grid-template-columns: repeat(3, minmax(0, 1fr)); row-gap: 16px; }
    .detail-stats > div:nth-child(4) { border-left: 0; }
    .breakdown { grid-template-columns: 1fr 1fr; }
    .call { grid-template-columns: 64px minmax(180px, 1fr) minmax(0, 1.2fr) 70px 74px 84px; gap: 10px; }
  }
  @media (max-width: 720px) {
    .detail-head h2 { font-size: 17px; }
    .detail-stats { grid-template-columns: 1fr 1fr; padding: 14px 0; }
    .detail-stats > div { padding: 0 12px; }
    .detail-stats > div:nth-child(odd) { border-left: 0; }
    .detail-stats > div:nth-child(4) { border-left: 1px solid var(--desk-line); }
    .detail-stats > div:last-child { grid-column: 1 / -1; }
    .detail-stats strong { font-size: 19px; }
    .breakdown { grid-template-columns: 1fr; }
    .map-lane, .map-axis { grid-template-columns: 64px 1fr; gap: 8px; }
    .legend { display: none; }
    .timeline .panel-title { flex-wrap: wrap; gap: 8px; }
    .turn { padding: 10px 12px; }
    .turn.child { padding-left: 20px; }
    .turn-meta { margin-left: 23px; flex-basis: 100%; }
    .detail-stats small { white-space: normal; }
    .call { grid-template-columns: 58px minmax(0, 1fr) auto; grid-template-areas: "time model status" ". tools tools" ". tokens ms"; gap: 6px 10px; padding: 10px 12px 10px 20px; align-items: start; }
    .call-time { padding-top: 2px; }
    .call-tools:empty { display: none; }
    .call-time { grid-area: time; }
    .call-model { grid-area: model; }
    .call-tools { grid-area: tools; }
    .call .status-label { grid-area: status; }
    .call-num { text-align: left; }
    .call-num:nth-of-type(4) { grid-area: tokens; }
    .call-num:nth-of-type(5) { grid-area: ms; text-align: right; }
    .call-num small { display: inline; margin-left: 6px; }
  }
</style>
