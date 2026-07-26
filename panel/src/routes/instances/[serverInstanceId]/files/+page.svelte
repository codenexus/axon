<script lang="ts">
	import { enhance } from '$app/forms';
	import { goto, invalidateAll } from '$app/navigation';
	import type { ActionResult } from '@sveltejs/kit';
	import ConfirmModal from '$lib/components/ConfirmModal.svelte';
	import type { ActionData, PageData } from './$types';

	let { data, form }: { data: PageData; form: ActionData } = $props();

	// Mirrors protocol.ts's FileEntry — can't import a $lib/server/* type
	// into a .svelte file, so duplicated here (small enough that this
	// codebase's established per-file-duplication convention applies).
	interface FileEntry {
		name: string;
		path: string;
		is_dir: boolean;
		size_bytes: number;
		mod_time_ms: number;
	}

	let entries = $state<FileEntry[]>([]);
	// Tracks which path `entries` actually reflects — distinct from the
	// command-id guard below, since that guard fires for upload/delete
	// completions too (which don't carry a listing) and for a stale
	// listing whose path no longer matches where the admin has navigated.
	let entriesForPath = $state<string | null>(null);
	// Guards against re-processing the same already-applied command on
	// every 1s/3s poll — same pattern as the properties editor's
	// loadedPropertiesCommandId.
	let loadedFilesCommandId = $state<string | null>(null);

	$effect(() => {
		const cmd = data.latestFilesCommand;
		if (!cmd || cmd.status !== 'completed' || cmd.id === loadedFilesCommandId) return;
		loadedFilesCommandId = cmd.id;
		if (cmd.type !== 'list_files') return;
		const payload = cmd.payload ? (JSON.parse(cmd.payload) as { path?: string }) : null;
		if ((payload?.path ?? '') !== data.path) return; // stale result for a directory we've since navigated away from
		try {
			entries = cmd.output ? (JSON.parse(cmd.output) as FileEntry[]) : [];
		} catch {
			entries = [];
		}
		entriesForPath = data.path;
	});

	const hasCurrentListing = $derived(entriesForPath === data.path);

	// Auto-refresh the listing once an upload or delete resolves, rather
	// than making the admin click Load again — mirrors the existing
	// auto-download-trigger effect's "programmatically follow up once a
	// command resolves" pattern.
	let refreshedForCommandId: string | null = null;
	let listForm: HTMLFormElement;
	let listPathInput: HTMLInputElement;

	$effect(() => {
		const cmd = data.latestFilesCommand;
		if (!cmd || cmd.status !== 'completed') return;
		if (cmd.type !== 'upload_file' && cmd.type !== 'delete_file') return;
		if (cmd.id === refreshedForCommandId) return;
		refreshedForCommandId = cmd.id;
		listPathInput.value = data.path;
		listForm.requestSubmit();
	});

	// A files command sent now only reaches Pulse on its next heartbeat —
	// same latency reality as console/properties. Poll faster while one's
	// in flight, page-local (this route has its own lifecycle).
	const filesInFlight = $derived(
		data.latestFilesCommand?.status === 'queued' || data.latestFilesCommand?.status === 'sent'
	);
	$effect(() => {
		const ms = filesInFlight ? 1000 : 3000;
		const interval = setInterval(() => invalidateAll(), ms);
		return () => clearInterval(interval);
	});

	function handleListSubmit() {
		return async ({ result }: { result: ActionResult }) => {
			if (result.type === 'success' && typeof result.data?.path === 'string') {
				await goto(`?path=${encodeURIComponent(result.data.path)}`, { invalidateAll: true });
			}
		};
	}

	function navigateTo(path: string) {
		listPathInput.value = path;
		listForm.requestSubmit();
	}

	const breadcrumbSegments = $derived(data.path ? data.path.split('/') : []);
	function breadcrumbPath(index: number): string {
		return breadcrumbSegments.slice(0, index + 1).join('/');
	}
	function upOneLevelPath(): string {
		return breadcrumbSegments.slice(0, -1).join('/');
	}

	function formatBytes(bytes: number | null | undefined): string {
		if (!bytes) return '—';
		if (bytes < 1024 ** 3) return `${(bytes / 1024 ** 2).toFixed(1)} MB`;
		return `${(bytes / 1024 ** 3).toFixed(2)} GB`;
	}

	function formatTimestamp(ms: number | null | undefined): string {
		if (!ms) return '—';
		return new Date(ms).toLocaleString();
	}

	let confirmingDeletePath = $state<string | null>(null);
	let confirmingDeleteIsDir = $state(false);
	let deleteForms: Record<string, HTMLFormElement> = {};
</script>

<svelte:head>
	<title>Axon Panel — {data.instance.name} Files</title>
</svelte:head>

