/** Round up to a "nice" axis maximum (1/2/2.5/5/10 × a power of ten). */
export function niceCeil(value: number): number {
	if (value <= 0) return 1;
	const magnitude = 10 ** Math.floor(Math.log10(value));
	const normalized = value / magnitude;
	const steps = [1, 2, 2.5, 5, 10];
	const step = steps.find((s) => s >= normalized) ?? 10;
	return step * magnitude;
}
