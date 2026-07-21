import { drizzle as drizzleD1 } from 'drizzle-orm/d1';
import { drizzle as drizzleProxy } from 'drizzle-orm/sqlite-proxy';
import * as schema from './schema';

export type Db = ReturnType<typeof drizzleD1<typeof schema>>;

let localDbPromise: Promise<Db> | undefined;

// Local (adapter-node / Tauri) dev + same-machine deployments use Node's
// built-in node:sqlite rather than a native module — better-sqlite3 needs
// network access to download prebuilt headers at install time, which isn't
// guaranteed on every box Axon runs on (home-lab machines, offline VPS
// images). node:sqlite ships with Node itself, no native build step.
async function getLocalDb(): Promise<Db> {
	if (!localDbPromise) {
		localDbPromise = (async () => {
			// Dynamic + isolated from the cloudflare code path so the
			// adapter-cloudflare build never has to resolve node:sqlite.
			const nodeSqlite = await import(/* @vite-ignore */ 'node:sqlite');
			const fs = await import('node:fs');
			const path = await import('node:path');
			const { fileURLToPath } = await import('node:url');

			const dbPath = process.env.AXON_LOCAL_DB_PATH ?? path.resolve(process.cwd(), 'axon-local.db');
			const sqlite = new nodeSqlite.DatabaseSync(dbPath);

			const migrationsDir = fileURLToPath(new URL('../../../../../migrations', import.meta.url));
			if (fs.existsSync(migrationsDir)) {
				const files = fs
					.readdirSync(migrationsDir)
					.filter((f: string) => f.endsWith('.sql'))
					.sort();
				for (const file of files) {
					const sql = fs.readFileSync(path.join(migrationsDir, file), 'utf-8');
					// Migrations are drizzle-kit-generated `CREATE TABLE` statements;
					// run idempotently for local dev by tolerating "already exists".
					for (const statement of sql.split('--> statement-breakpoint')) {
						const trimmed = statement.trim();
						if (!trimmed) continue;
						try {
							sqlite.exec(trimmed);
						} catch (err) {
							const message = err instanceof Error ? err.message : String(err);
							if (!message.includes('already exists')) throw err;
						}
					}
				}
			}

			const callback = async (
				sql: string,
				params: unknown[],
				method: 'run' | 'all' | 'values' | 'get'
			) => {
				const stmt = sqlite.prepare(sql);
				stmt.setReturnArrays(true);
				if (method === 'run') {
					stmt.run(...(params as never[]));
					return { rows: [] };
				}
				if (method === 'get') {
					const row = stmt.get(...(params as never[]));
					return { rows: (row as unknown as unknown[]) ?? [] };
				}
				return { rows: stmt.all(...(params as never[])) as unknown as unknown[][] };
			};

			return drizzleProxy(callback, { schema }) as unknown as Db;
		})();
	}
	return localDbPromise;
}

export async function getDb(platform: App.Platform | undefined): Promise<Db> {
	const d1 = platform?.env?.DB;
	if (d1) {
		return drizzleD1(d1, { schema });
	}
	return getLocalDb();
}

export { schema };