<div class="page">
	<header>
		<p class="breadcrumb"><a href="/instances/{data.instance.id}">← {data.instance.name}</a></p>
		<h1>Files</h1>
	</header>

	<section class="card">
		<!-- Hidden navigation form: every folder click / breadcrumb segment /
		     "Up one level" / "Load" button drives this same form, then
		     handleListSubmit navigates the URL's ?path= to match. -->
		<form method="POST" action="?/listPath" use:enhance={handleListSubmit} bind:this={listForm} class="hidden-form">
			<input type="hidden" name="path" bind:this={listPathInput} value={data.path} />
		</form>

		<div class="path-bar">
			<div class="breadcrumbs">
				<button type="button" class="crumb" onclick={() => navigateTo('')}>Home</button>
				{#each breadcrumbSegments as segment, i (i)}
					<span class="crumb-sep">/</span>
					<button type="button" class="crumb" onclick={() => navigateTo(breadcrumbPath(i))}>{segment}</button>
				{/each}
			</div>
			<div class="path-actions">
				{#if breadcrumbSegments.length > 0}
					<button type="button" class="ghost" onclick={() => navigateTo(upOneLevelPath())}>Up one level</button>
				{/if}
				<button type="button" class="ghost" onclick={() => navigateTo(data.path)}>
					{hasCurrentListing ? 'Refresh' : 'Load'}
				</button>
			</div>
		</div>

		{#if filesInFlight}
			<span class="badge badge-warning badge-pulsing">
				{data.latestFilesCommand?.type === 'upload_file'
					? 'Uploading…'
					: data.latestFilesCommand?.type === 'delete_file'
						? 'Deleting…'
						: 'Loading…'}
			</span>
		{:else if data.latestFilesCommand?.status === 'failed' && !hasCurrentListing}
			<p class="error">{data.latestFilesCommand.resultMessage}</p>
		{/if}

		{#if hasCurrentListing}
			{#if entries.length === 0}
				<p class="empty">This directory is empty.</p>
			{:else}
				<table class="files-table">
					<thead>
						<tr>
							<th>Name</th>
							<th>Size</th>
							<th>Modified</th>
							<th></th>
						</tr>
					</thead>
					<tbody>
						{#each entries as entry (entry.path)}
							<tr>
								<td>
									{#if entry.is_dir}
										<button type="button" class="entry-link" onclick={() => navigateTo(entry.path)}>
											📁 {entry.name}
										</button>
									{:else}
										<span class="entry-name">📄 {entry.name}</span>
									{/if}
								</td>
								<td class="meta">{entry.is_dir ? '—' : formatBytes(entry.size_bytes)}</td>
								<td class="meta">{formatTimestamp(entry.mod_time_ms)}</td>
								<td>
									<form
										method="POST"
										action="?/deleteEntry"
										use:enhance
										bind:this={deleteForms[entry.path]}
									>
										<input type="hidden" name="path" value={entry.path} />
										<button
											type="button"
											class="ghost"
											onclick={() => {
												confirmingDeletePath = entry.path;
												confirmingDeleteIsDir = entry.is_dir;
											}}
										>
											Delete
										</button>
									</form>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			{/if}

			<form method="POST" action="?/uploadFile" use:enhance class="upload-form">
				<input type="hidden" name="path" value={data.path} />
				<input type="file" name="file" required />
				<button type="submit">Upload to {data.path || 'this directory'}</button>
			</form>
			{#if form?.error}
				<p class="error">{form.error}</p>
			{/if}
		{/if}
	</section>
</div>

<ConfirmModal
	open={confirmingDeletePath !== null}
	title="Delete {confirmingDeleteIsDir ? 'folder' : 'file'}?"
	message={confirmingDeleteIsDir
		? 'This permanently deletes this folder and everything inside it. This cannot be undone.'
		: 'This permanently deletes this file. This cannot be undone.'}
	confirmLabel="Delete"
	danger
	onConfirm={() => {
		const path = confirmingDeletePath;
		confirmingDeletePath = null;
		if (path) deleteForms[path]?.requestSubmit();
	}}
	onCancel={() => (confirmingDeletePath = null)}
/>

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
	}

	.hidden-form {
		display: none;
	}

	.path-bar {
		display: flex;
		justify-content: space-between;
		align-items: center;
		flex-wrap: wrap;
		gap: 0.5rem;
		margin-bottom: 1rem;
	}

	.breadcrumbs {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: 0.25rem;
	}

	.crumb {
		background: none;
		border: none;
		color: var(--axon-status-info);
		cursor: pointer;
		padding: 0.15rem 0.25rem;
		font-size: 0.9rem;
	}

	.crumb-sep {
		opacity: 0.5;
	}

	.path-actions {
		display: flex;
		gap: 0.5rem;
	}

	.files-table {
		width: 100%;
		border-collapse: collapse;
		margin-bottom: 1rem;
	}

	.files-table th {
		text-align: left;
		font-size: 0.75rem;
		opacity: 0.7;
		padding: 0.3rem 0.5rem;
		border-bottom: 1px solid var(--axon-accent);
	}

	.files-table td {
		padding: 0.4rem 0.5rem;
		border-bottom: 1px solid var(--axon-accent);
		vertical-align: middle;
	}

	.entry-link {
		background: none;
		border: none;
		color: var(--axon-text);
		cursor: pointer;
		padding: 0;
		font-size: 0.9rem;
		text-align: left;
	}

	.entry-name {
		font-size: 0.9rem;
	}

	.meta {
		font-size: 0.8rem;
		opacity: 0.7;
	}

	.upload-form {
		display: flex;
		align-items: center;
		gap: 0.75rem;
		flex-wrap: wrap;
		padding-top: 0.75rem;
		border-top: 1px solid var(--axon-accent);
	}

	.upload-form input[type='file'] {
		font-size: 0.8rem;
	}

	.badge {
		display: inline-block;
		padding: 0.1rem 0.5rem;
		border-radius: 999px;
		font-size: 0.75rem;
		font-weight: 600;
		color: white;
		background: var(--axon-status-warning);
		margin-bottom: 0.75rem;
	}

	.badge-pulsing {
		animation: badge-pulse 1.4s ease-in-out infinite;
	}

	@keyframes badge-pulse {
		0%,
		100% {
			opacity: 1;
		}
		50% {
			opacity: 0.45;
		}
	}

	@media (prefers-reduced-motion: reduce) {
		.badge-pulsing {
			animation: none;
		}
	}

	.empty {
		opacity: 0.7;
	}

	.error {
		color: var(--axon-status-error);
		font-size: 0.8rem;
		margin: 0 0 0.75rem;
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
		padding: 0.3rem 0.7rem;
		font-size: 0.8rem;
	}
</style>
