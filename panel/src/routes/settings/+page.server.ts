import { fail } from '@sveltejs/kit';
import { desc, eq } from 'drizzle-orm';
import type { Actions, PageServerLoad } from './$types';
import { enrollmentTokens, pulseReleases, serverDefinitions } from '$lib/server/db/schema';
import { randomToken, sha256Hex } from '$lib/server/tokens';
import {
	resolveBedrockVersions,
	resolveFabricVersions,
	resolveForgeVersions,
	resolveJavaVersions,
	resolvePaperVersions,
	resolveVersionSelection
} from '$lib/server/versionCatalog';

const ENROLLMENT_TOKEN_TTL_MS = 30 * 60 * 1000; // 30 minutes

const VALID_OS = ['linux', 'darwin', 'windows'];
const VALID_ARCH = ['amd64', 'arm64'];
// Ed25519 signatures are 64 bytes, hex-encoded = 128 chars.
const SIGNATURE_HEX_LENGTH = 128;

export const load: PageServerLoad = async ({ locals }) => {
	const releases = await locals.db.select().from(pulseReleases).orderBy(desc(pulseReleases.createdAt));
	const definitions = await locals.db.select().from(serverDefinitions).orderBy(desc(serverDefinitions.createdAt));
	const [javaVersions, paperVersions, fabricVersions, forgeVersions, bedrockVersions] = await Promise.all([
		resolveJavaVersions(locals.db),
		resolvePaperVersions(locals.db),
		resolveFabricVersions(locals.db),
		resolveForgeVersions(locals.db),
		resolveBedrockVersions(locals.db)
	]);
	return { releases, definitions, javaVersions, paperVersions, fabricVersions, forgeVersions, bedrockVersions };
};

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
	},

	publishRelease: async ({ request, locals }) => {
		const data = await request.formData();
		const version = (data.get('version') as string | null)?.trim();
		const os = data.get('os') as string | null;
		const arch = data.get('arch') as string | null;
		const downloadUrl = (data.get('download_url') as string | null)?.trim();
		const signatureHex = (data.get('signature_hex') as string | null)?.trim();

		if (!version) return fail(400, { error: 'version is required' });
		if (!os || !VALID_OS.includes(os)) return fail(400, { error: 'select a valid OS' });
		if (!arch || !VALID_ARCH.includes(arch)) return fail(400, { error: 'select a valid architecture' });
		if (!downloadUrl) return fail(400, { error: 'download URL is required' });
		if (!signatureHex || signatureHex.length !== SIGNATURE_HEX_LENGTH || !/^[0-9a-fA-F]+$/.test(signatureHex)) {
			return fail(400, {
				error: `signature must be ${SIGNATURE_HEX_LENGTH} hex characters (the output of pulse/tools/sign)`
			});
		}

		// Panel never verifies this signature itself — that's Pulse's job via
		// updater.VerifyBinary, the real security boundary. This is metadata
		// relay only; the admin is responsible for actually building, signing,
		// and hosting the binary somewhere Pulse can reach.
		await locals.db.insert(pulseReleases).values({
			id: `rel_${randomToken(8)}`,
			version,
			os,
			arch,
			downloadUrl,
			signatureHex,
			createdAt: Date.now()
		});

		return { published: true };
	},

	createDefinition: async ({ request, locals }) => {
		const data = await request.formData();
		const name = (data.get('name') as string | null)?.trim();
		const gamePlatform = data.get('game_platform') as string | null;
		const catalogId = (data.get('catalog_id') as string | null) ?? '';
		const bedrockUrlOverride = ((data.get('download_url') as string | null) ?? '').trim();

		if (!name) return fail(400, { error: 'a name is required' });
		if (gamePlatform !== 'java' && gamePlatform !== 'bedrock') {
			return fail(400, { error: 'select a valid edition' });
		}
		// Bedrock has no loader ecosystem — always vanilla, no selector.
		const softwareType = gamePlatform === 'java' ? String(data.get('software_type') ?? 'vanilla') : 'vanilla';

		const resolved = await resolveVersionSelection(locals.db, gamePlatform, softwareType, catalogId, bedrockUrlOverride);
		if ('error' in resolved) return fail(400, { error: resolved.error });

		await locals.db.insert(serverDefinitions).values({
			id: `def_${randomToken(8)}`,
			name,
			gamePlatform,
			softwareType,
			version: resolved.version,
			downloadUrl: resolved.downloadUrl,
			javaMajorVersion: resolved.javaMajorVersion ?? null,
			loaderVersion: resolved.loaderVersion ?? null,
			createdAt: Date.now()
		});

		return { definitionCreated: true };
	},

	deleteDefinition: async ({ request, locals }) => {
		const data = await request.formData();
		const id = String(data.get('id') ?? '');
		if (!id) return fail(400, { error: 'missing definition id' });

		await locals.db.delete(serverDefinitions).where(eq(serverDefinitions.id, id));
		return { ok: true };
	}
};
