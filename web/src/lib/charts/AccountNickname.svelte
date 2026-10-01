<script lang="ts">
  import { onMount, tick } from "svelte";
  let { email, color = "", accountKey, editable = true }: { email: string; color?: string; accountKey?: string; editable?: boolean } = $props();
  const key = $derived(accountKey || email);
  const storageKey = $derived(`garcon.account-nickname.${email.toLowerCase()}`);
  let nickname = $state("");
  let draft = $state("");
  let editing = $state(false);
  let storageError = $state("");
  let input = $state<HTMLInputElement>();
  let trigger = $state<HTMLButtonElement>();

  async function persist(value: string) {
    const res = await fetch(
      `/api/nickname?account=${encodeURIComponent(key)}`,
      {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ nickname: value }),
      },
    );
    if (!res.ok) throw new Error("save failed");
  }
  onMount(() => {
    let alive = true;
    void (async () => {
      try {
        let res = await fetch(
          `/api/nickname?account=${encodeURIComponent(key)}`,
        );
        if (!res.ok) throw new Error("read failed");
        let data = await res.json();
        // Preserve existing email nicknames until this workspace has its own.
        if (!data.exists && key !== email) {
          res = await fetch(`/api/nickname?account=${encodeURIComponent(email)}`);
          if (!res.ok) throw new Error("read failed");
          data = await res.json();
        }
        if (!alive) return;
        nickname = data.nickname;
        if (!data.exists) {
          const previous = localStorage.getItem(storageKey);
          if (previous) {
            await persist(previous);
            if (alive) nickname = previous;
            localStorage.removeItem(storageKey);
          }
        }
      } catch {
        if (alive) storageError = "Could not load the saved nickname.";
      }
    })();
    return () => {
      alive = false;
    };
  });

  async function edit() {
    draft = nickname;
    editing = true;
    await tick();
    input?.focus();
    input?.select();
  }
  async function close() {
    editing = false;
    await tick();
    trigger?.focus();
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    storageError = "";
    const value = draft.trim();
    try {
      await persist(value);
      nickname = value;
      await close();
    } catch {
      storageError = "Could not save the nickname to the local database.";
    }
  }
</script>

<div class="nickname">
  {#if color}<i style:background={color} aria-hidden="true"></i>{/if}
  {#if editing}
    <form onsubmit={save}>
      <input
        bind:this={input}
        bind:value={draft}
        aria-label="Nickname for {email}"
        placeholder={email}
        maxlength="40"
        onkeydown={(event) => {
          if (event.key === "Escape") {
            event.preventDefault();
            close();
          }
        }}
      />
      <button type="submit">Save</button>
      <button type="button" onclick={close}>Cancel</button>
      <span class="hint">Leave blank to use email</span>
    </form>
  {:else if !editable}
    <span title={email}>{nickname || email}</span>
  {:else}
    <button
      class="label"
      bind:this={trigger}
      onclick={edit}
      title="{email} · Click to set a nickname"
      aria-label="Edit nickname for {email}">{nickname || email}</button
    >
  {/if}
  {#if storageError}<span class="error" role="status">{storageError}</span>{/if}
</div>

<style>
  .nickname {
    display: inline-flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 4px 8px;
    min-width: 0;
    max-width: 100%;
  }
  i {
    width: 8px;
    height: 8px;
    border-radius: 2px;
    flex-shrink: 0;
  }
  button,
  input {
    font: inherit;
    color: inherit;
    border-radius: 4px;
  }
  button {
    cursor: pointer;
    background: transparent;
    border: 1px solid var(--widget-border);
    padding: 2px 5px;
  }
  .label {
    border: 0;
    padding: 3px 0;
    text-align: left;
    overflow-wrap: anywhere;
  }
  .label:hover {
    text-decoration: underline;
    text-underline-offset: 3px;
  }
  button:focus-visible,
  input:focus-visible {
    outline: 2px solid var(--widget-text);
    outline-offset: 2px;
  }
  form {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 4px;
    min-width: 0;
  }
  input {
    width: 150px;
    max-width: 100%;
    border: 1px solid var(--widget-border);
    background: var(--widget-bg);
    padding: 3px 5px;
  }
  .hint {
    font-size: 9px;
  }
  .error {
    color: var(--claude-color);
  }
</style>
