/** Every OAuth connection drawer uses an agent prompt instead of manual config steps. */
const recipes = {
  codex: {
    name: "Codex",
    location: "$CODEX_HOME/config.toml when CODEX_HOME is set, otherwise ~/.codex/config.toml",
    merge: "Set model_provider at the TOML root and merge the model_providers.garcon table. Update existing keys in place; do not append duplicate keys or tables.",
    settings: `model_provider = "garcon"

[model_providers.garcon]
name = "Garcon"
base_url = "http://127.0.0.1:4141/codex/backend-api/codex"
wire_api = "responses"
requires_openai_auth = true
supports_websockets = false`,
    restart: "Restart Codex Desktop or start a new Codex CLI session",
  },

} as const;

export function connectionPrompt(client: "codex" | "claude"): string {
  if (client === "claude") return `Configure Claude Code to route model requests through my local Garcon service, with Remote Control enabled only when I explicitly request it.

1. Find the user settings for the Claude profile I actually use: $CLAUDE_CONFIG_DIR/settings.json when set, otherwise ~/.claude/settings.json. Local Claude Desktop Code sessions normally use the default profile. Keep named profiles and their logins separate. Never copy or print tokens.
2. Back up settings before editing and preserve unrelated preferences and permissions. Merge env.ANTHROPIC_BASE_URL = "http://127.0.0.1:4141/claude" and env._CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL = "1" for ordinary HTTP routing. Set remoteControlAtStartup to false in user settings. Do not change unrelated gateways without asking.
3. For an explicit Remote Control session, use garcon claude rc. With no profile argument it uses $CLAUDE_CONFIG_DIR, otherwise ~/.claude. To choose another profile, use garcon claude rc --config-dir /absolute/profile/path. Pass Claude arguments after --. This command uses a private per-session socket; never persist ANTHROPIC_UNIX_SOCKET. Do not enable Remote Control automatically or silently choose a different login.
4. Existing named CLI launchers can use garcon claude --config-dir /absolute/profile/path -- followed by Claude arguments. Preserve direct auth commands and avoid recursive wrappers. If configuring a launcher, verify it and register it with garcon claude --config-dir /absolute/profile/path --register-launcher /absolute/launcher/path. Registration verifies only the launcher, not Desktop or a live remote connection.
5. Parse the saved settings and check http://127.0.0.1:4141. Report an unavailable service without switching ports or starting another service. Port 4242 is only the dashboard preview. Start a new Claude Code session to load settings and verify its model request in Garcon Logs.
6. Report changed files, backups, and exact commands. Configuration or --version is not proof of live traffic or Remote Control. Only claim Remote Control connected after observing an interactive connection; report login, workspace trust, or account policy blockers without switching profiles.`;

  const recipe = recipes[client];
  return `Configure ${recipe.name} on this computer to route model requests through my local Garcon service, using my existing OAuth login. Please make the config change for me.

1. Find ${recipe.location}. Resolve the path for the client I actually launch, and report any profile, project, or shell overrides that would take precedence.
2. Back up the existing file before editing. If it is missing, create it and its parent directory. If it cannot be parsed, stop and explain instead of overwriting it. Preserve unrelated preferences, model selection, credentials, and file permissions. Do not read or print auth tokens or add API keys.
3. ${recipe.merge} Apply these settings:

${recipe.settings}

4. Parse the saved file to validate its syntax and confirm the selected Garcon endpoint. Check whether http://127.0.0.1:4141 is reachable; if it is unavailable, report that separately without changing ports or starting another service. Port 4242 is only the dashboard preview.
5. Summarize the changed file, backup location, and verification results without exposing secrets. ${recipe.restart}. Saved configuration is not proof of live traffic: explain how to confirm a new request in Garcon's Logs after restarting, and only claim traffic is verified if you observed it.`;
}
