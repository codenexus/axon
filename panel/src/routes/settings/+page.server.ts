import type { Actions } from './$types';
import { enrollmentTokens } from '$lib/server/db/schema';
import { randomToken, sha256Hex } from '$lib/server/tokens';

const ENROLLMENT_TOKEN_TTL_MS = 30 * 60 * 1000; // 30 minutes

export const actions: Actions = {
	generateEnrollmentToken: async ({ locals }) => {
		const token = randomToken(16);
		const now = Date.now();
		await locals.db.insert(enrollmentTokens).values({
			id: `tok_${randomToken(8)}`,
			tokenHash: sha256Hex(token),
			createdAt: now,
			expiresAt: now + ENROLLMENT_TOKEN_TTL_MS
		});
		return { enrollmentToken: token };
	}
};
