// Pure grouping and aggregation for the session drill-down. No Svelte or
// $lib runtime imports so node tests can load it directly.
import type { RequestRow } from "./model";

export type TimelineChild = {
  key: string;
  session_id: string;
  thread_id?: string;
  subagent?: string;
  agent_name?: string;
  parent_session_id?: string;
  models: string[];
  calls: number;
  tool_calls: number;
  input: number;
  cache_read: number;
  cache_write: number;
  output: number;
  first: number;
  last: number;
};

export type Timeline = {
  harness: string;
  session_id: string;
  parent_session_id?: string;
  requests: RequestRow[];
  total: number;
  truncated: boolean;
  children: TimelineChild[];
};

const MAIN = "main";

/** Which lane a call belongs to: the main thread, a child thread, or a child session. */
export function laneOf(row: RequestRow, sessionId: string): string {
  if (row.session_id && row.session_id !== sessionId) return `session:${row.session_id}`;
  if (row.thread_id) return `thread:${row.thread_id}`;
  return MAIN;
}

export function isModelCall(row: RequestRow): boolean {
  return !row.kind || row.kind === "completion";
}

export const rowTokens = (r: RequestRow) => (r.input || 0) + (r.cache_read || 0) + (r.cache_write || 0) + (r.output || 0);

export function failed(r: RequestRow): boolean {
  return r.status >= 400 || r.state === "failed" || r.state === "interrupted";
}

/** A row carries call details when it was recorded after tool tracking shipped. */
export function hasCallDetails(r: RequestRow): boolean {
  return !!(r.tools?.length || r.tool_results || r.effort || r.turn_id || r.request_kind || r.subagent || r.reasoning_tokens || r.thread_id);
}

export type Summary = {
  calls: number;
  requests: number;
  first: number;
  last: number;
  span: number;
  input: number;
  cacheRead: number;
  cacheWrite: number;
  output: number;
  reasoning: number;
  tokens: number;
  toolCalls: number;
  subagents: number;
  failures: number;
  inFlight: number;
  /** Earliest call with call details; earlier calls predate tool tracking. */
  detailsSince: number | null;
  untracked: number;
};

export function summarize(rows: RequestRow[], sessionId: string): Summary {
  const s: Summary = { calls: 0, requests: rows.length, first: 0, last: 0, span: 0, input: 0, cacheRead: 0, cacheWrite: 0, output: 0, reasoning: 0, tokens: 0, toolCalls: 0, subagents: 0, failures: 0, inFlight: 0, detailsSince: null, untracked: 0 };
  const lanes = new Set<string>();
  for (const r of rows) {
    const end = r.time + Math.max(0, r.ms || 0);
    s.first = r === rows[0] ? r.time : Math.min(s.first, r.time);
    s.last = Math.max(s.last, end);
    if (r.state === "streaming") s.inFlight++;
    if (failed(r)) s.failures++;
    const lane = laneOf(r, sessionId);
    if (lane !== MAIN) lanes.add(lane);
    if (!isModelCall(r)) continue;
    s.calls++;
    s.input += r.input || 0;
    s.cacheRead += r.cache_read || 0;
    s.cacheWrite += r.cache_write || 0;
    s.output += r.output || 0;
    s.reasoning += r.reasoning_tokens || 0;
    s.toolCalls += r.tools?.length || 0;
    if (hasCallDetails(r)) s.detailsSince = s.detailsSince === null ? r.time : Math.min(s.detailsSince, r.time);
  }
  s.tokens = s.input + s.cacheRead + s.cacheWrite + s.output;
  s.span = s.last - s.first;
  s.subagents = lanes.size;
  if (s.detailsSince !== null) s.untracked = rows.filter((r) => isModelCall(r) && r.time < s.detailsSince!).length;
  return s;
}

export type ModelStat = {
  model: string;
  calls: number;
  tokens: number;
  input: number;
  output: number;
  cacheRead: number;
  ms: number;
  efforts: { effort: string; calls: number }[];
};

export function modelBreakdown(rows: RequestRow[]): ModelStat[] {
  const out = new Map<string, ModelStat & { effortMap: Map<string, number> }>();
  for (const r of rows) {
    if (!isModelCall(r)) continue;
    const model = r.model || "Unknown model";
    const m = out.get(model) ?? { model, calls: 0, tokens: 0, input: 0, output: 0, cacheRead: 0, ms: 0, efforts: [], effortMap: new Map() };
    m.calls++;
    m.tokens += rowTokens(r);
    m.input += (r.input || 0) + (r.cache_write || 0);
    m.cacheRead += r.cache_read || 0;
    m.output += r.output || 0;
    m.ms += r.ms || 0;
    const effort = r.effort || "default";
    m.effortMap.set(effort, (m.effortMap.get(effort) ?? 0) + 1);
    out.set(model, m);
  }
  return [...out.values()]
    .map(({ effortMap, ...m }) => ({ ...m, efforts: [...effortMap].map(([effort, calls]) => ({ effort, calls })).sort((a, b) => b.calls - a.calls) }))
    .sort((a, b) => b.calls - a.calls || b.tokens - a.tokens);
}

