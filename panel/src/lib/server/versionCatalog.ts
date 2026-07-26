import { and, eq, gt } from 'drizzle-orm';
import type { Db } from './db';
import { versionCatalogEntries } from './db/schema';

// How long a resolved version list is trusted before Panel re-fetches it.
export const CATALOG_TTL_MS = 12 * 60 * 60 * 1000;

// Only the latest few versions are offered — not full historical catalogs.
const LATEST_VERSION_COUNT = 3;

const MOJANG_MANIFEST_URL = 'https://piston-meta.mojang.com/mc/game/version_manifest_v2.json';
const BEDROCK_DOWNLOAD_PAGE_URL = 'https://www.minecraft.net/en-us/download/server/bedrock';

export interface VersionCatalogEntry {
	id: string;
	gamePlatform: string;
	version: string;
	downloadUrl: string;
	javaMajorVersion: number | null;
	sortOrder: number;
}

interface MojangManifestVersion {
	id: string;
	type: string;
	url: string;
}

interface MojangVersionDetail {
	downloads?: { server?: { url: string } };
	javaVersion?: { majorVersion: number };
}

// Resolves the latest Java vanilla versions via Mojang's public version
// manifest — downloads.server.url and javaVersion.majorVersion are real,
// documented fields (confirmed live against the actual API while designing
// this), so no hardcoded MC-version-to-Java-version mapping is needed.
export async function resolveJavaVersions(db: Db): Promise<VersionCatalogEntry[]> {
	const cached = await freshEntries(db, 'java');
	if (cached.length > 0) return cached;

	try {
		const fresh = await fetchJavaVersions();
		if (fresh.length > 0) {
			await replaceEntries(db, 'java', fresh);
			return fresh;
		}
	} catch {
		// Fall through to whatever's cached (even stale) below.
	}
	return staleEntries(db, 'java');
}

// Resolves the current Bedrock vanilla version by scraping minecraft.net's
// download page — there's no versioned API for it. This is confirmed
// best-effort and unreliable in practice (a live test during this feature's
// design timed out, likely bot-detection or JS rendering on Mojang's end),
// so a scrape failure here is NOT an error to the caller — it just yields
// an empty/stale result, and the create-server form falls back to a blank,
// admin-editable download-URL field rather than blocking the whole flow.
export async function resolveBedrockVersions(db: Db): Promise<VersionCatalogEntry[]> {
	const cached = await freshEntries(db, 'bedrock');
	if (cached.length > 0) return cached;

	try {
		const fresh = await fetchBedrockVersions();
		if (fresh.length > 0) {
			await replaceEntries(db, 'bedrock', fresh);
			return fresh;
		}
	} catch {
		// Scraping is expected to fail sometimes — see doc comment above.
	}
	return staleEntries(db, 'bedrock');
}

async function freshEntries(db: Db, gamePlatform: string): Promise<VersionCatalogEntry[]> {
	const now = Date.now();
	return db
		.select()
		.from(versionCatalogEntries)
		.where(and(eq(versionCatalogEntries.gamePlatform, gamePlatform), gt(versionCatalogEntries.expiresAt, now)))
		.orderBy(versionCatalogEntries.sortOrder);
}

async function staleEntries(db: Db, gamePlatform: string): Promise<VersionCatalogEntry[]> {
	return db
		.select()
		.from(versionCatalogEntries)
		.where(eq(versionCatalogEntries.gamePlatform, gamePlatform))
		.orderBy(versionCatalogEntries.sortOrder);
}

async function replaceEntries(db: Db, gamePlatform: string, entries: VersionCatalogEntry[]): Promise<void> {
	const now = Date.now();
	await db.delete(versionCatalogEntries).where(eq(versionCatalogEntries.gamePlatform, gamePlatform));
	for (const entry of entries) {
		await db.insert(versionCatalogEntries).values({ ...entry, fetchedAt: now, expiresAt: now + CATALOG_TTL_MS });
	}
}

async function fetchJavaVersions(): Promise<VersionCatalogEntry[]> {
	const manifestRes = await fetch(MOJANG_MANIFEST_URL);
	if (!manifestRes.ok) throw new Error(`mojang manifest fetch failed: ${manifestRes.status}`);
	const manifest = (await manifestRes.json()) as { versions: MojangManifestVersion[] };

	const releases = manifest.versions.filter((v) => v.type === 'release').slice(0, LATEST_VERSION_COUNT);

	const entries: VersionCatalogEntry[] = [];
	for (let i = 0; i < releases.length; i++) {
		const release = releases[i];
		const detailRes = await fetch(release.url);
		if (!detailRes.ok) continue; // skip one bad version rather than failing the whole batch
		const detail = (await detailRes.json()) as MojangVersionDetail;
		const downloadUrl = detail.downloads?.server?.url;
		const javaMajorVersion = detail.javaVersion?.majorVersion;
		if (!downloadUrl || !javaMajorVersion) continue;

		entries.push({
			id: `java:${release.id}`,
			gamePlatform: 'java',
			version: release.id,
			downloadUrl,
			javaMajorVersion,
			sortOrder: i
		});
	}
	return entries;
}

async function fetchBedrockVersions(): Promise<VersionCatalogEntry[]> {
	const res = await fetch(BEDROCK_DOWNLOAD_PAGE_URL);
	if (!res.ok) throw new Error(`bedrock download page fetch failed: ${res.status}`);
	const html = await res.text();

	// Plain pattern match, not a real parser — minecraft.net has no stable
	// API for this, so this may simply find nothing if the page's shape
	// changes or it blocks non-browser requests (see doc comment above).
	const match = html.match(/(https:\/\/\S*?bedrock-server-([0-9.]+)\.zip)/i);
	if (!match) return [];

	return [
		{
			id: `bedrock:${match[2]}`,
			gamePlatform: 'bedrock',
			version: match[2],
			downloadUrl: match[1],
			javaMajorVersion: null,
			sortOrder: 0
		}
	];
}
