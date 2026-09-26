import assert from 'node:assert/strict';
import test from 'node:test';
import { quotaForRoute, usageAccounts, orderedUsageWindows, usageWindowDisplay, usageWindowLabel, usageFreshness } from '../src/lib/console/account-usage.ts';

const now = Date.UTC(2030, 0, 1);
const window = (id, label, used, seconds = 604800) => ({ id, label, used_percent: used, window_seconds: seconds, resets_at: now + seconds * 1000, expired: false });
const quota = (id, provider, workspace, windows) => ({ id, provider, workspace, email: 'same@example.com', status: 'fresh', fetched_at: now, windows });
const route = (id) => ({ id, email: 'same@example.com', profile: `.codex-${id}`, plan: 'pro', windows: [], enrolled: true, status: 'Ready' });

test('inventory and provider views share the exact account snapshot, never matching by email', () => {
  const a = quota('quota-a', 'codex', 'workspace-a', [window('a','Weekly',1)]);
  const b = quota('quota-b', 'codex', 'workspace-b', [window('b','Weekly',60)]);
  const c = quota('quota-c', 'claude', 'workspace-a', [window('c','Weekly',14)]);
  const routes = [route('workspace-a'), route('workspace-b')];
  const rows = usageAccounts([a,b,c], routes);
  assert.equal(rows.length,3);
  assert.equal(rows.find(r => r.routing?.id === 'workspace-a').quota, a);
  assert.equal(quotaForRoute(routes[0],[a,b,c]),a);
  assert.equal(quotaForRoute(routes[1],[a,b,c]),b);
  assert.equal(rows.find(r => r.provider === 'claude').quota,c);
  assert.equal(quotaForRoute(route('missing'),[a,b,c]),undefined);
  assert.equal(quotaForRoute(routes[0],[a,{...a,id:'another-person'}]),undefined);
});

test('missing routing snapshots stay visible without borrowing another account usage', () => {
  const rows = usageAccounts([quota('q','codex','other',[window('w','Weekly',0)])],[route('missing')]);
  assert.equal(rows.length,2);
  assert.equal(rows.find(r => r.routing).quota,undefined);
  assert.equal(usageFreshness(undefined,now).text,'Usage unavailable');
});

test('Claude shows 5-hour, weekly all-model and Fable limits with independent numbers and resets', () => {
  const windows = [window('f','Fable · Weekly',12),window('w','Weekly',6),window('s','5-hour',0,18000)];
  const ordered = orderedUsageWindows(windows);
  assert.deepEqual(ordered.map(w=>usageWindowLabel(w,'claude')),['5-hour','Weekly · all models','Weekly · Fable']);
  assert.deepEqual(ordered.map(w=>usageWindowDisplay(w,now).percent),['0%','6%','12%']);
  assert.equal(usageWindowDisplay(ordered[0],now).reset,'Resets in 5h 0m');
  assert.equal(usageWindowDisplay(ordered[1],now).reset,'Resets in 7d 0h');
  assert.equal(windows[0].id,'f'); // sorting must not mutate a shared snapshot
});

test('each account keeps its own reported windows, including Codex secondary and feature limits', () => {
  const weekly = window('codex:0','Weekly',2);
  assert.deepEqual(orderedUsageWindows([weekly]).map(w=>usageWindowLabel(w,'codex')),['Weekly']);
  const windows=[weekly,window('codex:1','5-hour',20,18000),window('review:0','Code review · Weekly',8),window('model','Other model · Weekly',3)];
  assert.equal(orderedUsageWindows(windows).length,4);
  assert.deepEqual(orderedUsageWindows(windows).map(w=>usageWindowDisplay(w,now).percent),['20%','2%','8%','3%']);
  assert.deepEqual(orderedUsageWindows([]),[]);
});

test('unknown, zero, exhausted, and over-limit percentages are distinct', () => {
  for (const value of [null,NaN,Infinity,-1]) {
    const display=usageWindowDisplay(window('w','Weekly',value),now);
    assert.equal(display.known,false);assert.equal(display.percent,'—');
  }
  assert.equal(usageWindowDisplay(window('w','Weekly',0),now).percent,'0%');
  assert.equal(usageWindowDisplay(window('w','Weekly',0.5),now).percent,'0.5%');
  assert.equal(usageWindowDisplay(window('w','Weekly',100),now).exhausted,true);
  const over=usageWindowDisplay(window('w','Weekly',120),now);
  assert.equal(over.percent,'120%');assert.equal(over.fill,100);
});

test('missing reset times and expired windows never imply a fresh full allowance', () => {
  const w=window('w','5-hour',0);
  assert.equal(usageWindowDisplay({...w,resets_at:0},now).reset,'Reset not reported');
  const ended=usageWindowDisplay({...w,used_percent:85,resets_at:now},now);
  assert.equal(ended.percent,'85%');assert.equal(ended.ended,true);assert.equal(ended.reset,'Awaiting reset update');
});

test('collection freshness is independent of routing enrollment or model health', () => {
  const q=quota('q','codex','w',[window('w','Weekly',2)]);
  assert.equal(usageFreshness(q,now).stale,false);
  assert.equal(usageFreshness(q,now+600001).stale,true);
  assert.match(usageFreshness({...q,status:'needs_login'},now).text,/Login needs refresh/);
  assert.match(usageFreshness({...q,status:'unavailable'},now).text,/Limits unavailable/);
  assert.equal(usageFreshness({...q,fetched_at:0},now).stale,true);
});
