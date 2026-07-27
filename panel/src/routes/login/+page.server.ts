import { fail, redirect } from '@sveltejs/kit';
import type { Actions, PageServerLoad } from './$types';
import { adminSettings } from '$lib/server/db/schema';
import { hasAdmin, createSession } from '$lib/server/auth';
import { hashPassword, verifyPassword } from '$lib/server/tokens';

export const load: PageServerLoad = async ({ locals }) => {
	return { firstRun: !(await hasAdmin(locals.db)) };
};

export const actions: Actions = {
	default: async ({ request, locals, cookies, url }) => {
		const form = await request.formData();
		const password = String(form.get('password') ?? '');
		if (password.length < 8) {
			return fail(400, { error: 'Password must be at least 8 characters.' });
		}
		const secure = url.protocol === 'https:';

		const firstRun = !(await hasAdmin(locals.db));
		if (firstRun) {
			await locals.db.insert(adminSettings).values({
				passwordHash: await hashPassword(password),
				createdAt: Date.now()
			});
			await createSession(locals.db, cookies, secure);
			throw redirect(303, '/');
		}

		const [admin] = await locals.db.select().from(adminSettings).limit(1);
		if (!admin || !(await verifyPassword(password, admin.passwordHash))) {
			return fail(400, { error: 'Incorrect password.' });
		}
		await createSession(locals.db, cookies, secure);
		throw redirect(303, '/');
	}
};
