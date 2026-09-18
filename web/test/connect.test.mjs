import assert from 'node:assert/strict';
import test from 'node:test';
import { baseUrl, snippetsFor } from '../src/lib/connect.ts';

const base = 'http://127.0.0.1:4141';
test('supported harness recipes default to account-free URLs', () => {
	assert.equal(baseUrl(base, 'claude', 'anthropic'), `${base}/claude`);
	assert.equal(baseUrl(base, 'codex', 'chatgpt'), `${base}/codex/backend-api/codex`);
	assert.equal(baseUrl(base, 'hermes', 'openrouter'), `${base}/hermes/openrouter`);
	assert.equal(snippetsFor(base, 'claude', 'anthropic')[0].text, 'garcon claude');
	assert.equal(snippetsFor(base, 'hermes', 'chatgpt')[0].text,
		`HERMES_CODEX_BASE_URL=${base}/hermes/chatgpt/backend-api/codex hermes chat --provider openai-codex`);
	for (const [harness, provider] of [['claude', 'anthropic'], ['codex', 'chatgpt'], ['hermes', 'openai']]) {
		assert.ok(snippetsFor(base, harness, provider).every(s => !s.text.includes('me@example.com')));
	}
});
test('all harness recipes are account-free', () => {
 assert.equal(baseUrl(base, 'openclaw', 'openai'), `${base}/openclaw/openai`);
 assert.equal(baseUrl(base, 'myagent', 'anthropic'), `${base}/myagent/anthropic`);
 assert.ok(snippetsFor(base, 'openclaw', 'openai')[0].text.includes(`${base}/openclaw/openai/v1`));
 assert.equal(snippetsFor(base, 'myagent', 'openrouter')[0].text, `${base}/myagent/openrouter/api/v1`);
});
