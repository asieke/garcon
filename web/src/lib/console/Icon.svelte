<script lang="ts">
  let { name, size = 18 }: { name: string; size?: number } = $props();
  const paths: Record<string, string> = {
    accounts: "M4 7h16M4 17h16M7 4v6m10 4v6M4 12h16",
    sessions: "M8 4H4v16h16v-4M12 4h8v8m0-8L10 14",
    analytics: "M4 20V10m8 10V4m8 16v-7",
    logs: "M8 6h12M8 12h12M8 18h12M3 6h.01M3 12h.01M3 18h.01",
    plus: "M12 5v14M5 12h14",
    chevron: "m9 5 7 7-7 7",
    down: "m6 9 6 6 6-6",
    up: "m6 15 6-6 6 6",
    arrow: "M4 12h16m-6-6 6 6-6 6",
    check: "m5 12 4 4L19 6",
    close: "m6 6 12 12M6 18 18 6",
    refresh:
      "M20 7v5h-5M4 17v-5h5M5.3 7a8 8 0 0 1 13.2-2L20 8M4 16l1.5 3A8 8 0 0 0 19 17",
    copy: "M8 8h12v12H8zM16 4H4v12",
    search: "M21 21l-5-5M18 10a8 8 0 1 1-16 0 8 8 0 0 1 16 0",
    terminal: "m4 6 6 6-6 6m9 0h7",
    database:
      "M20 5c0 2-4 3-8 3S4 7 4 5s4-3 8-3 8 1 8 3ZM4 5v14c0 2 4 3 8 3s8-1 8-3V5M4 12c0 2 4 3 8 3s8-1 8-3",
    link: "m10 13 4-4M8 16l-1 1a4 4 0 0 1-6-6l4-4a4 4 0 0 1 6 0m2 4a4 4 0 0 0 6 0l4-4a4 4 0 0 0-6-6l-1 1",
    pulse: "M2 12h5l3-8 4 16 3-8h5",
    shield: "m12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6z",
    clock: "M12 8v4l3 2M22 12a10 10 0 1 1-20 0 10 10 0 0 1 20 0",
    download: "M12 3v12m-5-5 5 5 5-5M4 16v5h16v-5",
  };
</script>

<svg
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill="none"
  stroke="currentColor"
  stroke-width="1.65"
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"><path d={paths[name] ?? paths.pulse} /></svg
>
