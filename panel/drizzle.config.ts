import { defineConfig } from 'drizzle-kit';

// Mirrors Beacon's worker/drizzle.config.ts pattern: schema lives with the
// app, generated SQL migrations land in the repo-root migrations/ directory
// so `wrangler d1 migrations apply` (cloudflare target) and the local dev
// bootstrap (node target, see src/lib/server/db/index.ts) share one source
// of truth.
export default defineConfig({
	schema: './src/lib/server/db/schema.ts',
	out: '../migrations',
	dialect: 'sqlite'
});
