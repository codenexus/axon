<script lang="ts">
	import { enhance } from '$app/forms';
	import { invalidateAll } from '$app/navigation';
	import { heartbeatProgress, nextHeartbeatLabel } from '$lib/heartbeat';
	import ConfirmModal from '$lib/components/ConfirmModal.svelte';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();

	// Restore confirmation: the button opens the themed modal instead of
	// submitting directly; confirming it programmatically submits the
	// matching hidden form via requestSubmit().
	let confirmingRestoreId = $state<string | null>(null);
	let restoreForms: Record<string, HTMLFormElement> = {};

	function isInFlight(backup: (typeof data.backups)[number]): boolean {
		return backup.status === 'pending' || backup.status === 'running' || !!backup.pendingOperation;
	}

	// While a restore is actively running, its auto-created safety backup
	// sits at status='pending' with trigger='pre_restore' — showing a
	// heartbeat countdown on that row too would just duplicate the one
	// already shown on the backup the admin actually clicked Restore on.
	// That's only true for this exact window: once the safety backup
	// resolves (complete/failed), any operation on it afterward (e.g.
	// deleting it) is independent and should get its own bar like anything
	// else.
	function suppressRedundantBar(backup: (typeof data.backups)[number]): boolean {
		return backup.trigger === 'pre_restore' && backup.status === 'pending';
	}

	// Poll unconditionally while this page is open, rather than only while
	// this page's own data already shows something in flight — that
	// reactive-only approach misses a backup queued from elsewhere (another
	// tab, the API directly) while this tab sat idle, since nothing would
	// tell an idle tab to start checking.
	$effect(() => {
		const interval = setInterval(() => invalidateAll(), 3000);
		return () => clearInterval(interval);
	});

	// A separate 1s local tick (no network call) just to animate the
	// countdown below — decoupled from the 3s poll above so the readout
	// feels live even between actual data refreshes.
	let now = $state(Date.now());
	$effect(() => {
		const anyPreparing = Object.values(data.downloadsByBackupId).some((d) => d.status === 'requested');
		if (!data.backups.some(isInFlight) && !anyPreparing) return;

		const tick = setInterval(() => (now = Date.now()), 1000);
		return () => clearInterval(tick);
	});

	// Auto-trigger the actual file download the moment it becomes ready,
	// rather than making the admin come back and click a second time once
	// "Preparing…" resolves. The visible "Download ⬇" link stays as a
	// manual fallback/re-trigger. Guarded by this set so a later poll of the
	// same already-ready backup doesn't keep re-firing it.
	const autoDownloaded = new Set<string>();
	$effect(() => {
		for (const [backupId, d] of Object.entries(data.downloadsByBackupId)) {
			if (d.status !== 'ready' || autoDownloaded.has(backupId)) continue;
			autoDownloaded.add(backupId);
			const a = document.createElement('a');
			a.href = `/backups/${backupId}/download-file`;
			a.download = '';
			document.body.appendChild(a);
			a.click();
			a.remove();
		}
	});

	function formatBytes(bytes: number | null | undefined): string {
		if (!bytes) return '—';
		if (bytes < 1024 ** 3) return `${(bytes / 1024 ** 2).toFixed(1)} MB`;
		return `${(bytes / 1024 ** 3).toFixed(2)} GB`;
	}

	function formatTimestamp(ms: number | null | undefined): string {
		if (!ms) return '—';
		return new Date(ms).toLocaleString();
	}

	const statusLabel: Record<string, string> = {
		pending: 'Pending',
		running: 'Running',
		complete: 'Complete',
		failed: 'Failed'
	};

	const statusClass: Record<string, string> = {
		pending: 'badge-info',
		running: 'badge-warning',
		complete: 'badge-success',
		failed: 'badge-error'
	};

	const pendingOperationLabel: Record<string, string> = {
		delete: 'Deleting…',
		restore: 'Restoring…'
	};

	function downloadState(backup: (typeof data.backups)[number]): 'none' | 'preparing' | 'ready' {
		const d = data.downloadsByBackupId[backup.id];
		if (!d) return 'none';
		if (d.status === 'requested') return 'preparing';
		if (d.status === 'ready') return 'ready';
		return 'none'; // expired/failed — treat like never requested, admin can just click again
	}
</script>

<svelte:head>
	<title>Axon Panel — {data.instance.name} Backups</title>
</svelte:head>

