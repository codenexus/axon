import adapterNode from '@sveltejs/adapter-node';
import adapterCloudflare from '@sveltejs/adapter-cloudflare';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

// Three Panel deployment targets share this one config; only the adapter
// changes, selected via ADAPTER=cloudflare|node (see Makefile / package.json
// scripts). Defaults to node so `pnpm run dev` / a bare `pnpm run build`
// works without the env var set.
const adapter = process.env.ADAPTER === 'cloudflare' ? adapterCloudflare() : adapterNode();

export default defineConfig({
	plugins: [
		sveltekit({
			compilerOptions: {
				// Force runes mode for the project, except for libraries. Can be removed in svelte 6.
				runes: ({ filename }) =>
					filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},
			adapter
		})
	]
});
