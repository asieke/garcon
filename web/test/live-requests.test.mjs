import assert from 'node:assert/strict';
import test from 'node:test';
import { LiveRequests } from '../src/lib/console/live-requests.ts';

const row = (sequence, fields = {}) => ({ sequence, state: 'complete', input: 10, ...fields });

test('dashboard ticker baselines history, orders arrivals and never replays consumed requests', () => {
  const queue = new LiveRequests();
  queue.update([row(2), row(1)], true, 0);
  assert.equal(queue.next(0), undefined);
  queue.update([row(4), row(2), row(3)], true, 10);
  assert.equal(queue.next(10).sequence, 3);
  assert.equal(queue.next(10).sequence, 4);
  queue.update([row(4), row(2), row(3)], true, 20);
  assert.equal(queue.next(20), undefined);
});

test('tokenless streams can qualify later, but completion never queues a displayed request again', () => {
  const queue = new LiveRequests();
  queue.update([], true, 0);
  const stream = row(1, { state: 'streaming', input: 0 });
  queue.update([stream, row(2, { input: 0, kind: 'request' })], true, 10);
  assert.equal(queue.next(10), undefined);
  queue.update([stream, row(2, { input: 0, kind: 'request' })], true, 60_000);
  queue.update([row(1, { cache_read: 20, input: 0, state: 'streaming' }), row(2, { input: 0 })], true, 60_010);
  assert.equal(queue.next(60_010).sequence, 1);
  queue.update([row(1, { output: 30 }), row(2, { input: 0 })], true, 60_020);
  assert.equal(queue.next(60_020), undefined);
});

test('pending requests take final usage updates and retain cached-only activity', () => {
  const queue = new LiveRequests();
  queue.update([], true, 0);
  queue.update([row(1, { state: 'streaming' }), row(2, { input: 0, cache_write: 4 })], true, 1);
  queue.update([row(1, { output: 42 }), row(2, { input: 0, cache_write: 4 })], true, 2);
  assert.equal(queue.next(2).output, 42);
  assert.equal(queue.next(2).sequence, 2);
});

test('filter changes apply to queued activity without replaying old metadata', () => {
  const queue = new LiveRequests();
  queue.update([], true, 0);
  queue.update([row(1, { input: 0 })], true, 1);
  queue.update([row(1, { input: 0 }), row(2, { input: 0 })], false, 2);
  assert.equal(queue.next(2).sequence, 2);
  queue.update([row(3, { input: 0 })], false, 3);
  queue.update([row(3, { input: 0 })], true, 4);
  assert.equal(queue.next(4), undefined);
});

test('bursts and pauses cannot build stale backlog; a replaced feed baselines again', () => {
  const queue = new LiveRequests();
  queue.update([], true, 0);
  queue.update(Array.from({ length: 100 }, (_, i) => row(i + 1)), true, 1);
  assert.equal(queue.next(1).sequence, 71);
  assert.equal(queue.next(30_002), undefined);
  assert.equal(queue.update([row(1)], true, 30_003), true);
  assert.equal(queue.next(30_003), undefined);
  queue.update([row(1), row(2)], true, 30_004);
  assert.equal(queue.next(30_004).sequence, 2);
});
