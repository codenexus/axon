<script lang="ts">
	import { enhance } from '$app/forms';
	import type { ActionData, PageData } from './$types';

	let { data, form }: { data: PageData; form: ActionData } = $props();

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

	const isConfigured = $derived(
		data.agent.portRangeStart != null && data.agent.portRangeEnd != null && !!data.agent.instancesRootDir
	);
</script>

<svelte:head>
	<title>Axon Panel — {data.agent.hostname}</title>
</svelte:head>

<div class="page">
	<header>
		<p class="breadcrumb"><a href="/">← Dashboard</a></p>
		<h1>{data.agent.hostname}</h1>
		<p class="meta">
			{data.agent.os}/{data.agent.arch} · Pulse v{data.agent.pulseVersion}
			{#if data.updateAvailable}
				<span class="update-note">→ v{data.updateAvailable} available</span>
			{/if}
		</p>
	</header>

	<section class="card">
		<div class="section-header">
			<h2>Server provisioning</h2>
		</div>
		<p class="meta">
			Configure a port range and a base directory before creating new servers on this agent. Panel picks the next
			free port in the range for each new server; the admin is responsible for choosing a range that doesn't
			collide with anything already configured outside Panel's knowledge.
		</p>

		<form method="POST" action="?/saveAgentSettings" use:enhance class="settings-form">
			<label>
				Port range start
				<input
					type="number"
					name="port_range_start"
					min="1"
					step="1"
					value={data.agent.portRangeStart ?? ''}
				/>
			</label>
			<label>
				Port range end
				<input type="number" name="port_range_end" min="1" step="1" value={data.agent.portRangeEnd ?? ''} />
			</label>
			<label class="grow">
				Instances directory
				<input
					type="text"
					name="instances_root_dir"
					placeholder="/home/axon/instances"
					value={data.agent.instancesRootDir ?? ''}
				/>
			</label>
			<button type="submit">Save</button>
		</form>
		{#if form?.error}
			<p class="error">{form.error}</p>
		{/if}

		{#if isConfigured}
			<a class="ghost-link" href="/agents/{data.agent.id}/new-instance">Create Server →</a>
		{:else}
			<button type="button" class="ghost" disabled title="Configure a port range and instances directory first">
				Create Server →
			</button>
		{/if}
	</section>

	<section class="card">
		<div class="section-header">
			<h2>Instances</h2>
		</div>
		{#if data.instances.length === 0}
			<p class="empty">No server instances reported yet.</p>
		{:else}
			<ul class="instances">
				{#each data.instances as instance (instance.id)}
					<li>
						<div class="instance-main">
							<strong>{instance.name}</strong>
							<span class="badge {stateClass[instance.runningState] ?? 'badge-info'}">
								{stateLabel[instance.runningState] ?? instance.runningState}
							</span>
						</div>
						<span class="meta"
							>{instance.gamePlatform} · {instance.softwareType} {instance.version}{instance.port
								? ` · port ${instance.port}`
								: ''}</span
						>
						<a class="ghost-link" href="/instances/{instance.id}">Backups →</a>
					</li>
				{/each}
			</ul>
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

	.meta {
		font-size: 0.8rem;
		opacity: 0.7;
		margin: 0.15rem 0 0;
	}

	.update-note {
		color: var(--axon-status-info);
		opacity: 1;
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

	.settings-form {
		display: flex;
		align-items: flex-end;
		gap: 0.75rem;
		flex-wrap: wrap;
		margin: 1rem 0;
	}

	.settings-form label {
		display: flex;
		flex-direction: column;
		gap: 0.25rem;
		font-size: 0.8rem;
		opacity: 0.85;
	}

	.settings-form label.grow {
		flex: 1;
		min-width: 12rem;
	}

	.settings-form input {
		padding: 0.5rem 0.625rem;
		border-radius: 0.375rem;
		border: 1px solid var(--axon-accent);
		background: var(--axon-background);
		color: var(--axon-text);
		width: 8rem;
	}

	.settings-form label.grow input {
		width: 100%;
	}

	.instances {
		list-style: none;
		padding: 0;
		margin: 0;
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

	.instance-main {
		display: flex;
		align-items: center;
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

	.empty {
		opacity: 0.7;
	}

	.error {
		color: var(--axon-status-error);
		font-size: 0.8rem;
		margin: 0.5rem 0 0;
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
