<script lang="ts">
	/** Multi-select filter: a button that opens a listbox of checkable options. Nothing selected
	 * means "all". Keyboard: Enter, Space or the arrow keys open it; arrows, Home and End move;
	 * Space or Enter toggle; typing jumps to a matching option; Escape closes; Tab moves on. */
	import { tick } from 'svelte';

	type Option = { value: string; label: string };
	let {
		id,
		label,
		allLabel = 'All',
		options,
		selected = $bindable<string[]>([])
	}: { id: string; label: string; allLabel?: string; options: Option[]; selected?: string[] } = $props();

	let open = $state(false);
	let active = $state(0); // 0 is the "All" row, then options in order
	let root: HTMLDivElement;
	let button: HTMLButtonElement;
	let list: HTMLDivElement | undefined = $state();

	const chosen = $derived(new Set(selected));
	const summary = $derived.by(() => {
		if (!selected.length) return allLabel;
		const labels = options.filter((o) => chosen.has(o.value)).map((o) => o.label);
		return labels.length <= 2 ? labels.join(', ') : `${labels.length} of ${options.length}`;
	});

	function toggle(value: string) {
		selected = chosen.has(value) ? selected.filter((v) => v !== value) : [...selected, value];
	}
	async function show(at = 0) {
		open = true;
		active = at;
		await tick();
		list?.focus();
	}
	function hide(refocus = true) {
		open = false;
		if (refocus) button?.focus();
	}
	function activate() {
		if (active === 0) selected = [];
		else toggle(options[active - 1].value);
	}
	function onButtonKey(e: KeyboardEvent) {
		if (e.key === 'ArrowDown' || e.key === 'ArrowUp' || e.key === 'Enter' || e.key === ' ') {
			e.preventDefault();
			show(e.key === 'ArrowUp' ? options.length : 0);
		}
	}
	function onListKey(e: KeyboardEvent) {
		switch (e.key) {
			case 'ArrowDown':
				active = Math.min(active + 1, options.length);
				break;
			case 'ArrowUp':
				active = Math.max(active - 1, 0);
				break;
			case 'Home':
				active = 0;
				break;
			case 'End':
				active = options.length;
				break;
			case ' ':
			case 'Enter':
				activate();
				break;
			case 'Escape':
				hide();
				break;
			case 'Tab':
				hide(false);
				return;
			default: {
				if (e.key.length !== 1 || e.altKey || e.ctrlKey || e.metaKey) return;
				const k = e.key.toLowerCase();
				const n = options.length;
				for (let step = 1; step <= n; step++) {
					const i = (active - 1 + step) % n;
					if (options[i].label.toLowerCase().startsWith(k)) {
						active = i + 1;
						break;
					}
				}
			}
		}
		e.preventDefault();
	}
	function onDocumentClick(e: MouseEvent) {
		if (open && !root.contains(e.target as Node)) open = false;
	}
	$effect(() => {
		if (open) document.getElementById(`${id}-opt-${active}`)?.scrollIntoView({ block: 'nearest' });
	});
</script>

<svelte:document onclick={onDocumentClick} />

<div class="ms" bind:this={root}>
	<span class="ms-label" id="{id}-label">{label}</span>
	<button
		type="button"
		bind:this={button}
		aria-haspopup="listbox"
		aria-expanded={open}
		aria-labelledby="{id}-label {id}-value"
		id="{id}-value"
		class:filtered={selected.length > 0}
		onclick={() => (open ? hide() : show())}
		onkeydown={onButtonKey}
	>
		<span class="ms-value">{summary}</span>
		<svg class="ms-chevron" aria-hidden="true" viewBox="0 0 10 6" width="10" height="6"><path d="M1 1l4 4 4-4" fill="none" stroke="currentColor" stroke-width="1.5" /></svg>
	</button>
	{#if open}
		<div
			class="ms-list"
			role="listbox"
			aria-multiselectable="true"
			aria-labelledby="{id}-label"
			aria-activedescendant="{id}-opt-{active}"
			tabindex="-1"
			bind:this={list}
			onkeydown={onListKey}
		>
			<!-- Keys are handled once on the listbox (aria-activedescendant); options are pointer targets only. -->
			<!-- svelte-ignore a11y_click_events_have_key_events -->
			<div role="option" tabindex="-1" id="{id}-opt-0" aria-selected={selected.length === 0} class:active={active === 0} onclick={() => (selected = [])} onmousemove={() => (active = 0)}>
				<i class="check" class:on={selected.length === 0}></i>{allLabel}
			</div>
			{#each options as o, i (o.value)}
				<!-- svelte-ignore a11y_click_events_have_key_events -->
				<div role="option" tabindex="-1" id="{id}-opt-{i + 1}" aria-selected={chosen.has(o.value)} class:active={active === i + 1} onclick={() => toggle(o.value)} onmousemove={() => (active = i + 1)}>
					<i class="check" class:on={chosen.has(o.value)}></i>{o.label}
				</div>
			{/each}
		</div>
	{/if}
</div>

<style>
	.ms {
		position: relative;
		display: flex;
		flex-direction: column;
		gap: 6px;
		flex: 1;
		min-width: 140px;
		max-width: 280px;
	}
	.ms-label {
		color: var(--text-secondary);
		font-size: 11px;
		font-weight: 550;
	}
	button {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
		width: 100%;
		min-height: 36px;
		padding: 7px 10px 7px 12px;
		font: inherit;
		font-size: 12px;
		text-align: left;
		color: var(--text-primary);
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: 6px;
		cursor: pointer;
	}
	button.filtered {
		border-color: var(--accent-border);
		background: var(--accent-soft);
	}
	button:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}
	.ms-value {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.ms-chevron {
		flex: none;
		color: var(--text-muted);
	}
	.ms-list {
		position: absolute;
		top: 100%;
		left: 0;
		z-index: 20;
		min-width: 100%;
		max-height: 280px;
		overflow-y: auto;
		margin-top: 4px;
		padding: 4px;
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: 8px;
		box-shadow: 0 10px 30px rgba(0, 0, 0, 0.18);
		outline: none;
	}
	[role='option'] {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 7px 10px;
		font-size: 12px;
		border-radius: 5px;
		cursor: pointer;
		white-space: nowrap;
	}
	[role='option'].active {
		background: var(--nav-hover);
	}
	[role='option'][aria-selected='true'] {
		color: var(--text-primary);
	}
	.check {
		flex: none;
		width: 13px;
		height: 13px;
		border: 1px solid var(--baseline);
		border-radius: 3px;
		background: var(--page);
		position: relative;
	}
	.check.on {
		border-color: var(--accent);
		background: var(--accent);
	}
	.check.on::after {
		content: '';
		position: absolute;
		left: 4px;
		top: 1px;
		width: 3px;
		height: 7px;
		border: solid var(--surface);
		border-width: 0 2px 2px 0;
		transform: rotate(45deg);
	}
	@media (max-width: 760px) {
		.ms {
			min-width: 0;
			max-width: none;
			width: 100%;
			flex-basis: 100%;
		}
	}
</style>
