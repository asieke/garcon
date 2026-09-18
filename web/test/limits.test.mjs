import assert from 'node:assert/strict';
import test from 'node:test';
import { elapsedPercent, expired, barPercent, countdown, accountStatus, overviewWindow, widgetGroups, widgetWindows, widgetWindowLabel, widgetReset, resetCreditInfo } from '../src/lib/limits.ts';

const reset = Date.UTC(2030, 0, 8);
const week = { id: 'week', label: 'Weekly', used_percent: 32, window_seconds: 604800, resets_at: reset, expired: false };
test('widget reset labels distinguish absent dates, expired snapshots and active countdowns', () => {
	assert.equal(widgetReset({ ...week, used_percent: 0, resets_at: 0 }, reset).text, '—');
	assert.equal(widgetReset({ ...week, used_percent: 25, resets_at: 0 }, reset).text, '—');
	assert.match(widgetReset({ ...week, resets_at: 0 }, reset).title, /provider has not reported/);
	assert.equal(widgetReset(null, reset).title, 'Usage window unavailable');
	assert.equal(widgetReset(week, reset).text, 'Pending');
	assert.equal(widgetReset({ ...week, expired: true }, reset - 1000).text, 'Pending');
	assert.equal(widgetReset(week, reset - 90000000).text, '1d 1h');
	assert.equal(widgetReset(week, reset - 1).title, new Date(reset).toLocaleString());
});
test('elapsed marker follows provider week and handles reset boundaries', () => {
	assert.equal(elapsedPercent(week, reset - 604800000), 0);
	assert.equal(elapsedPercent(week, reset - 302400000), 50);
	assert.equal(elapsedPercent(week, reset), null);
	assert.equal(expired(week, reset), true);
	assert.equal(elapsedPercent({ ...week, resets_at: 0 }, reset), null);
	assert.equal(elapsedPercent({ ...week, window_seconds: 0 }, reset - 1), null);
});
test('unknown allowance is not reported as zero, and bars stay bounded', () => {
	assert.equal(barPercent(null), 0);
	assert.equal(barPercent(130), 100);
	assert.equal(barPercent(-1), 0);
	assert.equal(countdown(0, reset), 'Reset time unavailable');
	assert.equal(countdown(reset, reset), 'Waiting for the next window');
	assert.equal(countdown(reset, reset - 90000000), 'Resets in 1d 1h');
	assert.equal(countdown(reset, reset - 1), 'Resets in 1m');
});
test('overview chooses weekly window and status flags stale credentials and snapshots', () => {
	const a = { status: 'fresh', fetched_at: reset - 601000, windows: [{ ...week, label: '5-hour' }, week] };
	assert.equal(overviewWindow(a), week);
	assert.equal(accountStatus(a, reset), 'Stale snapshot');
	assert.equal(accountStatus({ ...a, status: 'needs_login' }, reset), 'Login needs refresh');
	assert.equal(accountStatus({ ...a, fetched_at: reset }, reset), 'Up to date');
});

test('widget groups by email without combining separate provider allowances or workspace seats', () => {
	const a = { id: 'a', email: 'person@example.com', provider: 'claude', windows: [week, {...week,id:'5h',label:'5-hour'}] };
	const b = { ...a, id: 'b', provider: 'codex' };
	const c = { ...a, id: 'c' };
	const groups = widgetGroups([a, b, c]);
	assert.equal(groups.length, 1);
	assert.equal(groups[0].accounts.length, 3);
	assert.equal(groups[0].accounts[0].provider, 'codex');
	assert.equal(widgetWindows(a)[0].label, '5-hour');
	assert.deepEqual(widgetWindows({...a, windows: []}), [null]);
	assert.equal(widgetWindowLabel(week, 'claude'), 'Week · all');
	assert.equal(widgetWindowLabel({...week,label:'Fable · Weekly'}, 'claude'), 'Week · Fable');
});

test('reset credits distinguish zero from unknown and stop counting known expirations', () => {
	const account = { provider: 'codex', reset_credits: { available_count: 2, credits: [{ expires_at: reset }, { expires_at: reset + 1000 }], status: 'fresh', fetched_at: reset - 5000 } };
	assert.equal(resetCreditInfo(account, reset - 1).count, 2);
	assert.deepEqual(resetCreditInfo(account, reset), { count: 1, expirations: [reset + 1000], stale: true });
	assert.equal(resetCreditInfo(account, reset + 1000).count, 0);
	assert.equal(resetCreditInfo({ provider: 'codex' }, reset).count, null);
	assert.equal(resetCreditInfo({ ...account, reset_credits: { ...account.reset_credits, available_count: 0, credits: [] } }, reset).count, 0);
});
