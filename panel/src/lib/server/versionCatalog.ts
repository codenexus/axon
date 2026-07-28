import { and, eq, gt } from 'drizzle-orm';
import type { Db } from './db';
import { versionCatalogEntries, versionCatalogFetchAttempts } from './db/schema';

// How long a resolved version list is trusted before Panel re-fetches it.
export const CATALOG_TTL_MS = 12 * 60 * 60 * 1000;

// How long to wait before retrying a fetch that failed or produced zero
// entries, distinct from (and much shorter than) CATALOG_TTL_MS, which
// only applies once a fetch has actually succeeded. See
// versionCatalogFetchAttempts in db/schema.ts for why this needs its own
// tracking table rather than reusing versionCatalogEntries' own
// fetchedAt/expiresAt.
const NEGATIVE_CACHE_TTL_MS = 15 * 60 * 1000;

// Only the latest few versions are offered — not full historical catalogs.
const LATEST_VERSION_COUNT = 3;

const MOJANG_MANIFEST_URL = 'https://piston-meta.mojang.com/mc/game/version_manifest_v2.json';
const BEDROCK_DOWNLOAD_PAGE_URL = 'https://www.minecraft.net/en-us/download/server/bedrock';
const PAPER_PROJECT_URL = 'https://fill.papermc.io/v3/projects/paper';
const FABRIC_LOADER_URL = 'https://meta.fabricmc.net/v2/versions/loader';
const FABRIC_INSTALLER_URL = 'https://meta.fabricmc.net/v2/versions/installer';
const FORGE_PROMOTIONS_URL = 'https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json';

// All outbound requests here identify themselves — several of these APIs
// (Paper, Fabric) explicitly ask for a real User-Agent, and it's simply
// good practice for the rest.
const USER_AGENT = 'Axon-Panel (https://github.com/codenexus/axon)';

// resolveCached() below already catches a fetch failure and falls back to
// a stale (or empty) cached result -- but only once the fetch actually
// fails. With no timeout, a connection that hangs instead of erroring
// (confirmed live against minecraft.net's flaky Bedrock download page,
// see fetchBedrockVersions) never triggers that fallback at all; it just
// blocks the whole /settings and create-server page load indefinitely,
// which looks identical to a dead button from the admin's side. Every
// fetch() in this file must pass this signal.
const FETCH_TIMEOUT_MS = 8000;

