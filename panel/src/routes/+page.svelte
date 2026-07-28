<script lang="ts">
	import { enhance } from '$app/forms';
	import { invalidateAll } from '$app/navigation';
	import { heartbeatProgress, isOnline as isAgentOnline, nextHeartbeatLabel } from '$lib/heartbeat';
	import { formatDuration } from '$lib/format';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();

	// Poll unconditionally while the dashboard is open, rather than only
	// continuing to poll once something's already known (from this page's
	// own data) to be in-flight. That reactive-only approach missed changes
	// triggered elsewhere (e.g. a backup queued from the instance detail
	// page) whenever the dashboard was idle when they started — there was
	// nothing to tell an idle page to start checking. A steady baseline
	// poll catches all of that uniformly: start/stop transitions, backups
	// queued from another page/tab, anything.
	$effect(() => {
		const interval = setInterval(() => invalidateAll(), 5000);
		return () => clearInterval(interval);
	});

	// Local 1s tick (no network call) driving the heartbeat countdown bar
	// shown next to a pending action, so it feels live between polls.
	let now = $state(Date.now());
	$effect(() => {
		const tick = setInterval(() => (now = Date.now()), 1000);
		return () => clearInterval(tick);
	});

	function formatBytes(bytes: number | null | undefined): string {
		if (!bytes) return '—';
		const gb = bytes / 1024 ** 3;
		return `${gb.toFixed(1)} GB`;
	}

	function lastSeenLabel(lastSeenAt: number | null): string {
		if (!lastSeenAt) return 'never';
		const seconds = Math.round((Date.now() - lastSeenAt) / 1000);
		if (seconds < 90) return `${seconds}s ago`;
		return `${Math.round(seconds / 60)}m ago`;
	}

	function isOnline(agent: { lastSeenAt: number | null; intervalSeconds: number | null }): boolean {
		return isAgentOnline(agent, Date.now());
	}

	function isBusy(instance: { id: string }): boolean {
		return !!data.instancesBackingUp[instance.id] || !!data.pendingActions[instance.id];
	}

	const backupBadgeLabel: Record<string, string> = {
		backup: 'Backing up…',
		restore: 'Restoring…'
	};

	const pendingActionLabel: Record<string, string> = {
		starting: 'Starting…',
		stopping: 'Stopping…',
		restarting: 'Restarting…',
		deleting: 'Deleting…'
	};

	const stateLabel: Record<string, string> = {
		stopped: 'Stopped',
		starting: 'Starting',
		running: 'Running',
		stopping: 'Stopping',
		crashed: 'Crashed'
	};

	const stateClass: Record<string, string> = {
		stopped: 'badge-info',
		starting: 'badge-warning',
		running: 'badge-success',
		stopping: 'badge-warning',
		crashed: 'badge-error'
	};
</script>

<svelte:head>
	<title>Axon Panel — Dashboard</title>
</svelte:head>

