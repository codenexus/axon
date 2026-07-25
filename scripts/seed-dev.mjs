// Seeds a local dev admin account + a throwaway enrollment token so the
// Pulse <-> Panel vertical slice can be exercised end-to-end without going
// through the (not-yet-built) first-run setup wizard by hand each time.
//
// Run via `pnpm run seed:dev` from panel/ (sets cwd so it writes to the same
// axon-local.db the dev server reads — see panel/src/lib/server/db/index.ts).
import { DatabaseSync } from 'node:sqlite';
import { randomBytes, scrypt, createHash } from 'node:crypto';
import { promisify } from 'node:util';
import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const scryptAsync = promisify(scrypt);

const DEV_PASSWORD = 'axon-dev-password';
const dbPath = process.env.AXON_LOCAL_DB_PATH ?? path.resolve(process.cwd(), 'axon-local.db');
const db = new DatabaseSync(dbPath);

const migrationsDir = fileURLToPath(new URL('../migrations', import.meta.url));
if (existsSync(migrationsDir)) {
	for (const file of readdirSync(migrationsDir).filter((f) => f.endsWith('.sql')).sort()) {
		const sql = readFileSync(path.join(migrationsDir, file), 'utf-8');
		for (const statement of sql.split('--> statement-breakpoint')) {
			const trimmed = statement.trim();
			if (!trimmed) continue;
			try {
				db.exec(trimmed);
			} catch (err) {
				const message = String(err.message);
				if (!message.includes('already exists') && !message.includes('duplicate column name')) {
					throw err;
				}
			}
		}
	}
}

async function hashPassword(password) {
	const salt = randomBytes(16).toString('hex');
	const derived = (await scryptAsync(password, salt, 64)).toString('hex');
	return `${salt}:${derived}`;
}

function sha256Hex(value) {
	return createHash('sha256').update(value).digest('hex');
}

function randomToken(bytes = 32) {
	return randomBytes(bytes).toString('hex');
}

const existingAdmin = db.prepare('SELECT id FROM admin_settings LIMIT 1').get();
if (!existingAdmin) {
	db.prepare('INSERT INTO admin_settings (password_hash, created_at) VALUES (?, ?)').run(
		await hashPassword(DEV_PASSWORD),
		Date.now()
	);
	console.log(`Created dev admin — password: ${DEV_PASSWORD}`);
} else {
	console.log('Admin already exists, leaving as-is.');
}

const token = randomToken(16);
const now = Date.now();
db.prepare(
	'INSERT INTO enrollment_tokens (id, token_hash, created_at, expires_at) VALUES (?, ?, ?, ?)'
).run(`tok_${randomToken(8)}`, sha256Hex(token), now, now + 30 * 60 * 1000);

console.log(`Enrollment token (valid 30 min): ${token}`);
console.log(`\nRun pulse with:\n  ./pulse --server-url http://localhost:5173 --enroll-token ${token}`);

db.close();