export type ToolStat = { name: string; kind: string; count: number };

/** Count tool calls per tool name, most used first. */
export function toolBreakdown(rows: RequestRow[]): ToolStat[] {
  const out = new Map<string, ToolStat>();
  for (const r of rows) {
    for (const t of r.tools ?? []) {
      const name = t.name || "unknown";
      const s = out.get(name) ?? { name, kind: t.kind || "", count: 0 };
      s.count++;
      out.set(name, s);
    }
  }
  return [...out.values()].sort((a, b) => b.count - a.count || a.name.localeCompare(b.name));
}

/** Tool chips for one call: each tool name with its count, in first-call order. */
export function toolChips(row: RequestRow): { name: string; count: number }[] {
  const out = new Map<string, number>();
  for (const t of row.tools ?? []) out.set(t.name || "unknown", (out.get(t.name || "unknown") ?? 0) + 1);
  return [...out].map(([name, count]) => ({ name, count }));
}

export type SubagentStat = {
  key: string;
  kind: string;
  label: string;
  agentName: string;
  sessionId: string;
  threadId: string;
  models: string[];
  calls: number;
  tokens: number;
  toolCalls: number;
  first: number;
  last: number;
  inFlight: number;
};

const SUBAGENT_LABELS: Record<string, string> = {
  thread_spawn: "Subagent",
  sidechain: "Subagent",
  review: "Review",
  guardian: "Auto-review",
  compact: "Compaction",
  memory_consolidation: "Memory",
};

export function subagentLabel(kind?: string): string {
  if (!kind) return "";
  return SUBAGENT_LABELS[kind] ?? kind.replace(/_/g, " ");
}

