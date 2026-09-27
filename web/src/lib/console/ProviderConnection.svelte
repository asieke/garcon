<script lang="ts">
  import Icon from "./Icon.svelte";

  let { provider, connection, available, launchBased = false, onclick }: {
    provider: string;
    launchBased?: boolean;
    connection?: { connected: boolean | null; mode?: string; profiles?: number; remote_control_at_startup?: boolean };
    available: boolean;
    onclick: () => void;
  } = $props();

  const known = $derived(available && typeof connection?.connected === "boolean");
  const isConnected = $derived(known && connection?.connected === true);
  const socketConfigured = $derived(isConnected && connection?.mode === "socket");
  const label = $derived(!known ? "Connection unavailable" : socketConfigured
    ? "CLI launchers configured"
    : isConnected ? (launchBased ? "Default gateway configured" : "Connected")
    : "Not connected");
  const detail = $derived(socketConfigured
    ? `${connection?.profiles || 1} Claude profile launcher(s) route through Garcon. This does not configure Claude Desktop. Use garcon claude rc to explicitly start Remote Control.`
    : launchBased && known
      ? (isConnected ? "The default profile's saved HTTP gateway points to Garcon. Start a new Claude Code session and verify its request in Logs. Remote Control is opt-in with garcon claude rc." : "The default Claude profile does not point to Garcon. Open setup to configure routing; use garcon claude rc for an explicit remote session.")
      : !known ? `Could not check the saved ${provider} configuration.`
      : isConnected ? `Your default ${provider} configuration points to Garcon. Restart ${provider} after changing its configuration.`
      : `Your default ${provider} configuration does not point to Garcon. Open setup to connect.`);
</script>

<button
  class="filter-button provider-connection"
  class:connected={isConnected}
  title={`${detail} Click to view ${provider} setup.`}
  aria-label={`${provider}: ${label}. ${detail} Open setup`}
  {onclick}
>
  {#if isConnected}
    <span class="connected-dot" aria-hidden="true"></span>
  {:else}
    <Icon name="link" size={14} />
  {/if}
  {label}
</button>

<style>
  .provider-connection { display: inline-flex; align-items: center; gap: 7px; white-space: nowrap; }
  .provider-connection.connected { color: #17643b; border-color: #9ecdb0; background: #f1faf4; }
  .connected-dot { width: 8px; height: 8px; border-radius: 50%; background: #16803d; flex-shrink: 0; }
</style>
