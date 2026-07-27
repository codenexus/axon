<script lang="ts">
	import { enhance } from '$app/forms';
	import ThemeSwitcher from '$lib/theme/ThemeSwitcher.svelte';
	import type { ActionData, PageData } from './$types';

	let { data, form }: { data: PageData; form: ActionData } = $props();

	function formatTimestamp(ms: number): string {
		return new Date(ms).toLocaleString();
	}
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
</style>
