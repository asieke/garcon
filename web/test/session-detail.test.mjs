import assert from 'node:assert/strict';
import test from 'node:test';
import {
  laneOf, summarize, modelBreakdown, toolBreakdown, toolChips, subagents, groupTurns,
  inScope, relativeTime, span, modelSlots, subagentLabel, agentLabel, IDLE_GAP_MS,
} from '../src/lib/console/session-detail.ts';

let seq = 0;
const call = (time, fields = {}) => ({
  sequence: ++seq, time, ms: 1000, session_id: 'root', model: 'gpt-main', status: 200, state: 'complete',
  kind: 'completion', input: 10, cache_read: 100, cache_write: 0, output: 5, account: '', ...fields,
});
const shell = { name: 'shell', kind: 'function' };
const patch = { name: 'apply_patch', kind: 'custom' };

const session = () => {
  seq = 0;
  return [
    call(0, { turn_id: 't1', effort: 'high', tools: [shell, shell] }),
    call(2_000, { turn_id: 't1', effort: 'high', tools: [patch] }),
    // A spawned agent sharing the session, interleaved with turn t1.
    call(3_000, { turn_id: 'c1', thread_id: 'worker', subagent: 'thread_spawn', agent_name: '/root/worker', model: 'gpt-mini', tools: [shell] }),
    call(4_000, { turn_id: 't1', effort: 'medium' }),
    call(5_000, { turn_id: 'c1', thread_id: 'worker', subagent: 'thread_spawn', model: 'gpt-mini', state: 'streaming' }),
    call(60_000, { turn_id: 't2', effort: 'high', request_kind: 'compaction', subagent: 'compact', tools: [] }),
    // A child conversation recorded under its own session ID.
    call(70_000, { session_id: 'own-child', parent_session_id: 'root', subagent: 'thread_spawn', model: 'gpt-mini', status: 500, state: 'failed' }),
    call(80_000, { kind: 'request', model: '', input: 0, cache_read: 0, output: 0 }),
  ];
};

test('lanes separate the main thread, child threads and child sessions', () => {
  const rows = session();
  assert.equal(laneOf(rows[0], 'root'), 'main');
  assert.equal(laneOf(rows[2], 'root'), 'thread:worker');
  assert.equal(laneOf(rows[6], 'root'), 'session:own-child');
  assert.equal(inScope(rows, 'root', 'thread:worker').length, 2);
  assert.equal(inScope(rows, 'root', 'main').length, 5);
  assert.equal(inScope(rows, 'root', 'all').length, 8);
});

test('summary counts model calls, tokens, tools and subagents over the wall-clock span', () => {
  const s = summarize(session(), 'root');
  assert.equal(s.calls, 7);
  assert.equal(s.requests, 8);
  assert.equal(s.toolCalls, 4);
  assert.equal(s.subagents, 2);
  assert.equal(s.first, 0);
  assert.equal(s.last, 81_000);
  assert.equal(s.span, 81_000);
  assert.equal(s.input, 70);
  assert.equal(s.cacheRead, 700);
  assert.equal(s.output, 35);
  assert.equal(s.tokens, 805);
  assert.equal(s.inFlight, 1);
  assert.equal(s.failures, 1);
  assert.equal(s.detailsSince, 0);
  assert.equal(s.untracked, 0);
});

test('older rows without call details are reported, not mistaken for tool-free calls', () => {
  seq = 0;
  const legacy = [call(0), call(1000)];
  assert.equal(summarize(legacy, 'root').detailsSince, null);
  const mixed = [...legacy, call(2000, { turn_id: 't', tools: [shell] })];
  const s = summarize(mixed, 'root');
  assert.equal(s.detailsSince, 2000);
  assert.equal(s.untracked, 2);
});

test('model breakdown has calls, tokens and the effort mix, busiest first', () => {
  const models = modelBreakdown(session());
  assert.deepEqual(models.map((m) => [m.model, m.calls]), [['gpt-main', 4], ['gpt-mini', 3]]);
  assert.deepEqual(models[0].efforts, [{ effort: 'high', calls: 3 }, { effort: 'medium', calls: 1 }]);
  assert.deepEqual(models[1].efforts, [{ effort: 'default', calls: 3 }]);
  assert.equal(models[0].tokens, 460);
});

test('tool breakdown counts by name and chips group repeats in one call', () => {
  assert.deepEqual(toolBreakdown(session()), [
    { name: 'shell', kind: 'function', count: 3 },
    { name: 'apply_patch', kind: 'custom', count: 1 },
  ]);
  assert.deepEqual(toolChips(session()[0]), [{ name: 'shell', count: 2 }]);
  assert.deepEqual(toolChips(call(0)), []);
});

test('subagents summarize each child lane with a readable label', () => {
  const subs = subagents(session(), 'root');
  assert.equal(subs.length, 2);
  assert.deepEqual([subs[0].key, subs[0].label, subs[0].calls, subs[0].toolCalls, subs[0].inFlight, subs[0].last], ['thread:worker', 'worker', 2, 1, 1, 6_000]);
  assert.deepEqual([subs[1].key, subs[1].label, subs[1].sessionId, subs[1].models], ['session:own-child', 'Subagent', 'own-child', ['gpt-mini']]);
  assert.equal(subagentLabel('guardian'), 'Auto-review');
  assert.equal(subagentLabel('custom_label'), 'custom label');
  assert.equal(agentLabel('/root/explorer/sub'), 'explorer/sub');
});

test('turns group by turn ID; child agent calls join the main turn running when they started', () => {
  const rows = session();
  const turns = groupTurns(rows, 'root');
  assert.deepEqual(turns.map((t) => [t.key, t.index, t.calls, t.childCalls]), [
    ['turn:main:t1', 1, 5, 2],
    ['turn:main:t2', 2, 2, 1],
    ['block:main:1', 0, 0, 0],
  ]);
  assert.deepEqual(turns[0].rows.map((r) => r.time), [0, 2_000, 3_000, 4_000, 5_000]);
  assert.equal(turns[0].end, 6_000);
  assert.equal(turns[0].toolCalls, 4);
  assert.equal(turns[0].subagent, '');
  assert.deepEqual(turns[1].requestKinds, ['compaction']);
  assert.equal(turns[1].failures, 1);
  // Viewing one child agent groups its calls by its own turns.
  const worker = groupTurns(inScope(rows, 'root', 'thread:worker'), 'root');
  assert.deepEqual(worker.map((t) => [t.key, t.index, t.calls, t.subagent]), [['turn:thread:worker:c1', 0, 2, 'thread_spawn']]);
});

test('calls without turn IDs split into blocks at idle gaps', () => {
  seq = 0;
  const rows = [call(0), call(10_000), call(10_000 + 1000 + IDLE_GAP_MS + 1), call(10_000 + 1000 + IDLE_GAP_MS + 2000)];
  assert.deepEqual(groupTurns(rows, 'root').map((t) => t.rows.length), [2, 2]);
});

test('time labels and stable model colors', () => {
  assert.equal(relativeTime(65_000), '+01:05');
  assert.equal(relativeTime(3_725_000), '+1:02:05');
  assert.equal(span(850), '850ms');
  assert.equal(span(12_400), '12.4s');
  assert.equal(span(185_000), '3m 05s');
  assert.equal(span(3_720_000), '1h 02m');
  seq = 0;
  const rows = ['a', 'b', 'a', 'c', 'd', 'e', 'f', 'g'].map((model, i) => call(i, { model }));
  assert.deepEqual([...modelSlots(rows)], [['a', 1], ['b', 2], ['c', 3], ['d', 4], ['e', 5], ['f', 0], ['g', 0]]);
});
