import assert from 'node:assert/strict';
import test from 'node:test';
import { elapsedPercent, expired, barPercent, countdown, accountStatus, overviewWindow, widgetGroups, widgetProviderGroups, widgetUsage, widgetShortLabel, needsAttention, widgetWindows, widgetWindowLabel, widgetReset, resetCreditInfo, claudeResetDetails } from '../src/lib/limits.ts';

const reset = Date.UTC(2030, 0, 8);
const week = { id: 'week', label: 'Weekly', used_percent: 32, window_seconds: 604800, resets_at: reset, expired: false };
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
	// A Muse reading kept between sessions is dated, not an error.
	assert.equal(accountStatus({ ...a, status: 'idle' }, reset), 'Last reading');
	assert.equal(needsAttention({ ...a, status: 'idle' }, reset), false);
	assert.equal(needsAttention(a, reset), true);
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


test('compact reset display preserves unknown, pending, and active states', () => {
 assert.deepEqual(widgetReset(null, reset), { text: '—', description: 'Usage window unavailable' });
 for (const used_percent of [0, 25, null]) {
  const display = widgetReset({ ...week, used_percent, resets_at: 0 }, reset);
  assert.equal(display.text, '—');
  assert.match(display.description, /provider has not reported/);
 }
 assert.equal(widgetReset(week, reset).text, 'Pending');
 assert.equal(widgetReset({ ...week, expired: true }, reset - 1000).text, 'Pending');
 assert.equal(widgetReset(week, reset - 90000000).text, '1d 1h');
 assert.equal(widgetReset(week, reset - 1).description, `Resets ${new Date(reset).toLocaleString()}`);
});

test('Claude saved resets keep count, expiry and coverage together without attention for missing offers', () => {
	const a = { provider: 'claude', status: 'fresh', fetched_at: reset, reset_credits: {
		available_count: 4, status: 'fresh', fetched_at: reset,
		credits: [{ count: 2, expires_at: reset, limits: ['five_hour'] }, { count: 1, expires_at: reset + 1000, limits: ['five_hour', 'seven_day', 'seven_day_overage_included'] }, { count: 1, expires_at: 0 }]
	} };
	assert.equal(resetCreditInfo(a, reset - 1).count, 4);
	assert.equal(resetCreditInfo(a, reset).count, 2);
	assert.deepEqual(claudeResetDetails(a, reset), [
		{ count: 1, expiresAt: reset + 1000, limits: '5h + week + included models' },
		{ count: 1, expiresAt: 0, limits: 'limits not reported' }
	]);
	assert.deepEqual(claudeResetDetails({ ...a, reset_credits: undefined }, reset), []);
	assert.equal(needsAttention({ ...a, reset_credits: undefined }, reset), false);
	assert.deepEqual(claudeResetDetails({ ...a, provider: 'codex' }, reset), []);
	assert.deepEqual(claudeResetDetails({ ...a, reset_credits: { ...a.reset_credits, available_count: 0, credits: [] } }, reset), []);
});

test('provider widget groups order Codex, Claude, Google, and Facebook and map compact cells', () => {
 const accounts = [
  { id: 'muse-1', provider: 'muse', email: 'm@example.com', windows: [week] },
  { id: 'agy-1', provider: 'agy', email: 'g@example.com', windows: [week, { ...week, id: '3p', label: 'Claude & GPT · Weekly' }] },
  { id: 'claude-b', provider: 'claude', email: 'B@example.com', windows: [week, { ...week, id: '5h', label: '5-hour' }, { ...week, id: 'fable', label: 'Fable · Weekly' }] },
  { id: 'codex-b', provider: 'codex', email: 'b@example.com', windows: [week] },
  { id: 'claude-a2', provider: 'claude', email: 'a@example.com', windows: [] },
  { id: 'claude-a1', provider: 'claude', email: 'a@example.com', windows: [week] },
 ];
 const groups = widgetProviderGroups(accounts);
 assert.deepEqual(groups.map(group => group.label), ['Codex', 'Claude', 'Google', 'Facebook']);
 assert.deepEqual(groups[1].accounts.map(account => account.id), ['claude-a1', 'claude-a2', 'claude-b']);
 assert.equal(groups[1].accounts[0], accounts[5]);
 // One bar for the weekly allowance; every other window becomes short text, 5-hour first.
 const claude = widgetUsage(accounts[2]);
 assert.equal(claude.primary.label, 'Weekly');
 assert.deepEqual(claude.extras.map(e => e.label), ['5h', 'Fable']);
 const agy = widgetUsage({ ...accounts[1], windows: [...accounts[1].windows, { ...week, id: '3p5', label: 'Claude & GPT · 5-hour' }, { ...week, id: 'g5', label: '5-hour' }] });
 assert.deepEqual(agy.extras.map(e => e.label), ['5h', '3P', '3P 5h']);
 assert.equal(widgetUsage({ ...accounts[0], windows: [{ ...week, label: '5-hour' }] }).primary.label, '5-hour');
 assert.deepEqual(widgetUsage({ ...accounts[0], windows: [] }), { primary: null, extras: [] });
 assert.equal(widgetShortLabel('Weekly'), 'Week');
 assert.deepEqual(widgetProviderGroups([]), []);
 assert.deepEqual(widgetProviderGroups([accounts[2]]).map(group => group.key), ['claude']);
});
