<script lang="ts">
	import { n, when } from '../format';
	import { providerOf, harnessLabel, harnessVar, sortHarnesses, tokensOf, type Row } from '../usage';
	import { PROVIDERS } from '../connect';
	import { priceFor } from '../pricing';
	import { prices } from '../prices.svelte';

	let { rows }: { rows: Row[] } = $props();

	const providerLabel = (key: string) => PROVIDERS.find((p) => p.key === key)?.label ?? key;

	// Error replies carry no model; they are counted on the Overview, not here.
	const models = $derived(
		[...Map.groupBy(rows.filter((r) => r.model), (r) => r.model)]
			.map(([model, rs]) => {
				const resolved = priceFor(model, prices.catalog);
				return {
					model,
					providers: [...new Set(rs.map(providerOf))].sort(),
					harnesses: sortHarnesses(rs.map((r) => r.harness)),
					n: rs.length,
					tokens: rs.reduce((s, r) => s + tokensOf(r), 0),
					first: Math.min(...rs.map((r) => r.time)),
					last: Math.max(...rs.map((r) => r.time)),
					priced: resolved.source
				};
			})
			.sort((a, b) => b.last - a.last)
	);
</script>

<div class="scroll">
	<table>
		<thead>
			<tr>
				<th>Model</th>
				<th>Provider</th>
				<th>Harnesses</th>
				<th class="num">Requests</th>
				<th class="num">Tokens</th>
				<th>First seen</th>
				<th>Last seen</th>
				<th>Price</th>
			</tr>
		</thead>
		<tbody>
			{#each models as m (m.model)}
				<tr>
					<td><code>{m.model}</code></td>
					<td>{m.providers.map(providerLabel).join(', ')}</td>
					<td>{#each m.harnesses as h, i}{i ? ', ' : ''}<i class="dot" style="background: {harnessVar(h)}"></i>{harnessLabel(h)}{/each}</td>
					<td class="num">{n(m.n)}</td>
					<td class="num">{n(m.tokens)}</td>
					<td>{when(m.first)}</td>
					<td>{when(m.last)}</td>
					<td>
						{#if m.priced === 'catalog'}<span class="badge good">catalogue</span>
						{:else if m.priced === 'list'}<span class="badge">built-in</span>
						{:else}<span class="badge warn">unpriced</span>{/if}
					</td>
				</tr>
			{:else}
				<tr><td colspan="8">No model has answered through the proxy yet.</td></tr>
			{/each}
		</tbody>
	</table>
</div>
<p class="caption">
	Model ids are whatever the provider reported in its reply. The Price column says where the rate for the cost estimates comes from; the rates themselves are on <a href="?view=settings&section=prices">Prices</a>.
</p>
