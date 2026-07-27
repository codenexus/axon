<script lang="ts">
	import { enhance } from '$app/forms';
	import ThemeSwitcher from '$lib/theme/ThemeSwitcher.svelte';
	import type { ActionData, PageData } from './$types';

	let { data, form }: { data: PageData; form: ActionData } = $props();

	function formatTimestamp(ms: number): string {
		return new Date(ms).toLocaleString();
	}

	let definitionEdition = $state<'java' | 'bedrock'>('java');
	let definitionSoftwareType = $state<'vanilla' | 'paper' | 'fabric' | 'forge'>('vanilla');
	let definitionCatalogId = $state('');
	let definitionBedrockUrl = $state('');

	const definitionJavaVersions = $derived.by(() => {
		switch (definitionSoftwareType) {
			case 'paper':
				return data.paperVersions;
			case 'fabric':
				return data.fabricVersions;
			case 'forge':
				return data.forgeVersions;
			default:
				return data.javaVersions;
		}
	});

	$effect(() => {
		definitionSoftwareType;
		definitionCatalogId = definitionJavaVersions[0]?.id ?? '';
	});

	let bedrockPrefilled = $state(false);
	$effect(() => {
		if (bedrockPrefilled || data.bedrockVersions.length === 0) return;
		definitionBedrockUrl = data.bedrockVersions[0].downloadUrl;
		definitionCatalogId = data.bedrockVersions[0].id;
		bedrockPrefilled = true;
	});
</script>

<svelte:head>
	<title>Axon Panel — Settings</title>
</svelte:head>

