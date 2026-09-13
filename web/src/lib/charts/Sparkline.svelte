<script lang="ts">
	let {
		points,
		color = 'var(--series-1)',
		width = 64,
		height = 22
	}: { points: number[]; color?: string; width?: number; height?: number } = $props();

	const path = $derived.by(() => {
		if (points.length < 2) return '';
		const max = Math.max(...points, 0);
		const min = Math.min(...points, 0);
		const range = max - min || 1;
		const stepX = width / (points.length - 1);
		return points
			.map((v, i) => {
				const x = i * stepX;
				const y = height - ((v - min) / range) * height;
				return `${i === 0 ? 'M' : 'L'} ${x.toFixed(1)} ${y.toFixed(1)}`;
			})
			.join(' ');
	});
</script>

{#if points.length >= 2}
	<svg {width} {height} viewBox="0 0 {width} {height}" aria-hidden="true">
		<path
			d={path}
			fill="none"
			stroke={color}
			stroke-width="2"
			stroke-linejoin="round"
			stroke-linecap="round"
		/>
	</svg>
{/if}
