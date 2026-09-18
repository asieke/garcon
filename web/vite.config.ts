import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig, loadEnv } from 'vite';

export default defineConfig(({ mode }) => ({
	plugins: [
		sveltekit({
			compilerOptions: { runes: true },
			adapter: adapter({ pages: '../internal/dashboard/build', assets: '../internal/dashboard/build' }),
			// The dashboard loads nothing from the network, and the prerendered page says so
			// to the browser. Hash mode covers SvelteKit's own inline start script; the styles
			// exception is for the charts' style attributes. frame-ancestors is ignored in a
			// <meta> policy, so the proxy sends X-Frame-Options instead.
			csp: {
				mode: 'hash',
				directives: {
					'default-src': ['self'],
					'script-src': ['self'],
					'style-src': ['self', 'unsafe-inline'],
					'img-src': ['self', 'data:'],
					'connect-src': ['self'],
					'object-src': ['none'],
					'base-uri': ['self'],
					'form-action': ['self']
				}
			}
		})
	],
	// `npm run dev` serves the UI on 4242 while a garcon from npm keeps running on 4141; the
	// proxy hands /api to it (GARCON_URL points it at a garcon built from source on another port).
	// strictPort so a stale server fails loudly instead of drifting to 4243.
	server: {
		host: '127.0.0.1', port: 4242, strictPort: true,
		proxy: {
			'/api': {
				target: loadEnv(mode, '.', 'GARCON_').GARCON_URL || 'http://127.0.0.1:4141',
				// Keep the browser's Host aligned with Origin for guarded local POSTs.
				changeOrigin: false
			}
		}
	}
}));
