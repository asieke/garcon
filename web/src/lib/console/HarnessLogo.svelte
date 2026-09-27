<script lang="ts">
  import codex from "$lib/assets/providers/openai-app.png";
  import claude from "$lib/assets/providers/claude-app.png";
  import Icon from "./Icon.svelte";
  import { harnessName } from "./harness";

  let { harness, size = 24 }: { harness?: string; size?: number } = $props();
  const src = $derived(harness === "codex" ? codex : harness === "claude" ? claude : undefined);
</script>

<span class="harness-logo" title={harnessName(harness)} role="img" aria-label={harnessName(harness)}>
  {#if src}<img {src} width={size} height={size} alt="" />{:else}<Icon name="sessions" {size} />{/if}
</span>

<style>
  .harness-logo { display: inline-flex; align-items: center; flex-shrink: 0; vertical-align: middle; }
  img { display: block; border-radius: 5px; }
</style>
