import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
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
	server: { proxy: { '/api': 'http://127.0.0.1:4141' } }
});
