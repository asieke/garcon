/** The price catalogue shared by the Cost, Sessions and Settings views: loaded once per page
 * load from /api/prices, refreshed on demand from Settings → Prices. */
import { parseCatalog, type Catalog } from './pricing';

/** problem: null when the last request worked; otherwise why the catalogue is not available. */
export const prices = $state<{ catalog: Catalog | null; loading: boolean; problem: string | null }>({
	catalog: null,
	loading: false,
	problem: null
});

/** GET returns the cached catalogue (fetching it first if the proxy has none); POST refreshes it now. */
export async function loadPrices(refresh = false): Promise<void> {
	if (prices.loading) return;
	prices.loading = true;
	try {
		const r = await fetch('/api/prices', refresh ? { method: 'POST' } : undefined);
		if (r.status === 404) {
			// An older proxy that predates the endpoint, typically a dev UI in front of an installed garcon.
			prices.problem = 'The running garcon has no /api/prices; restart it from the current source.';
			return;
		}
		if (!r.ok) throw new Error(String(r.status));
		prices.catalog = parseCatalog(await r.json());
		prices.problem = null;
	} catch {
		prices.problem = 'Could not reach the proxy.';
	} finally {
		prices.loading = false;
	}
}
