<script lang="ts">
	import { groupsFor, type ViewId } from './views';
	let { active, syncEnabled = false, onnavigate }: { active: ViewId; syncEnabled?: boolean; onnavigate: () => void } = $props();
	const groups = $derived(groupsFor(syncEnabled));
</script>

<nav aria-label="Main navigation">
	{#each groups as group}
		<section class:administration={group.label === 'Administration'} aria-label={group.label}>
			<h2>{group.label}</h2>
			{#each group.items as item}
				<a href={'?view=' + item.id} aria-current={active === item.id ? 'page' : undefined} onclick={onnavigate}>
					<span class="marker" aria-hidden="true"></span>{item.label}
				</a>
			{/each}
		</section>
	{/each}
</nav>

<style>
	nav { display: flex; flex-direction: column; gap: 24px; min-height: 100%; }
	h2 { font-size: 10px; letter-spacing: .13em; text-transform: uppercase; color: var(--text-muted); padding: 0 14px; margin-bottom: 8px; }
	a { display: flex; gap: 12px; align-items: center; padding: 9px 14px; color: var(--text-secondary); text-decoration: none; border-radius: 7px; font-size: 13px; border: 1px solid transparent; }
	a:hover { background: var(--nav-hover); color: var(--text-primary); }
	a[aria-current] { color: var(--accent); background: var(--accent-soft); border-color: var(--accent-border); font-weight: 600; }
	.marker { width: 6px; height: 6px; border: 1px solid currentColor; border-radius: 2px; opacity: .6; }
	a[aria-current] .marker { background: currentColor; opacity: 1; }
	.administration { margin-top: auto; padding-top: 20px; border-top: 1px solid var(--border); }
</style>
