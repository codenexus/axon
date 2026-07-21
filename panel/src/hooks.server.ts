import { redirect, type Handle } from '@sveltejs/kit';
import { getDb } from '$lib/server/db';
import { isAuthenticated } from '$lib/server/auth';

const PUBLIC_PATHS = ['/login'];

export const handle: Handle = async ({ event, resolve }) => {
	event.locals.db = await getDb(event.platform);

	const isApiRoute = event.url.pathname.startsWith('/api/v1/');
	const isPublic = PUBLIC_PATHS.includes(event.url.pathname);

	if (isApiRoute) {
		// /api/v1/* routes (enroll, heartbeat) authenticate Pulse agents via
		// their own bearer tokens, not the admin session cookie.
		return resolve(event);
	}

	event.locals.adminAuthenticated = await isAuthenticated(event.locals.db, event.cookies);

	if (!isPublic && !event.locals.adminAuthenticated) {
		throw redirect(303, '/login');
	}
	if (isPublic && event.locals.adminAuthenticated) {
		throw redirect(303, '/');
	}

	return resolve(event);
};
