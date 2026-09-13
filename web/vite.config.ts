import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [sveltekit({ compilerOptions: { runes: true }, adapter: adapter() })],
	server: { proxy: { '/api': 'http://127.0.0.1:4141' } }
});