<div class="page">
	<header>
		<div>
			<p class="breadcrumb"><a href="/">← Dashboard</a></p>
			<h1>Settings</h1>
		</div>
	</header>

	<section class="card">
		<h2>Appearance</h2>
		<ThemeSwitcher />
	</section>

	<section class="card">
		<h2>Enroll a Pulse agent</h2>
		<p class="meta">Generates a one-time token (valid 30 min) a new Pulse agent uses to register with this Panel.</p>
		<form method="POST" action="?/generateEnrollmentToken" use:enhance>
			<button type="submit">Generate enrollment token</button>
		</form>
		{#if form?.enrollmentToken}
			<p class="token-display">
				New token (valid 30 min, copy now): <code>{form.enrollmentToken}</code>
			</p>
			<p class="hint">
				On the Pulse machine: <code
					>pulse --server-url &lt;this Panel's URL&gt; --enroll-token {form.enrollmentToken}</code
				>
			</p>
		{/if}
	</section>

	<section class="card">
		<h2>Publish Pulse release</h2>
		<p class="meta">
			Panel never verifies the signature itself — that's Pulse's job (the real security boundary). Build, sign
			(<code>AXON_SIGNING_KEY=... go run ./tools/sign &lt;binary&gt;</code>), and host the binary yourself; this
			just records where agents should fetch it from.
		</p>
		<form method="POST" action="?/publishRelease" use:enhance class="release-form">
			<label>
				Version
				<input type="text" name="version" placeholder="a1b2c3d" required />
			</label>
			<label>
				OS
				<select name="os" required>
					<option value="linux">linux</option>
					<option value="darwin">darwin</option>
					<option value="windows">windows</option>
				</select>
			</label>
			<label>
				Arch
				<select name="arch" required>
					<option value="amd64">amd64</option>
					<option value="arm64">arm64</option>
				</select>
			</label>
			<label class="wide">
				Download URL
				<input type="text" name="download_url" placeholder="https://example.com/pulse-linux-amd64" required />
			</label>
			<label class="wide">
				Signature (hex)
				<input type="text" name="signature_hex" placeholder="128 hex characters" required />
			</label>
			<button type="submit">Publish</button>
		</form>
		{#if form?.error}
			<p class="error">{form.error}</p>
		{/if}
		{#if form?.published}
			<p class="meta">Published. Matching agents will be offered this version on their next heartbeat.</p>
		{/if}

		{#if data.releases.length > 0}
			<table class="releases">
				<thead>
					<tr>
						<th>Version</th>
						<th>OS/Arch</th>
						<th>Download URL</th>
						<th>Published</th>
					</tr>
				</thead>
				<tbody>
					{#each data.releases as release (release.id)}
						<tr>
							<td>{release.version}</td>
							<td>{release.os}/{release.arch}</td>
							<td class="url-cell">{release.downloadUrl}</td>
							<td>{formatTimestamp(release.createdAt)}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		{:else}
			<p class="empty">No releases published yet.</p>
		{/if}
	</section>

	<section class="card">
		<h2>Server Definitions</h2>
		<p class="meta">
			A reusable template for creating servers — edition, version, and download URL are pinned when you save it,
			not re-resolved later. Use one from the "Create Server" page on any agent.
		</p>
		<form method="POST" action="?/createDefinition" use:enhance class="release-form">
			<label class="wide">
				Name
				<input type="text" name="name" placeholder="Modded Survival Preset" required />
			</label>
			<label>
				Edition
				<select name="game_platform" bind:value={definitionEdition}>
					<option value="java">Java</option>
					<option value="bedrock">Bedrock</option>
				</select>
			</label>
			{#if definitionEdition === 'java'}
				<label>
					Software
					<select name="software_type" bind:value={definitionSoftwareType}>
						<option value="vanilla">Vanilla</option>
						<option value="paper">Paper</option>
						<option value="fabric">Fabric</option>
						<option value="forge">Forge</option>
					</select>
				</label>
				<label>
					Version
					<select name="catalog_id" bind:value={definitionCatalogId}>
						{#each definitionJavaVersions as opt (opt.id)}
							<option value={opt.id}>{opt.version} (Java {opt.javaMajorVersion})</option>
						{/each}
					</select>
				</label>
			{:else}
				<label>
					Version
					<select name="catalog_id" bind:value={definitionCatalogId}>
						{#each data.bedrockVersions as opt (opt.id)}
							<option value={opt.id}>{opt.version}</option>
						{/each}
						{#if data.bedrockVersions.length === 0}
							<option value="">(unknown — enter URL manually)</option>
						{/if}
					</select>
				</label>
				<label class="wide">
					Download URL
					<input
						type="text"
						name="download_url"
						bind:value={definitionBedrockUrl}
						placeholder="https://www.minecraft.net/.../bedrock-server-....zip"
					/>
				</label>
			{/if}
			<button type="submit">Save definition</button>
		</form>
		{#if form?.error}
			<p class="error">{form.error}</p>
		{/if}
		{#if form?.definitionCreated}
			<p class="meta">Saved.</p>
		{/if}

		{#if data.definitions.length > 0}
			<table class="releases">
				<thead>
					<tr>
						<th>Name</th>
						<th>Edition</th>
						<th>Software</th>
						<th>Version</th>
						<th></th>
					</tr>
				</thead>
				<tbody>
					{#each data.definitions as def (def.id)}
						<tr>
							<td>{def.name}</td>
							<td>{def.gamePlatform}</td>
							<td>{def.softwareType}</td>
							<td>{def.version}</td>
							<td>
								<form method="POST" action="?/deleteDefinition" use:enhance>
									<input type="hidden" name="id" value={def.id} />
									<button type="submit" class="ghost">Delete</button>
								</form>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		{:else}
			<p class="empty">No definitions saved yet.</p>
		{/if}
	</section>
</div>

<style>
	.page {
		min-height: 100vh;
		background: var(--axon-background);
		color: var(--axon-text);
		padding: 1.5rem 2rem 4rem;
		max-width: 960px;
		margin: 0 auto;
	}

	header {
		margin-bottom: 1.5rem;
	}

	.breadcrumb {
		margin: 0 0 0.5rem;
		font-size: 0.875rem;
	}

	.breadcrumb a {
		color: var(--axon-text);
		opacity: 0.7;
	}

	.card {
		background: var(--axon-surface);
		border: 1px solid var(--axon-accent);
		border-radius: 0.75rem;
		padding: 1.25rem;
		margin-bottom: 1.25rem;
	}

	.card h2 {
		margin-top: 0;
	}

	.meta {
		font-size: 0.8rem;
		opacity: 0.7;
		margin: 0.15rem 0 1rem;
	}

	.token-display {
		margin-top: 0.75rem;
	}

	.hint {
		font-size: 0.8rem;
		opacity: 0.75;
	}

	.error {
		color: var(--axon-status-error);
		font-size: 0.8rem;
		margin: 0.5rem 0 0;
	}

	.empty {
		opacity: 0.7;
		font-size: 0.85rem;
	}

	.release-form {
		display: flex;
		align-items: flex-end;
		gap: 0.75rem;
		flex-wrap: wrap;
		margin-bottom: 0.75rem;
	}

	.release-form label {
		display: flex;
		flex-direction: column;
		gap: 0.25rem;
		font-size: 0.8rem;
		opacity: 0.85;
	}

	.release-form label.wide {
		flex: 1 1 16rem;
	}

	.release-form input,
	.release-form select {
		padding: 0.5rem 0.625rem;
		border-radius: 0.375rem;
		border: 1px solid var(--axon-accent);
		background: var(--axon-background);
		color: var(--axon-text);
		width: 100%;
	}

	.releases {
		width: 100%;
		border-collapse: collapse;
		margin-top: 1rem;
		font-size: 0.8rem;
	}

	.releases th,
	.releases td {
		text-align: left;
		padding: 0.4rem 0.6rem;
		border-bottom: 1px solid var(--axon-accent);
	}

	.releases th {
		opacity: 0.7;
		font-weight: 600;
	}

	.url-cell {
		max-width: 20rem;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	button {
		padding: 0.4rem 0.9rem;
		border-radius: 0.375rem;
		border: none;
		background: var(--axon-primary);
		color: var(--axon-background);
		font-weight: 600;
		cursor: pointer;
	}

	button.ghost {
		background: transparent;
		border: 1px solid var(--axon-accent);
		color: var(--axon-text);
	}
</style>
