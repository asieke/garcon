import assert from 'node:assert/strict';
import test from 'node:test';
import { claudeRows } from '../src/lib/console/claude.ts';

const now = 1_800_000_000_000;
const account = { id: 'claude:one', provider: 'claude', email: 'person@example.test', plan: 'max', status: 'fresh', fetched_at: now,
  windows: [{id:'weekly_all:Weekly', label:'Weekly', used_percent:25, resets_at:now+3_600_000, expired:false, window_seconds:604800}] };
test('Claude quota uses the shared row contract without inventing pooling or scores', () => {
  const [row] = claudeRows([account, {...account, provider:'codex'}], now);
  assert.equal(row.email, account.email);
  assert.equal(row.remaining_percent, 75);
  assert.equal(row.hours_left, 1);
  assert.equal(row.enrolled, false);
  assert.equal(row.score, null);
  assert.equal(claudeRows([account, {...account, provider:'codex'}], now).length, 1);
});
test('missing, stale and expired Claude quota never becomes available capacity', () => {
  for (const a of [{...account,status:'stale'}, {...account,fetched_at:now-600001}, {...account,windows:[]},
    {...account,windows:[{...account.windows[0],used_percent:null}]},
    {...account,windows:[{...account.windows[0],expired:true}]}]) {
    assert.equal(claudeRows([a], now)[0].remaining_percent, null);
  }
  const [empty] = claudeRows([{...account, windows:[{...account.windows[0],used_percent:100}]}], now);
  assert.equal(empty.remaining_percent, 0);
  assert.equal(empty.status, 'Usage exhausted');
});
