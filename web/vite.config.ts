import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';
import { caErrorPlugin, caThemePlugin } from '@codeatlasdev/ui/vite';
import { theme } from './src/lib/theme.ts';

// The UI is a single-page app embedded in the Go binary; in dev, /api goes to a local `lowlex`.
const api = process.env.LOWLEX_API ?? 'http://localhost:4747';

export default defineConfig({
	plugins: [
		sveltekit({
			compilerOptions: {
				// Force runes mode for the project, except for libraries. Can be removed in svelte 6.
				runes: ({ filename }) =>
					filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},
			adapter: adapter({ fallback: 'index.html' })
		}),
		caThemePlugin(theme),
		caErrorPlugin({ scaffold: true, theme })
	],
	server: {
		proxy: { '/api': { target: api, changeOrigin: false } }
	}
});
