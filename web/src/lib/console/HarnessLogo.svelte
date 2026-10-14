<script lang="ts">
  import codex from "$lib/assets/providers/openai-app.png";
  import claude from "$lib/assets/providers/claude-app.png";
  import google from "$lib/assets/providers/google.svg";
  import facebook from "$lib/assets/providers/facebook.svg";
  import Icon from "./Icon.svelte";
  import { harnessName } from "./harness";

  let { harness, size = 24 }: { harness?: string; size?: number } = $props();
  const src = $derived(
    harness === "codex" ? codex
    : harness === "claude" ? claude
    : harness === "agy" || harness === "google" ? google
    : harness === "muse" || harness === "facebook" || harness === "meta" ? facebook
    : undefined
  );
</script>

<span class="harness-logo" title={harnessName(harness)} role="img" aria-label={harnessName(harness)}>
  {#if src}<img {src} width={size} height={size} alt="" />{:else}<Icon name="sessions" {size} />{/if}
</span>

<style>
  .harness-logo { display: inline-flex; align-items: center; flex-shrink: 0; vertical-align: middle; }
  img { display: block; border-radius: 5px; }
</style>
