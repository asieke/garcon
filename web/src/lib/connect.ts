/** Configuration recipes for pointing a harness at the proxy. The Settings tab renders these and
 * the add-provider skill mirrors them, so keep the two in step. */

export const PROVIDERS: { key: string; label: string; login: string; host: string; api: string; usage: string; path: string }[] = [
	// Labelled by what reaches them. Claude Code and Codex sign in with OAuth rather than an API key,
	// so plain "Anthropic" and "ChatGPT" would suggest a developer account that is not involved; the
	// Anthropic upstream keeps both names because API-key clients (Hermes, OpenClaw) share it.
	{ key: 'anthropic', label: 'Claude Code / Anthropic API', login: 'Your Claude login, through the Anthropic API; API-key clients work too', host: 'api.anthropic.com', api: 'Messages', usage: 'input / cache read / cache write / output', path: '/v1/messages' },
	{ key: 'openai', label: 'OpenAI', login: 'An OpenAI API key', host: 'api.openai.com', api: 'Responses, chat completions', usage: 'input (cached share) / output', path: '/v1/responses, /v1/chat/completions' },
	{ key: 'openrouter', label: 'OpenRouter', login: 'An OpenRouter API key; one key for many vendors', host: 'openrouter.ai', api: 'Chat completions', usage: 'prompt (cached share) / completion', path: '/api/v1/chat/completions' },
	{ key: 'chatgpt', label: 'Codex', login: 'Your ChatGPT login, through the Codex backend', host: 'chatgpt.com', api: 'Responses', usage: 'input (cached share) / output', path: '/backend-api/codex/responses' }
];

/** Which providers each harness can be pointed at. Claude Code and Codex are single-provider by
 * construction; the others accept any upstream. */
export const HARNESS_PROVIDERS: Record<string, string[]> = {
	claude: ['anthropic'],
	codex: ['chatgpt'],
	openclaw: ['anthropic', 'openai', 'openrouter', 'chatgpt'],
	hermes: ['anthropic', 'openai', 'openrouter', 'chatgpt']
};

export type Snippet = { title: string; text: string; note?: string };

/** The URL a harness must use as its base for this provider. Claude and Codex imply their provider. */
export function baseUrl(base: string, harness: string, provider: string): string {
	if (harness === 'claude') return `${base}/claude`;
	if (harness === 'codex') return `${base}/codex/backend-api/codex`;
	return `${base}/${harness}/${provider}`;
}

export function snippetsFor(base: string, harness: string, provider: string): Snippet[] {
	const url = baseUrl(base, harness, provider);
	switch (harness) {
		case 'claude':
			return [
				{
					title: 'garcon claude (keeps Remote Control)',
					text: 'garcon claude',
					note: 'Runs claude through a private socket so the claude.ai app can attach to the session. Put -- before any claude arguments.'
				},
				{
					title: 'Environment only (no Remote Control)',
					text: `ANTHROPIC_BASE_URL=${url} \\\n_CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL=1 claude`,
					note: 'The second variable keeps claude.ai login working.'
				}
			];
		case 'codex':
			return [
				{
					title: 'Codex custom provider (command line)',
					text: `codex -c model_provider="garcon" -c model_providers.garcon.name="OpenAI" \\\n  -c model_providers.garcon.base_url="${url}" \\\n  -c model_providers.garcon.wire_api="responses" -c model_providers.garcon.requires_openai_auth=true`,
					note: 'A custom provider is required under a ChatGPT login. Only model calls are routed.'
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
					note: 'Keys stay in OpenClaw.'
				}
			];
		}
		case 'hermes': {
			if (provider === 'chatgpt') return [{
				title: 'Hermes with a ChatGPT login',
				text: `HERMES_CODEX_BASE_URL=${url}/backend-api/codex hermes chat --provider openai-codex`,
				note: 'Uses Hermes’ own ChatGPT login. Account identity follows the credentials on each request.'
			}];
			const suffix = provider === 'openai' ? '/v1' : provider === 'openrouter' ? '/api/v1' : '';
			const out: Snippet[] = [
				{
					title: '~/.hermes/config.yaml',
					text: `providers:\n  ${provider}:\n    base_url: ${url}${suffix}`,
					note: provider === 'anthropic'
						? 'For api_mode anthropic_messages.'
						: 'Hermes requests include_usage, so tokens are counted.'
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
					note: 'Streamed replies need stream_options: {"include_usage": true} for token counts.'
				}
			];
	}
}
