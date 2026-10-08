import assert from 'node:assert/strict';
import test from 'node:test';
import { RequestQueue, requestProvider, requestTokens } from '../src/lib/ticker.ts';

test('ticker skips initial history and queues new simultaneous requests once in arrival order', () => {
	const queue = new RequestQueue();
	const rows = [1, 2].map(sequence => ({ sequence, time: 123, input: 1 }));
	queue.update(rows);
	assert.equal(queue.next(), undefined);
	queue.update([...rows, { sequence: 3, time: 123, input: 1 }]);
	queue.update([...rows, { sequence: 3, time: 123, input: 1 }, { sequence: 4, time: 123, input: 1 }]);
	assert.equal(queue.next().sequence, 3);
	assert.equal(queue.next().sequence, 4);
	queue.update([...rows, { sequence: 3, time: 123, input: 1 }, { sequence: 4, time: 123, input: 1 }]);
	// Once consumed, a request never returns during later polls or idle frames.
	assert.equal(queue.next(), undefined);
	queue.update([{ sequence: 5, time: 123, input: 1 }]);
	assert.equal(queue.next().sequence, 5);
	assert.equal(queue.next(), undefined);
});

test('ticker bounds backlog and recovers after the local log is replaced', () => {
	const queue = new RequestQueue();
	queue.update([]);
	queue.update(Array.from({ length: 100 }, (_, i) => ({ sequence: i + 1, input: 1 })));
	assert.equal(queue.next().sequence, 71);
	assert.equal(queue.update([{ sequence: 1 }]), true);
	assert.equal(queue.next(), undefined);
	queue.update([{ sequence: 1, input: 1 }, { sequence: 2, input: 1 }]);
	assert.equal(queue.next().sequence, 2);
	queue.update([]);
	assert.equal(queue.next(), undefined);
});

test('reopening the widget baselines existing requests without replaying them', () => {
	const rows = [{ sequence: 100, time: 123 }];
	for (let i = 0; i < 2; i++) {
		const queue = new RequestQueue();
		queue.update(rows);
		queue.update(rows);
		assert.equal(queue.next(), undefined);
	}
});

test('ticker labels the provider subscription and counts cache tokens once', () => {
	const row = { harness: 'hermes', provider: 'chatgpt', input: 1000, cache_read: 40000, cache_write: 1000, output: 3000 };
	assert.equal(requestProvider(row).label, 'Codex');
	assert.equal(requestProvider({ ...row, provider: 'anthropic' }).label, 'Claude');
	assert.equal(requestTokens(row), 45000);
	assert.equal(requestTokens({}), 0);
});

test('ticker hides zero-token completions and shows streams only after tokens arrive', () => {
	const queue = new RequestQueue();
	queue.update([]);
	queue.update([
		{ sequence: 1, state: 'complete', status: 0 },
		{ sequence: 2, state: 'streaming', input: 0 },
		{ sequence: 3, state: 'complete', cache_read: 12 }
	]);
	assert.equal(queue.next().sequence, 3);
	assert.equal(queue.next(), undefined);
	queue.update([
		{ sequence: 1, state: 'complete', status: 0 },
		{ sequence: 2, state: 'complete', output: 8 },
		{ sequence: 3, state: 'complete', cache_read: 12 },
		{ sequence: 4, state: 'failed', status: 426, input: 0 }
	]);
	assert.equal(queue.next().sequence, 2);
	assert.equal(queue.next(), undefined);
	queue.update([{ sequence: 4, state: 'failed', status: 426 }, { sequence: 5, state: 'complete', output: 3 }]);
	assert.equal(queue.next().sequence, 5);
});
