// Client identity is independent of the upstream provider and model.
export function harnessName(harness?: string): string {
  return ({ codex: "Codex", claude: "Claude Code", pi: "Pi", hermes: "Hermes", openclaw: "OpenClaw" } as Record<string, string>)[harness || ""] || harness || "Unknown client";
}

export function sessionKey(id: string, harness?: string): string {
  return JSON.stringify([harness || "", id]);
}
