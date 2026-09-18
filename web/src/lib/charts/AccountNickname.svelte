<script lang="ts">
	import { onMount, tick } from 'svelte';
	let { email, color }: { email: string; color: string } = $props();
	const storageKey = $derived(`garcon.account-nickname.${email.toLowerCase()}`);
	let nickname = $state('');
	let draft = $state('');
	let editing = $state(false);
	let storageError = $state('');
	let input = $state<HTMLInputElement>();
	let trigger = $state<HTMLButtonElement>();

	onMount(() => {
		try { nickname = localStorage.getItem(storageKey) ?? ''; } catch { /* Editing still works for this visit. */ }
		function sync(event: StorageEvent) {
			if (event.storageArea === localStorage && (event.key === storageKey || event.key === null)) nickname = event.newValue ?? '';
		}
		window.addEventListener('storage', sync);
		return () => window.removeEventListener('storage', sync);
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
	function save(event: SubmitEvent) {
		event.preventDefault();
		nickname = draft.trim();
		storageError = '';
		try {
			if (nickname) localStorage.setItem(storageKey, nickname);
			else localStorage.removeItem(storageKey);
		} catch { storageError = 'Nickname could not be saved in this browser. It will last until you reload.'; }
		close();
	}
</script>

<div class="nickname">
	<i style:background={color} aria-hidden="true"></i>
	{#if editing}
		<form onsubmit={save}>
			<input bind:this={input} bind:value={draft} aria-label="Nickname for {email}" placeholder={email} maxlength="40" onkeydown={event => { if (event.key === 'Escape') { event.preventDefault(); close(); } }} />
			<button type="submit">Save</button>
			<button type="button" onclick={close}>Cancel</button>
			<span class="hint">Leave blank to use email</span>
		</form>
	{:else}
		<button class="label" bind:this={trigger} onclick={edit} title="{email} · Click to set a nickname" aria-label="Edit nickname for {email}">{nickname || email}</button>
	{/if}
	{#if storageError}<span class="error" role="status">{storageError}</span>{/if}
</div>

<style>
	.nickname { display: inline-flex; align-items: center; flex-wrap: wrap; gap: 4px 8px; min-width: 0; max-width: 100%; }
	i { width: 8px; height: 8px; border-radius: 2px; flex-shrink: 0; }
	button, input { font: inherit; color: inherit; border-radius: 4px; }
	button { cursor: pointer; background: transparent; border: 1px solid var(--widget-border); padding: 2px 5px; }
	.label { border: 0; padding: 3px 0; text-align: left; overflow-wrap: anywhere; }
	.label:hover { text-decoration: underline; text-underline-offset: 3px; }
	button:focus-visible, input:focus-visible { outline: 2px solid var(--widget-text); outline-offset: 2px; }
	form { display: flex; align-items: center; flex-wrap: wrap; gap: 4px; min-width: 0; }
	input { width: 150px; max-width: 100%; border: 1px solid var(--widget-border); background: var(--widget-bg); padding: 3px 5px; }
	.hint { font-size: 9px; }
	.error { color: var(--claude-color); }
</style>
