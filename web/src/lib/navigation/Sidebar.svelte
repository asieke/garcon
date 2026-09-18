<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import { groupsFor, type ViewId } from './views';
	let { active, syncEnabled = false, onnavigate }: { active: ViewId; syncEnabled?: boolean; onnavigate: () => void } = $props();
	const groups = $derived(groupsFor(syncEnabled));
	const storageKey = 'garcon.sidebar.collapsed';
	let collapsed = $state<string[]>([]);
	let ready = false;
	function persist() {
		try { localStorage.setItem(storageKey, JSON.stringify(collapsed)); } catch { /* Storage can be disabled. */ }
	}
	function reveal(view: ViewId) {
		const group = groups.find(g => g.items.some(i => i.id === view));
		if (group && collapsed.includes(group.id)) { collapsed = collapsed.filter(id => id !== group.id); persist(); }
	}
	function toggle(id: string) {
		collapsed = collapsed.includes(id) ? collapsed.filter(g => g !== id) : [...collapsed, id];
		persist();
	}
	onMount(() => {
		try {
			const saved: unknown = JSON.parse(localStorage.getItem(storageKey) ?? '[]');
			if (Array.isArray(saved)) collapsed = saved.filter(id => typeof id === 'string' && groups.some(g => g.id === id));
		} catch { /* Invalid or unavailable storage uses expanded groups. */ }
		reveal(active); ready = true;
	});
	$effect(() => {
		const view = active;
		untrack(() => { if (ready) reveal(view); });
	});
</script>

<nav aria-label="Main navigation">
	{#each groups as group (group.id)}
		<section class:administration={group.label === 'Administration'} aria-label={group.label}>
			<h2><button type="button" class="group-toggle" onclick={() => toggle(group.id)} aria-expanded={!collapsed.includes(group.id)} aria-controls={'nav-' + group.id}><span>{group.label}</span><svg class="chevron" class:collapsed={collapsed.includes(group.id)} width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m4 6 4 4 4-4" /></svg></button></h2>
			<div id={'nav-' + group.id} hidden={collapsed.includes(group.id)}>
			{#each group.items as item}
				<a href={'?view=' + item.id} aria-current={active === item.id ? 'page' : undefined} onclick={onnavigate}>
					<span class="marker" aria-hidden="true"></span>{item.label}
				</a>
			{/each}
			</div>
		</section>
	{/each}
</nav>

<style>
	nav { display: flex; flex-direction: column; gap: 16px; min-height: 100%; }
	h2 { margin: 0 0 4px; }
	.group-toggle { display: flex; align-items: center; justify-content: space-between; width: 100%; min-height: 26px; font-size: 10px; font-weight: 600; line-height: 14px; letter-spacing: .08em; text-transform: uppercase; color: var(--text-muted); padding: 5px 10px; border: 1px solid transparent; border-radius: 6px; background: transparent; cursor: pointer; }
	.group-toggle:hover { color: var(--text-primary); background: var(--nav-hover); }
	.group-toggle:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
	.chevron { display: block; flex-shrink: 0; opacity: .65; transform: rotate(0); }
	.chevron.collapsed { transform: rotate(-90deg); }
	a { display: flex; gap: 10px; align-items: center; min-height: 32px; padding: 6px 10px; color: var(--text-secondary); text-decoration: none; border-radius: 6px; font-size: 12px; line-height: 18px; font-weight: 450; border: 1px solid transparent; }
	a:hover { background: var(--nav-hover); color: var(--text-primary); }
	a:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
	a[aria-current] { color: var(--accent); background: var(--accent-soft); border-color: var(--accent-border); font-weight: 600; }
	.marker { width: 5px; height: 5px; flex-shrink: 0; border: 1px solid currentColor; border-radius: 2px; opacity: .45; }
	a[aria-current] .marker { background: currentColor; opacity: 1; }
	.administration { margin-top: auto; padding-top: 12px; border-top: 1px solid var(--border); }
	@media (max-width: 760px) { a { min-height: 40px; } .group-toggle { min-height: 36px; } }
</style>
