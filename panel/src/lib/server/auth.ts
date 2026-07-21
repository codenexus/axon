import type { Cookies } from '@sveltejs/kit';
import { eq } from 'drizzle-orm';
import { dev } from '$app/environment';
import type { Db } from './db';
import { adminSessions, adminSettings } from './db/schema';
import { randomToken, sha256Hex } from './tokens';

export const SESSION_COOKIE = 'axon_session';
const SESSION_TTL_MS = 7 * 24 * 60 * 60 * 1000;

export async function createSession(db: Db, cookies: Cookies): Promise<void> {
	const token = randomToken();
	const now = Date.now();
	await db.insert(adminSessions).values({
		id: sha256Hex(token),
		createdAt: now,
		expiresAt: now + SESSION_TTL_MS
	});
	cookies.set(SESSION_COOKIE, token, {
		path: '/',
		httpOnly: true,
		// Home-lab/LAN deployments over plain http need the cookie to still
		// be set; only require `secure` once we know we're not in dev.
		secure: !dev,
		sameSite: 'lax',
		maxAge: SESSION_TTL_MS / 1000
	});
}

export async function destroySession(db: Db, cookies: Cookies): Promise<void> {
	const token = cookies.get(SESSION_COOKIE);
	if (token) {
		await db.delete(adminSessions).where(eq(adminSessions.id, sha256Hex(token)));
	}
	cookies.delete(SESSION_COOKIE, { path: '/' });
}

export async function isAuthenticated(db: Db, cookies: Cookies): Promise<boolean> {
	const token = cookies.get(SESSION_COOKIE);
	if (!token) return false;

	const rows = await db.select().from(adminSessions).where(eq(adminSessions.id, sha256Hex(token)));
	const session = rows[0];
	if (!session) return false;
	return session.expiresAt >= Date.now();
}

export async function hasAdmin(db: Db): Promise<boolean> {
	const rows = await db.select().from(adminSettings).limit(1);
	return rows.length > 0;
}