export interface VersionCatalogEntry {
	id: string;
	gamePlatform: string;
	softwareType: string;
	version: string;
	downloadUrl: string;
	javaMajorVersion: number | null;
	loaderVersion: string | null;
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

// Resolves the latest vanilla Java versions via Mojang's public version
// manifest — downloads.server.url and javaVersion.majorVersion are real,
// documented fields (confirmed live against the actual API while designing
// this), so no hardcoded MC-version-to-Java-version mapping is needed.
export async function resolveJavaVersions(db: Db): Promise<VersionCatalogEntry[]> {
	return resolveCached(db, 'java', 'vanilla', fetchJavaVersions);
}

// Resolves the current Bedrock vanilla version by scraping minecraft.net's
// download page — there's no versioned API for it. This is confirmed
// best-effort and unreliable in practice (a live test during this feature's
// design timed out, likely bot-detection or JS rendering on Mojang's end),
// so a scrape failure here is NOT an error to the caller — it just yields
// an empty/stale result, and the create-server form falls back to a blank,
// admin-editable download-URL field rather than blocking the whole flow.
export async function resolveBedrockVersions(db: Db): Promise<VersionCatalogEntry[]> {
	return resolveCached(db, 'bedrock', 'vanilla', fetchBedrockVersions);
}

// Paper's own API (fill.papermc.io) — structurally identical to vanilla:
// it returns a direct downloadable server jar URL, so Pulse needs zero
// changes to run a Paper server (it's just "server.jar" like any other
// Java instance). Paper's own version list is queried rather than assumed
// identical to vanilla's latest-3 (Paper sometimes lags behind a fresh MC
// release).
export async function resolvePaperVersions(db: Db): Promise<VersionCatalogEntry[]> {
	return resolveCached(db, 'java', 'paper', fetchPaperVersions);
}

// Fabric needs an *installer* run (see pulse/internal/provision) — the
// "download" here is the installer jar itself, not a runnable server.
// loaderVersion carries the separately-resolved Fabric loader version
// (independent of the Minecraft version) the installer needs as an arg.
export async function resolveFabricVersions(db: Db): Promise<VersionCatalogEntry[]> {
	return resolveCached(db, 'java', 'fabric', fetchFabricVersions);
}

// Forge also needs an installer run. The installer URL is constructed
// deterministically from Forge's own maven layout once the recommended
// Forge build for a given MC version is known (verified live against the
// real maven repo while designing this).
export async function resolveForgeVersions(db: Db): Promise<VersionCatalogEntry[]> {
	return resolveCached(db, 'java', 'forge', fetchForgeVersions);
}

export interface ResolvedVersionSelection {
	version: string;
	downloadUrl: string;
	javaMajorVersion?: number;
	loaderVersion?: string;
}

// Shared by the create-server action and the create-definition action:
// resolves a concrete version/downloadUrl/javaMajorVersion/loaderVersion
// from an edition + software type + catalog id + (Bedrock only) an
// optional admin-supplied URL override. Every Java software type is
// always re-looked-up server-side by catalog id, never trusted from
// client input — these catalogs are authoritative and there's no reason
// to let them be overridden, unlike Bedrock, which has no authoritative
// source to defer to.
export async function resolveVersionSelection(
	db: Db,
	gamePlatform: string,
	softwareType: string,
	catalogId: string,
	bedrockUrlOverride: string
): Promise<ResolvedVersionSelection | { error: string }> {
	if (gamePlatform === 'java') {
		const [entry] = await db.select().from(versionCatalogEntries).where(eq(versionCatalogEntries.id, catalogId));
		if (!entry || entry.gamePlatform !== 'java' || entry.softwareType !== softwareType) {
			return { error: 'select a valid version' };
		}
		return {
			version: entry.version,
			downloadUrl: entry.downloadUrl,
			javaMajorVersion: entry.javaMajorVersion ?? undefined,
			loaderVersion: entry.loaderVersion ?? undefined
		};
	}

	let catalogVersion = '';
	if (catalogId) {
		const [entry] = await db.select().from(versionCatalogEntries).where(eq(versionCatalogEntries.id, catalogId));
		if (entry && entry.gamePlatform === 'bedrock') catalogVersion = entry.version;
	}
	const downloadUrl = bedrockUrlOverride;
	const version = catalogVersion || 'unknown';
	if (!downloadUrl) {
		return { error: 'a download URL is required (the automatic lookup may have failed — see the field above)' };
	}
	return { version, downloadUrl };
}

// Shared cache-or-fetch shape every resolveXVersions() above follows:
// serve a fresh cached catalog if one exists, otherwise fetch live and
// replace the cache, falling back to a stale cache (or empty) if the
// live fetch itself fails. A failure (or empty result) also records a
// negative-cache attempt so a reliably-failing endpoint (e.g.
// minecraft.net's Bedrock scrape) isn't retried on every single page
// load — see NEGATIVE_CACHE_TTL_MS and versionCatalogFetchAttempts.
async function resolveCached(
	db: Db,
	gamePlatform: string,
	softwareType: string,
	fetcher: () => Promise<VersionCatalogEntry[]>
): Promise<VersionCatalogEntry[]> {
	const cached = await freshEntries(db, gamePlatform, softwareType);
	if (cached.length > 0) return cached;

	const attemptId = `${gamePlatform}:${softwareType}`;
	if (await recentlyAttempted(db, attemptId)) {
		return staleEntries(db, gamePlatform, softwareType);
	}

	try {
		const fresh = await fetcher();
		if (fresh.length > 0) {
			await replaceEntries(db, gamePlatform, softwareType, fresh);
			return fresh;
		}
	} catch {
		// Fall through to whatever's cached (even stale) below — every
		// fetcher here talks to a third-party API/page that can fail or
		// change shape; that's never a reason to break the create-server
		// form entirely.
	}
	await recordAttempt(db, attemptId);
	return staleEntries(db, gamePlatform, softwareType);
}

async function recentlyAttempted(db: Db, id: string): Promise<boolean> {
	const [row] = await db
		.select({ attemptedAt: versionCatalogFetchAttempts.attemptedAt })
		.from(versionCatalogFetchAttempts)
		.where(eq(versionCatalogFetchAttempts.id, id));
	return !!row && row.attemptedAt > Date.now() - NEGATIVE_CACHE_TTL_MS;
}

async function recordAttempt(db: Db, id: string): Promise<void> {
	const attemptedAt = Date.now();
	await db
		.insert(versionCatalogFetchAttempts)
		.values({ id, attemptedAt })
		.onConflictDoUpdate({ target: versionCatalogFetchAttempts.id, set: { attemptedAt } });
}

async function freshEntries(db: Db, gamePlatform: string, softwareType: string): Promise<VersionCatalogEntry[]> {
	const now = Date.now();
	return db
		.select()
		.from(versionCatalogEntries)
		.where(
			and(
				eq(versionCatalogEntries.gamePlatform, gamePlatform),
				eq(versionCatalogEntries.softwareType, softwareType),
				gt(versionCatalogEntries.expiresAt, now)
			)
		)
		.orderBy(versionCatalogEntries.sortOrder);
}

async function staleEntries(db: Db, gamePlatform: string, softwareType: string): Promise<VersionCatalogEntry[]> {
	return db
		.select()
		.from(versionCatalogEntries)
		.where(and(eq(versionCatalogEntries.gamePlatform, gamePlatform), eq(versionCatalogEntries.softwareType, softwareType)))
		.orderBy(versionCatalogEntries.sortOrder);
}

async function replaceEntries(
	db: Db,
	gamePlatform: string,
	softwareType: string,
	entries: VersionCatalogEntry[]
): Promise<void> {
	const now = Date.now();
	await db
		.delete(versionCatalogEntries)
		.where(and(eq(versionCatalogEntries.gamePlatform, gamePlatform), eq(versionCatalogEntries.softwareType, softwareType)));
	for (const entry of entries) {
		await db.insert(versionCatalogEntries).values({ ...entry, fetchedAt: now, expiresAt: now + CATALOG_TTL_MS });
	}
}

async function fetchJavaVersions(): Promise<VersionCatalogEntry[]> {
	const manifestRes = await fetch(MOJANG_MANIFEST_URL, { headers: { 'User-Agent': USER_AGENT }, signal: AbortSignal.timeout(FETCH_TIMEOUT_MS) });
	if (!manifestRes.ok) throw new Error(`mojang manifest fetch failed: ${manifestRes.status}`);
	const manifest = (await manifestRes.json()) as { versions: MojangManifestVersion[] };

	const releases = manifest.versions.filter((v) => v.type === 'release').slice(0, LATEST_VERSION_COUNT);

	const entries: VersionCatalogEntry[] = [];
	for (let i = 0; i < releases.length; i++) {
		const release = releases[i];
		const detailRes = await fetch(release.url, { headers: { 'User-Agent': USER_AGENT }, signal: AbortSignal.timeout(FETCH_TIMEOUT_MS) });
		if (!detailRes.ok) continue; // skip one bad version rather than failing the whole batch
		const detail = (await detailRes.json()) as MojangVersionDetail;
		const downloadUrl = detail.downloads?.server?.url;
		const javaMajorVersion = detail.javaVersion?.majorVersion;
		if (!downloadUrl || !javaMajorVersion) continue;

		entries.push({
			id: `java:vanilla:${release.id}`,
			gamePlatform: 'java',
			softwareType: 'vanilla',
			version: release.id,
			downloadUrl,
			javaMajorVersion,
			loaderVersion: null,
			sortOrder: i
		});
	}
	return entries;
}

async function fetchBedrockVersions(): Promise<VersionCatalogEntry[]> {
	const res = await fetch(BEDROCK_DOWNLOAD_PAGE_URL, { headers: { 'User-Agent': USER_AGENT }, signal: AbortSignal.timeout(FETCH_TIMEOUT_MS) });
	if (!res.ok) throw new Error(`bedrock download page fetch failed: ${res.status}`);
	const html = await res.text();

	// Plain pattern match, not a real parser — minecraft.net has no stable
	// API for this, so this may simply find nothing if the page's shape
	// changes or it blocks non-browser requests (see doc comment above).
	const match = html.match(/(https:\/\/\S*?bedrock-server-([0-9.]+)\.zip)/i);
	if (!match) return [];

	return [
		{
			id: `bedrock:vanilla:${match[2]}`,
			gamePlatform: 'bedrock',
			softwareType: 'vanilla',
			version: match[2],
			downloadUrl: match[1],
			javaMajorVersion: null,
			loaderVersion: null,
			sortOrder: 0
		}
	];
}

// Reuses the same latest-3 vanilla MC versions Java resolution already
// settled on, so Paper/Fabric/Forge always offer exactly the same MC
// version set vanilla does — one source of truth for "which MC versions
// are current," not three independently-derived lists.
async function latestVanillaMcVersions(): Promise<{ version: string; javaMajorVersion: number }[]> {
	const vanilla = await fetchJavaVersions();
	return vanilla.map((v) => ({ version: v.version, javaMajorVersion: v.javaMajorVersion ?? 21 }));
}

interface PaperProjectResponse {
	versions: Record<string, string[]>;
}

interface PaperBuild {
	channel: string;
	downloads: { 'server:default'?: { url: string; name: string } };
}

async function fetchPaperVersions(): Promise<VersionCatalogEntry[]> {
	const vanillaVersions = await latestVanillaMcVersions();
	const javaMajorByVersion = new Map(vanillaVersions.map((v) => [v.version, v.javaMajorVersion]));

	const projectRes = await fetch(PAPER_PROJECT_URL, { headers: { 'User-Agent': USER_AGENT }, signal: AbortSignal.timeout(FETCH_TIMEOUT_MS) });
	if (!projectRes.ok) throw new Error(`paper project fetch failed: ${projectRes.status}`);
	const project = (await projectRes.json()) as PaperProjectResponse;

	// Families are returned newest-first; flatten and drop pre-release/rc
	// entries to get a plain "latest stable versions" list, without
	// needing to numerically compare version strings ourselves (Paper's
	// own version families aren't simple linear "1.x.y" — trust the API's
	// own ordering instead of reimplementing version comparison).
	const flatVersions: string[] = [];
	for (const versions of Object.values(project.versions)) {
		for (const v of versions) {
			if (/-(rc|pre|snapshot)/i.test(v)) continue;
			flatVersions.push(v);
		}
	}
	const candidateVersions = flatVersions.slice(0, LATEST_VERSION_COUNT);

	const entries: VersionCatalogEntry[] = [];
	for (let i = 0; i < candidateVersions.length; i++) {
		const version = candidateVersions[i];
		const buildsRes = await fetch(`${PAPER_PROJECT_URL}/versions/${version}/builds`, {
			headers: { 'User-Agent': USER_AGENT },
			signal: AbortSignal.timeout(FETCH_TIMEOUT_MS)
		});
		if (!buildsRes.ok) continue;
		const builds = (await buildsRes.json()) as PaperBuild[];
		const stable = builds.find((b) => b.channel === 'STABLE' && b.downloads['server:default']);
		const downloadUrl = stable?.downloads['server:default']?.url;
		if (!downloadUrl) continue;

		entries.push({
			id: `java:paper:${version}`,
			gamePlatform: 'java',
			softwareType: 'paper',
			version,
			downloadUrl,
			javaMajorVersion: javaMajorByVersion.get(version) ?? 21,
			loaderVersion: null,
			sortOrder: i
		});
	}
	return entries;
}

interface FabricLoaderEntry {
	loader: { version: string; stable: boolean };
}

interface FabricInstallerEntry {
	version: string;
	url: string;
	stable: boolean;
}

async function fetchFabricVersions(): Promise<VersionCatalogEntry[]> {
	const vanillaVersions = await latestVanillaMcVersions();

	const installerRes = await fetch(FABRIC_INSTALLER_URL, { headers: { 'User-Agent': USER_AGENT }, signal: AbortSignal.timeout(FETCH_TIMEOUT_MS) });
	if (!installerRes.ok) throw new Error(`fabric installer list fetch failed: ${installerRes.status}`);
	const installers = (await installerRes.json()) as FabricInstallerEntry[];
	const installer = installers.find((i) => i.stable) ?? installers[0];
	if (!installer) return [];

	const entries: VersionCatalogEntry[] = [];
	for (let i = 0; i < vanillaVersions.length; i++) {
		const { version, javaMajorVersion } = vanillaVersions[i];
		const loaderRes = await fetch(`${FABRIC_LOADER_URL}/${version}`, { headers: { 'User-Agent': USER_AGENT }, signal: AbortSignal.timeout(FETCH_TIMEOUT_MS) });
		if (!loaderRes.ok) continue;
		const loaders = (await loaderRes.json()) as FabricLoaderEntry[];
		const loader = loaders.find((l) => l.loader.stable) ?? loaders[0];
		if (!loader) continue;

		entries.push({
			id: `java:fabric:${version}`,
			gamePlatform: 'java',
			softwareType: 'fabric',
			version,
			downloadUrl: installer.url,
			javaMajorVersion,
			loaderVersion: loader.loader.version,
			sortOrder: i
		});
	}
	return entries;
}

interface ForgePromotions {
	promos: Record<string, string>;
}

async function fetchForgeVersions(): Promise<VersionCatalogEntry[]> {
	const vanillaVersions = await latestVanillaMcVersions();

	const promosRes = await fetch(FORGE_PROMOTIONS_URL, { headers: { 'User-Agent': USER_AGENT }, signal: AbortSignal.timeout(FETCH_TIMEOUT_MS) });
	if (!promosRes.ok) throw new Error(`forge promotions fetch failed: ${promosRes.status}`);
	const promotions = (await promosRes.json()) as ForgePromotions;

	const entries: VersionCatalogEntry[] = [];
	for (let i = 0; i < vanillaVersions.length; i++) {
		const { version, javaMajorVersion } = vanillaVersions[i];
		const forgeVersion = promotions.promos[`${version}-recommended`] ?? promotions.promos[`${version}-latest`];
		if (!forgeVersion) continue;

		const fullVersion = `${version}-${forgeVersion}`;
		const downloadUrl = `https://maven.minecraftforge.net/net/minecraftforge/forge/${fullVersion}/forge-${fullVersion}-installer.jar`;

		entries.push({
			id: `java:forge:${version}`,
			gamePlatform: 'java',
			softwareType: 'forge',
			version,
			downloadUrl,
			javaMajorVersion,
			loaderVersion: null,
			sortOrder: i
		});
	}
	return entries;
}