<div class="page">
	<header>
		<div>
			<p class="breadcrumb"><a href="/">← Dashboard</a></p>
			<h1>{data.instance.name}</h1>
			<p class="meta">{data.instance.gamePlatform} · {data.instance.softwareType} {data.instance.version}</p>
		</div>
	</header>

	<section class="card">
		<div class="section-header">
			<h2>Backups</h2>
			<form method="POST" action="?/createBackup" use:enhance>
				<button type="submit">Create backup</button>
			</form>
		</div>

		{#if data.backups.length === 0}
			<p class="empty">No backups yet.</p>
		{:else}
			<ul class="backups">
				{#each data.backups as backup (backup.id)}
					<li>
						<div class="backup-row">
							<div class="backup-main">
								<span
									class="badge {statusClass[backup.status] ?? 'badge-info'} {isInFlight(backup)
										? 'badge-pulsing'
										: ''}"
								>
									{statusLabel[backup.status] ?? backup.status}
								</span>
								{#if backup.pendingOperation}
									<span class="badge badge-warning-outline badge-pulsing">
										{pendingOperationLabel[backup.pendingOperation] ?? backup.pendingOperation}
									</span>
								{/if}
								{#if downloadState(backup) === 'preparing'}
									<span class="badge badge-warning-outline badge-pulsing">Preparing download…</span>
								{/if}
								<span class="meta">{formatTimestamp(backup.createdAt)}</span>
								<span class="meta">{backup.trigger}</span>
								<span class="meta">{formatBytes(backup.sizeBytes)}</span>
							</div>
							{#if (isInFlight(backup) || downloadState(backup) === 'preparing') && !suppressRedundantBar(backup) && data.agentHeartbeat?.lastSeenAt}
								<div class="heartbeat-bar" title={nextHeartbeatLabel(data.agentHeartbeat, now)}>
									<div
										class="heartbeat-bar-fill"
										class:heartbeat-bar-overdue={heartbeatProgress(data.agentHeartbeat, now) >= 1}
										style="width: {heartbeatProgress(data.agentHeartbeat, now) * 100}%"
									></div>
								</div>
								<p class="meta">{nextHeartbeatLabel(data.agentHeartbeat, now)}</p>
							{/if}
							{#if backup.errorMessage}
								<p class="error">{backup.errorMessage}</p>
							{/if}
						</div>
						<div class="backup-actions">
							{#if backup.status === 'complete'}
								{#if downloadState(backup) === 'ready'}
									<a class="ghost-link" href="/backups/{backup.id}/download-file">Download ⬇</a>
								{:else if downloadState(backup) === 'preparing'}
									<button type="button" class="ghost" disabled>Preparing…</button>
								{:else}
									<form method="POST" action="?/downloadBackup" use:enhance>
										<input type="hidden" name="backup_id" value={backup.id} />
										<button type="submit" class="ghost">Download</button>
									</form>
								{/if}
							{/if}
							{#if backup.status === 'complete'}
								<form
									method="POST"
									action="?/restoreBackup"
									use:enhance
									bind:this={restoreForms[backup.id]}
								>
									<input type="hidden" name="backup_id" value={backup.id} />
									<button
										type="button"
										class="ghost"
										disabled={!!backup.pendingOperation}
										onclick={() => (confirmingRestoreId = backup.id)}
									>
										Restore
									</button>
								</form>
							{/if}
							<form method="POST" action="?/deleteBackup" use:enhance>
								<input type="hidden" name="backup_id" value={backup.id} />
								<button
									type="submit"
									class="ghost"
									disabled={backup.status === 'pending' ||
										backup.status === 'running' ||
										!!backup.pendingOperation ||
										downloadState(backup) === 'preparing'}
								>
									Delete
								</button>
							</form>
						</div>
					</li>
				{/each}
			</ul>
		{/if}
	</section>
</div>

<ConfirmModal
	open={confirmingRestoreId !== null}
	title="Restore backup?"
	message="This overwrites the current world with the contents of this backup and cannot be undone. A safety backup of the current state is taken automatically first."
	confirmLabel="Restore"
	danger
	onConfirm={() => {
		const id = confirmingRestoreId;
		confirmingRestoreId = null;
		if (id) restoreForms[id]?.requestSubmit();
	}}
	onCancel={() => (confirmingRestoreId = null)}
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

	.meta {
		font-size: 0.8rem;
		opacity: 0.7;
		margin: 0.15rem 0 0;
	}

	.card {
		background: var(--axon-surface);
		border: 1px solid var(--axon-accent);
		border-radius: 0.75rem;
		padding: 1.25rem;
		margin-bottom: 1.25rem;
	}

	.section-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: 1rem;
	}

	.section-header h2 {
		margin: 0;
	}

	.backups {
		list-style: none;
		padding: 0;
		margin: 0;
		display: flex;
		flex-direction: column;
		gap: 0.75rem;
	}

	.backups li {
		border-top: 1px solid var(--axon-accent);
		padding-top: 0.75rem;
		display: flex;
		justify-content: space-between;
		align-items: flex-start;
		gap: 1rem;
	}

	.backup-row {
		display: flex;
		flex-direction: column;
		gap: 0.25rem;
	}

	.backup-main {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		flex-wrap: wrap;
	}

	.backup-actions {
		display: flex;
		gap: 0.5rem;
	}

	.badge {
		display: inline-block;
		padding: 0.1rem 0.5rem;
		border-radius: 999px;
		font-size: 0.75rem;
		font-weight: 600;
		color: white;
	}

	.badge-success {
		background: var(--axon-status-success);
	}
	.badge-warning {
		background: var(--axon-status-warning);
	}
	.badge-error {
		background: var(--axon-status-error);
	}
	.badge-info {
		background: var(--axon-status-info);
	}
	.badge-warning-outline {
		background: transparent;
		border: 1px solid var(--axon-status-warning);
		color: var(--axon-status-warning);
	}

	.badge-pulsing {
		animation: badge-pulse 1.4s ease-in-out infinite;
	}

	.heartbeat-bar {
		width: min(200px, 100%);
		height: 0.3rem;
		border-radius: 999px;
		background: var(--axon-accent);
		margin-top: 0.4rem;
		overflow: hidden;
	}

	.heartbeat-bar-fill {
		height: 100%;
		background: var(--axon-status-info);
		border-radius: 999px;
		transition: width 1s linear;
	}

	.heartbeat-bar-fill.heartbeat-bar-overdue {
		background: var(--axon-status-warning);
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

	.error {
		color: var(--axon-status-error);
		font-size: 0.8rem;
		margin: 0;
	}

	.empty {
		opacity: 0.7;
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

	button:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	button.ghost {
		background: transparent;
		border: 1px solid var(--axon-accent);
		color: var(--axon-text);
	}

	.ghost-link {
		padding: 0.4rem 0.9rem;
		border-radius: 0.375rem;
		border: 1px solid var(--axon-accent);
		color: var(--axon-text);
		text-decoration: none;
		font-size: 0.875rem;
		display: inline-flex;
		align-items: center;
	}
</style>