<div class="page">
	<header>
		<h1>Axon Panel</h1>
		<div class="header-actions">
			<a class="icon-link" href="/settings" title="Settings" aria-label="Settings">
				<svg
					viewBox="0 0 24 24"
					width="20"
					height="20"
					fill="none"
					stroke="currentColor"
					stroke-width="2"
					stroke-linecap="round"
					stroke-linejoin="round"
				>
					<circle cx="12" cy="12" r="3"></circle>
					<path
						d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"
					></path>
				</svg>
			</a>
			<form method="POST" action="?/logout" use:enhance>
				<button type="submit" class="ghost">Log out</button>
			</form>
		</div>
	</header>

	{#if data.agents.length === 0}
		<p class="empty">No Pulse agents enrolled yet.</p>
	{/if}

	{#each data.agents as agent (agent.id)}
		<section class="agent-card">
			<div class="agent-header">
				<div>
					<h2>
						<span
							class="status-dot {isOnline(agent) ? 'status-dot-online' : 'status-dot-offline'}"
							title={isOnline(agent) ? 'Online' : 'Offline'}
						></span>
						{agent.hostname}
					</h2>
					<p class="meta">
						{agent.os}/{agent.arch} · Pulse v{agent.pulseVersion}
						{#if agent.updateAvailable}
							<span class="update-note">→ v{agent.updateAvailable} available</span>
						{/if}
						· last seen {lastSeenLabel(agent.lastSeenAt)}
					</p>
				</div>
				<div class="host-metrics">
					<span>CPU {agent.cpuUsagePercent?.toFixed(0) ?? '—'}%</span>
					<span>RAM {formatBytes(agent.ramUsedBytes)} / {formatBytes(agent.ramTotalBytes)}</span>
					<a class="ghost-link manage-link" href="/agents/{agent.id}">Manage →</a>
				</div>
			</div>

			{#if agent.instances.length === 0}
				<p class="empty">No server instances reported yet.</p>
			{:else}
				<ul class="instances">
					{#each agent.instances as instance (instance.id)}
						<li>
							<div class="instance-row">
								<div class="instance-info">
									<div class="instance-name">
										<svg
											class="server-icon"
											viewBox="0 0 24 24"
											width="14"
											height="14"
											fill="none"
											stroke="currentColor"
											stroke-width="2"
											stroke-linecap="round"
											stroke-linejoin="round"
											aria-hidden="true"
										>
											<title>Minecraft server</title>
											<path d="M12 2 L21 7 L21 17 L12 22 L3 17 L3 7 Z"></path>
											<path d="M12 22 L12 12"></path>
											<path d="M21 7 L12 12 L3 7"></path>
										</svg>
										<strong>{instance.name}</strong>
										{#if data.instancesBackingUp[instance.id]}
											<span
												class="badge badge-warning badge-pulsing"
												title="A backup/restore's stop→archive→restart cycle happens between heartbeats, so running_state can't be trusted until it's done"
											>
												{backupBadgeLabel[data.instancesBackingUp[instance.id]]}
											</span>
										{:else if data.pendingActions[instance.id]}
											<span
												class="badge badge-warning badge-pulsing"
												title="Command sent to Pulse; running_state often jumps straight to the new state without ever showing this in between"
											>
												{pendingActionLabel[data.pendingActions[instance.id]]}
											</span>
										{:else}
											<span
												class="badge {stateClass[instance.runningState] ?? 'badge-info'} {instance.runningState ===
													'starting' || instance.runningState === 'stopping'
													? 'badge-pulsing'
													: ''}"
											>
												{stateLabel[instance.runningState] ?? instance.runningState}
											</span>
										{/if}
									</div>
									<span class="meta">{instance.gamePlatform} · {instance.softwareType} {instance.version}</span>
								</div>
								<div class="instance-side">
									{#if instance.runningState === 'running'}
										<span class="instance-stats"
											>{instance.playerCount} player{instance.playerCount === 1 ? '' : 's'} · up {formatDuration(
												instance.uptimeSeconds
											)}</span
										>
									{/if}
									<div class="instance-actions">
										<form method="POST" action="?/queueCommand" use:enhance>
											<input type="hidden" name="pulse_agent_id" value={agent.id} />
											<input type="hidden" name="instance_id" value={instance.instanceId} />
											<input
												type="hidden"
												name="type"
												value={instance.runningState === 'running' ? 'restart_instance' : 'start_instance'}
											/>
											<button
												type="submit"
												disabled={isBusy(instance) ||
													instance.runningState === 'starting' ||
													instance.runningState === 'stopping'}
											>
												{instance.runningState === 'running' ? 'Restart' : 'Start'}
											</button>
										</form>
										<form method="POST" action="?/queueCommand" use:enhance>
											<input type="hidden" name="pulse_agent_id" value={agent.id} />
											<input type="hidden" name="instance_id" value={instance.instanceId} />
											<input type="hidden" name="type" value="stop_instance" />
											<button
												type="submit"
												class="ghost"
												disabled={isBusy(instance) ||
													instance.runningState === 'stopped' ||
													instance.runningState === 'stopping'}
											>
												Stop
											</button>
										</form>
									</div>
									<a class="ghost-link backups-link" href="/instances/{instance.id}">Manage →</a>
								</div>
							</div>
							{#if isBusy(instance) && agent.lastSeenAt}
								<div class="heartbeat-bar" title={nextHeartbeatLabel(agent, now)}>
									<div
										class="heartbeat-bar-fill"
										class:heartbeat-bar-overdue={heartbeatProgress(agent, now) >= 1}
										style="width: {heartbeatProgress(agent, now) * 100}%"
									></div>
								</div>
								<p class="meta">{nextHeartbeatLabel(agent, now)}</p>
							{/if}
						</li>
					{/each}
				</ul>
			{/if}
		</section>
	{/each}
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
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: 1.5rem;
	}

	.header-actions {
		display: flex;
		align-items: center;
		gap: 1rem;
	}

	.agent-card {
		background: var(--axon-surface);
		border: 1px solid var(--axon-accent);
		border-radius: 0.75rem;
		padding: 1.25rem;
		margin-bottom: 1.25rem;
	}

	.agent-header {
		display: flex;
		justify-content: space-between;
		align-items: flex-start;
		gap: 1rem;
	}

	.host-metrics {
		display: flex;
		flex-direction: column;
		align-items: flex-end;
		font-size: 0.875rem;
		opacity: 0.85;
	}

	.meta {
		font-size: 0.8rem;
		opacity: 0.7;
		margin: 0.15rem 0 0;
	}

	.update-note {
		color: var(--axon-status-info);
		opacity: 1;
	}

	.instances {
		list-style: none;
		padding: 0;
		margin: 1rem 0 0;
		display: flex;
		flex-direction: column;
		gap: 0.75rem;
	}

	.instances li {
		border-top: 1px solid var(--axon-accent);
		padding-top: 0.75rem;
	}

	.instance-row {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: 1rem;
		flex-wrap: wrap;
	}

	.instance-info {
		display: flex;
		flex-direction: column;
		gap: 0.25rem;
	}

	.instance-side {
		display: flex;
		align-items: center;
		gap: 1rem;
	}

	.instance-stats {
		font-size: 0.8rem;
		opacity: 0.85;
		white-space: nowrap;
	}

	.instance-name {
		display: flex;
		align-items: center;
		gap: 0.5rem;
	}

	.server-icon {
		opacity: 0.6;
		flex-shrink: 0;
	}

	.instance-actions {
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

	.agent-header h2 {
		display: flex;
		align-items: center;
		margin: 0;
	}

	.status-dot {
		display: inline-block;
		width: 0.55rem;
		height: 0.55rem;
		border-radius: 50%;
		margin-right: 0.5rem;
		flex-shrink: 0;
	}

	.status-dot-online {
		background: var(--axon-status-success);
	}

	.status-dot-offline {
		background: var(--axon-status-error);
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

	.icon-link {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 2.1rem;
		height: 2.1rem;
		border-radius: 0.375rem;
		border: 1px solid var(--axon-accent);
		color: var(--axon-text);
	}

	.backups-link {
		align-self: flex-start;
		margin-top: 0.25rem;
	}

	.manage-link {
		margin-top: 0.5rem;
		padding: 0.25rem 0.7rem;
		font-size: 0.8rem;
	}

	.heartbeat-bar {
		width: min(220px, 100%);
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
</style>
