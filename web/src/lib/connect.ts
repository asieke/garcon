/** Configuration recipes for pointing a harness at the proxy. The Settings tab renders these and
 * the add-provider skill mirrors them, so keep the two in step. */

export const PROVIDERS: { key: string; label: string; host: string; api: string; usage: string; path: string }[] = [
	{ key: 'anthropic', label: 'Anthropic', host: 'api.anthropic.com', api: 'Messages', usage: 'input / cache read / cache write / output', path: '/v1/messages' },
	{ key: 'openai', label: 'OpenAI', host: 'api.openai.com', api: 'Responses, chat completions', usage: 'input (cached share) / output', path: '/v1/responses, /v1/chat/completions' },
	{ key: 'openrouter', label: 'OpenRouter', host: 'openrouter.ai', api: 'Chat completions', usage: 'prompt (cached share) / completion', path: '/api/v1/chat/completions' },
	{ key: 'chatgpt', label: 'ChatGPT (Codex backend)', host: 'chatgpt.com', api: 'Responses', usage: 'input (cached share) / output', path: '/backend-api/codex/responses' }
];

/** Which providers each harness can be pointed at. Claude Code and Codex are single-provider by
 * construction; the others accept any upstream. */
export const HARNESS_PROVIDERS: Record<string, string[]> = {
	claude: ['anthropic'],
	codex: ['chatgpt'],
	openclaw: ['anthropic', 'openai', 'openrouter', 'chatgpt'],
	hermes: ['anthropic', 'openai', 'openrouter']
};

export type Snippet = { title: string; text: string; note?: string };

/** The URL a harness must use as its base for this provider. Claude and Codex imply their provider. */
export function baseUrl(base: string, harness: string, account: string, provider: string): string {
	const acct = account || 'me@example.com';
	if (harness === 'claude') return `${base}/claude/${acct}`;
	if (harness === 'codex') return `${base}/codex/${acct}/backend-api/codex`;
	return `${base}/${harness}/${acct}/${provider}`;
}

export function snippetsFor(base: string, harness: string, provider: string, account: string): Snippet[] {
	const url = baseUrl(base, harness, account, provider);
	switch (harness) {
		case 'claude':
			return [
				{
					title: 'Environment for claude',
					text: `ANTHROPIC_BASE_URL=${url} \\\n_CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL=1 claude`,
					note: 'Claude Code appends /v1/messages itself. The second variable keeps subscription (claude.ai) login working through a non-Anthropic host.'
				}
			];
		case 'codex':
			return [
				{
					title: 'Codex custom provider (command line)',
					text: `codex -c model_provider="garcon" -c model_providers.garcon.name="OpenAI" \\\n  -c model_providers.garcon.base_url="${url}" \\\n  -c model_providers.garcon.wire_api="responses" -c model_providers.garcon.requires_openai_auth=true`,
					note: 'The built-in provider ignores base URL overrides under a ChatGPT login; a custom provider also uses plain HTTP rather than websockets, which is what lets the proxy read usage. Only model calls are routed.'
				},
				{
					title: 'Same thing in ~/.codex/config.toml',
					text: `model_provider = "garcon"\n\n[model_providers.garcon]\nname = "OpenAI"\nbase_url = "${url}"\nwire_api = "responses"\nrequires_openai_auth = true`
				}
			];
		case 'openclaw': {
			const key = provider === 'chatgpt' ? 'openai-codex' : provider;
			const suffix = provider === 'openai' ? '/v1' : provider === 'openrouter' ? '/api/v1' : provider === 'chatgpt' ? '/backend-api' : '';
			return [
				{
					title: '~/.openclaw/openclaw.json',
					text: `{\n  models: {\n    mode: "merge",\n    providers: {\n      ${key}: { baseUrl: "${url}${suffix}" },\n    },\n  },\n}`,
					note: 'Each OpenClaw adapter appends its own endpoint path (/v1/messages, /responses, /chat/completions). Keys stay in OpenClaw; the proxy forwards Authorization and x-api-key untouched.'
				}
			];
		}
		case 'hermes': {
			const suffix = provider === 'openai' ? '/v1' : provider === 'openrouter' ? '/api/v1' : '';
			const out: Snippet[] = [
				{
					title: '~/.hermes/config.yaml',
					text: `providers:\n  ${provider}:\n    base_url: ${url}${suffix}`,
					note: provider === 'anthropic'
						? 'Hermes uses the Anthropic SDK for api_mode anthropic_messages and appends /v1/messages itself.'
						: 'Hermes appends /chat/completions and requests stream_options.include_usage, which is what lets the proxy count tokens.'
				}
			];
			if (provider === 'openai') out.push({ title: 'One-off, without editing config', text: `OPENAI_BASE_URL=${url}${suffix} hermes` });
			return out;
		}
		default:
			return [
				{
					title: 'Base URL for any OpenAI- or Anthropic-compatible client',
					text: url + (provider === 'openai' ? '/v1' : provider === 'openrouter' ? '/api/v1' : provider === 'chatgpt' ? '/backend-api' : ''),
					note: 'Rows are tagged with this harness name. OpenAI-compatible clients only get token counts on streamed replies when they send stream_options: {"include_usage": true}.'
				}
			];
	}
}
