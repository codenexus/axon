import { json, error } from '@sveltejs/kit';
import { and, eq, gt } from 'drizzle-orm';
import type { RequestHandler } from './$types';
import { enrollmentTokens, pulseAgents } from '$lib/server/db/schema';
import { bearerToken } from '$lib/server/http';
import { randomToken, sha256Hex } from '$lib/server/tokens';
import type { EnrollRequestBody, EnrollResponseBody } from '$lib/server/protocol';

export const POST: RequestHandler = async ({ request, locals }) => {
	const token = bearerToken(request);
	if (!token) throw error(401, 'missing enrollment token');

	const now = Date.now();
	const [enrollment] = await locals.db
		.select()
		.from(enrollmentTokens)
		.where(and(eq(enrollmentTokens.tokenHash, sha256Hex(token)), gt(enrollmentTokens.expiresAt, now)));

	if (!enrollment) throw error(401, 'invalid or expired enrollment token');

	const body = (await request.json()) as EnrollRequestBody;
	if (!body.hostname) throw error(400, 'hostname is required');

	const deviceId = `agent_${randomToken(8)}`;
	const deviceCredential = randomToken();

	await locals.db.insert(pulseAgents).values({
		id: deviceId,
		hostname: body.hostname,
		os: body.os ?? 'unknown',
		arch: body.arch ?? 'unknown',
		deviceCredentialHash: sha256Hex(deviceCredential),
		pulseVersion: body.pulse_version ?? 'unknown',
		createdAt: now
	});

	await locals.db.update(enrollmentTokens).set({ usedAt: now }).where(eq(enrollmentTokens.id, enrollment.id));

	const response: EnrollResponseBody = { device_id: deviceId, device_credential: deviceCredential };
	return json(response);
};
