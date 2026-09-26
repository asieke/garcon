<script lang="ts">
  import appIcon from "$lib/assets/providers/openai-app.png";

  let { size = 32 }: { size?: number } = $props();
</script>

<!-- Original OpenAI app icon matching the requested reference. See ./ASSETS.md. -->
<img src={appIcon} width={size} height={size} alt="" aria-hidden="true" />

<style>
  img {
    display: block;
    flex-shrink: 0;
  }
</style>