/** Short display name for a Codex agent path such as /root/explorer. */
export function agentLabel(name?: string): string {
  if (!name) return "";
  return name.replace(/^\/root\//, "").replace(/^\//, "");
}

/** One entry per child thread or child session, in order of first call. */
export function subagents(rows: RequestRow[], sessionId: string): SubagentStat[] {
  const out = new Map<string, SubagentStat>();
  for (const r of rows) {
    const key = laneOf(r, sessionId);
    if (key === MAIN) continue;
    const s = out.get(key) ?? { key, kind: "", label: "", agentName: "", sessionId: r.session_id || "", threadId: r.thread_id || "", models: [], calls: 0, tokens: 0, toolCalls: 0, first: r.time, last: r.time, inFlight: 0 };
    if (!s.kind && r.subagent) s.kind = r.subagent;
    if (!s.agentName && r.agent_name) s.agentName = r.agent_name;
    if (r.model && !s.models.includes(r.model)) s.models.push(r.model);
    if (isModelCall(r)) {
      s.calls++;
      s.tokens += rowTokens(r);
      s.toolCalls += r.tools?.length || 0;
    }
    if (r.state === "streaming") s.inFlight++;
    s.first = Math.min(s.first, r.time);
    s.last = Math.max(s.last, r.time + Math.max(0, r.ms || 0));
    out.set(key, s);
  }
  return [...out.values()]
    .map((s) => ({ ...s, label: agentLabel(s.agentName) || subagentLabel(s.kind) || "Child agent" }))
    .sort((a, b) => a.first - b.first);
}

/** Calls without a turn ID (older rows, Claude Code) group into activity blocks split by idle gaps. */
export const IDLE_GAP_MS = 5 * 60_000;

export type Turn = {
  key: string;
  /** 1-based index among the main thread's turns; 0 for child agents and untracked blocks. */
  index: number;
  turnId: string;
  lane: string;
  tracked: boolean;
  start: number;
  end: number;
  rows: RequestRow[];
  calls: number;
  /** Calls made by child agents while this main-thread turn ran. */
  childCalls: number;
  tokens: number;
  toolCalls: number;
  failures: number;
  subagent: string;
  agentName: string;
  requestKinds: string[];
};

/**
 * Group calls into turns. Calls sharing a turn ID form one turn; calls without
 * one form blocks split at idle gaps. When the main thread is present, child
 * agent calls (spawned workers, reviews) join the main turn running when they
 * started, so the timeline reads as the conversation's turns. Turns are ordered
 * by their first call.
 */
export function groupTurns(rows: RequestRow[], sessionId: string): Turn[] {
  const sorted = [...rows].sort((a, b) => a.time - b.time || a.sequence - b.sequence);
  const turns = new Map<string, Turn>();
  const lastUntracked = new Map<string, Turn>();
  let blocks = 0;
  const own = (r: RequestRow, lane: string) => {
    if (r.turn_id) {
      let turn = turns.get(`turn:${lane}:${r.turn_id}`);
      if (!turn) {
        turn = newTurn(`turn:${lane}:${r.turn_id}`, r.turn_id, lane, true, r);
        turns.set(turn.key, turn);
      }
      return turn;
    }
    const previous = lastUntracked.get(lane);
    if (previous && r.time - previous.end <= IDLE_GAP_MS) return previous;
    const turn = newTurn(`block:${lane}:${++blocks}`, "", lane, false, r);
    turns.set(turn.key, turn);
    lastUntracked.set(lane, turn);
    return turn;
  };
  const add = (turn: Turn, r: RequestRow, lane: string) => {
    turn.rows.push(r);
    turn.start = Math.min(turn.start, r.time);
    turn.end = Math.max(turn.end, r.time + Math.max(0, r.ms || 0));
    if (lane === turn.lane) {
      if (!turn.subagent && r.subagent) turn.subagent = r.subagent;
      if (!turn.agentName && r.agent_name) turn.agentName = r.agent_name;
    }
    if (r.request_kind && r.request_kind !== "turn" && !turn.requestKinds.includes(r.request_kind)) turn.requestKinds.push(r.request_kind);
    if (failed(r)) turn.failures++;
    if (isModelCall(r)) {
      turn.calls++;
      if (lane !== turn.lane) turn.childCalls++;
      turn.tokens += rowTokens(r);
      turn.toolCalls += r.tools?.length || 0;
    }
  };
  const children: RequestRow[] = [];
  for (const r of sorted) {
    const lane = laneOf(r, sessionId);
    if (lane === MAIN) add(own(r, lane), r, lane);
    else children.push(r);
  }
  const main = [...turns.values()].sort((a, b) => a.start - b.start);
  for (const r of children) {
    const lane = laneOf(r, sessionId);
    let host: Turn | undefined;
    for (const t of main) if (t.start <= r.time) host = t; else break;
    add(host ?? own(r, lane), r, lane);
  }
  const out = [...turns.values()].sort((a, b) => a.start - b.start);
  let index = 0;
  for (const t of out) {
    t.rows.sort((a, b) => a.time - b.time || a.sequence - b.sequence);
    if (t.lane === MAIN && t.tracked) t.index = ++index;
  }
  return out;
}

function newTurn(key: string, turnId: string, lane: string, tracked: boolean, r: RequestRow): Turn {
  return { key, index: 0, turnId, lane, tracked, start: r.time, end: r.time, rows: [], calls: 0, childCalls: 0, tokens: 0, toolCalls: 0, failures: 0, subagent: "", agentName: "", requestKinds: [] };
}

/** Rows visible in a scope: everything, the main thread, or one child lane. */
export function inScope(rows: RequestRow[], sessionId: string, scope: string): RequestRow[] {
  if (scope === "all") return rows;
  return rows.filter((r) => laneOf(r, sessionId) === scope);
}

/** Offset from session start as +m:ss, or +h:mm:ss past an hour. */
export function relativeTime(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  const pad = (n: number) => String(n).padStart(2, "0");
  return h ? `+${h}:${pad(m)}:${pad(s)}` : `+${pad(m)}:${pad(s)}`;
}

/** Human duration: 850ms, 12.4s, 3m 05s, 1h 02m. */
export function span(ms: number): string {
  if (ms < 1000) return `${Math.max(0, Math.round(ms))}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  const total = Math.round(ms / 1000);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  return h ? `${h}h ${String(m).padStart(2, "0")}m` : `${m}m ${String(s).padStart(2, "0")}s`;
}

/** Categorical slots for models, in order of first appearance; the sixth and later share a neutral. */
export const MODEL_SLOTS = 5;
export function modelSlots(rows: RequestRow[]): Map<string, number> {
  const slots = new Map<string, number>();
  for (const r of [...rows].sort((a, b) => a.time - b.time || a.sequence - b.sequence)) {
    if (!isModelCall(r) || !r.model || slots.has(r.model)) continue;
    slots.set(r.model, slots.size < MODEL_SLOTS ? slots.size + 1 : 0);
  }
  return slots;
}
