<script lang="ts">
	import { enhance } from '$app/forms';
	import ThemeSwitcher from '$lib/theme/ThemeSwitcher.svelte';
	import type { ActionData, PageData } from './$types';

	let { data, form }: { data: PageData; form: ActionData } = $props();

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
			<ThemeSwitcher />
			<form method="POST" action="?/logout" use:enhance>
				<button type="submit" class="ghost">Log out</button>
			</form>
		</div>
	</header>

	<section class="enroll">
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

	{#if data.agents.length === 0}
		<p class="empty">No Pulse agents enrolled yet.</p>
	{/if}

	{#each data.agents as agent (agent.id)}
		<section class="agent-card">
			<div class="agent-header">
				<div>
					<h2>{agent.hostname}</h2>
					<p class="meta">{agent.os}/{agent.arch} · Pulse v{agent.pulseVersion} · last seen {lastSeenLabel(agent.lastSeenAt)}</p>
				</div>
				<div class="host-metrics">
					<span>CPU {agent.cpuUsagePercent?.toFixed(0) ?? '—'}%</span>
					<span>RAM {formatBytes(agent.ramUsedBytes)} / {formatBytes(agent.ramTotalBytes)}</span>
				</div>
			</div>

			{#if agent.instances.length === 0}
				<p class="empty">No server instances reported yet.</p>
			{:else}
				<ul class="instances">
					{#each agent.instances as instance (instance.id)}
						<li>
							<div class="instance-name">
								<strong>{instance.name}</strong>
								<span class="badge {stateClass[instance.runningState] ?? 'badge-info'}">
									{stateLabel[instance.runningState] ?? instance.runningState}
								</span>
							</div>
							<span class="meta">{instance.gamePlatform} · {instance.softwareType} {instance.version}</span>
							<div class="instance-actions">
								<form method="POST" action="?/queueCommand" use:enhance>
									<input type="hidden" name="pulse_agent_id" value={agent.id} />
									<input type="hidden" name="instance_id" value={instance.instanceId} />
									<input type="hidden" name="type" value="start_instance" />
									<button
										type="submit"
										disabled={instance.runningState === 'running' || instance.runningState === 'starting'}
									>
										Start
									</button>
								</form>
								<form method="POST" action="?/queueCommand" use:enhance>
									<input type="hidden" name="pulse_agent_id" value={agent.id} />
									<input type="hidden" name="instance_id" value={instance.instanceId} />
									<input type="hidden" name="type" value="stop_instance" />
									<button
										type="submit"
										class="ghost"
										disabled={instance.runningState === 'stopped' || instance.runningState === 'stopping'}
									>
										Stop
									</button>
								</form>
							</div>
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

	.enroll {
		background: var(--axon-surface);
		border: 1px solid var(--axon-accent);
		border-radius: 0.75rem;
		padding: 1rem 1.25rem;
		margin-bottom: 1.5rem;
	}

	.token-display {
		margin-top: 0.75rem;
	}

	.hint {
		font-size: 0.8rem;
		opacity: 0.75;
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
		display: flex;
		flex-direction: column;
		gap: 0.25rem;
	}

	.instance-name {
		display: flex;
		align-items: center;
		gap: 0.5rem;
	}

	.instance-actions {
		display: flex;
		gap: 0.5rem;
		margin-top: 0.25rem;
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
</style>
