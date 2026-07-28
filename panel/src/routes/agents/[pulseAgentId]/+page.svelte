<script lang="ts">
	import { enhance } from '$app/forms';
	import { invalidateAll } from '$app/navigation';
	import type { ActionResult } from '@sveltejs/kit';
	import type { ActionData, PageData } from './$types';
	import { formatDuration } from '$lib/format';

	let { data, form }: { data: PageData; form: ActionData } = $props();

	const diagnosticLabel: Record<string, string> = {
		uptime: 'Uptime',
		disk_usage: 'Disk usage',
		memory: 'Memory',
		processes: 'Processes'
	};

	// A diagnostic sent now only reaches Pulse on its next heartbeat, same
	// latency shape as the instance page's RCON console — drop to a 1s poll
	// while one's in flight, same fastPollNeeded pattern.
	const diagnosticInFlight = $derived(
		data.recentDiagnostics.some((c) => c.status === 'queued' || c.status === 'sent')
	);
	$effect(() => {
		const ms = diagnosticInFlight ? 1000 : 5000;
		const interval = setInterval(() => invalidateAll(), ms);
		return () => clearInterval(interval);
	});

	function diagnosticPayload(entry: (typeof data.recentDiagnostics)[number]): { name?: string; args?: string } {
		if (!entry.payload) return {};
		try {
			return JSON.parse(entry.payload) as { name?: string; args?: string };
		} catch {
			return {};
		}
	}

	let diagnosticForm: HTMLFormElement;
	function handleDiagnosticSubmit() {
		return async ({ result, update }: { result: ActionResult; update: () => Promise<void> }) => {
			await update();
			if (result.type === 'success') diagnosticForm?.reset();
		};
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

	const isConfigured = $derived(
		data.agent.portRangeStart != null && data.agent.portRangeEnd != null && !!data.agent.instancesRootDir
	);

	function formatBytes(bytes: number | null | undefined): string {
		if (!bytes) return '—';
		return `${(bytes / 1024 ** 3).toFixed(1)} GB`;
	}

	function formatUptime(seconds: number | null | undefined): string {
		if (!seconds) return '—';
		const days = Math.floor(seconds / 86400);
		const hours = Math.floor((seconds % 86400) / 3600);
		if (days === 0 && hours === 0) return '<1h';
		return days > 0 ? `${days}d ${hours}h` : `${hours}h`;
	}

	interface DiskUsage {
		mount: string;
		total_bytes: number;
		used_bytes: number;
	}

	const disks = $derived<DiskUsage[]>(
		(() => {
			if (!data.agent.diskUsageJson) return [];
			try {
				return JSON.parse(data.agent.diskUsageJson) as DiskUsage[];
			} catch {
				return [];
			}
		})()
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
			<h2>Host</h2>
		</div>
		<div class="host-stats">
			<div class="host-stat">
				<span class="host-stat-label">CPU</span>
				<span>{data.agent.cpuUsagePercent?.toFixed(0) ?? '—'}% ({data.agent.cpuCores ?? '—'} cores)</span>
			</div>
			<div class="host-stat">
				<span class="host-stat-label">Memory</span>
				<span>{formatBytes(data.agent.ramUsedBytes)} / {formatBytes(data.agent.ramTotalBytes)}</span>
			</div>
			<div class="host-stat">
				<span class="host-stat-label">Uptime</span>
				<span>{formatUptime(data.agent.hostUptimeSeconds)}</span>
			</div>
			{#each disks as disk (disk.mount)}
				<div class="host-stat">
					<span class="host-stat-label">Disk ({disk.mount})</span>
					<span>{formatBytes(disk.used_bytes)} / {formatBytes(disk.total_bytes)}</span>
				</div>
			{/each}
		</div>
	</section>

	<section class="card">
		<div class="section-header">
			<h2>Diagnostics</h2>
		</div>
		<p class="meta">
			Runs a fixed, read-only command on the Pulse host itself — not RCON, not a Minecraft server. Only the
			options below can ever run; nothing else is accepted.
		</p>

		<form
			method="POST"
			action="?/runDiagnostic"
			use:enhance={handleDiagnosticSubmit}
			bind:this={diagnosticForm}
			class="diagnostic-form"
		>
			<select name="name" required>
				{#each Object.entries(diagnosticLabel) as [value, label] (value)}
					<option {value}>{label}</option>
				{/each}
			</select>
			<input type="text" name="args" placeholder="extra arguments (optional)" autocomplete="off" />
			<button type="submit">Run</button>
		</form>
		{#if form?.error}
			<p class="error">{form.error}</p>
		{/if}

		{#if data.recentDiagnostics.length > 0}
			<ul class="console-log">
				{#each data.recentDiagnostics as entry (entry.id)}
					{@const p = diagnosticPayload(entry)}
					<li>
						<div class="console-entry-header">
							<code>&gt; {diagnosticLabel[p.name ?? ''] ?? p.name}{p.args ? ` ${p.args}` : ''}</code>
							{#if entry.status === 'queued' || entry.status === 'sent'}
								<span class="badge badge-warning badge-pulsing">Sent, waiting…</span>
							{:else if entry.status === 'failed'}
								<span class="badge badge-error">Failed</span>
							{/if}
						</div>
						{#if entry.status === 'completed'}
							<pre class="console-output">{entry.output || '(no output)'}</pre>
						{:else if entry.status === 'failed'}
							<p class="error">{entry.resultMessage}</p>
						{/if}
					</li>
				{/each}
			</ul>
		{/if}
	</section>

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
		{:else if form?.ok}
			<p class="success">Saved.</p>
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
						<div class="instance-row">
						<div class="instance-info">
						<div class="instance-main">
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
							<span class="badge {stateClass[instance.runningState] ?? 'badge-info'}">
								{stateLabel[instance.runningState] ?? instance.runningState}
							</span>
						</div>
						<span class="meta"
							>{instance.gamePlatform} · {instance.softwareType} {instance.version}{instance.port
								? ` · port ${instance.port}`
								: ''}</span
						>
						</div>
						<div class="instance-side">
							{#if instance.runningState === 'running'}
								<span class="instance-stats"
									>{instance.playerCount} player{instance.playerCount === 1 ? '' : 's'} · up {formatDuration(
										instance.uptimeSeconds
									)}</span
								>
							{/if}
							<a class="ghost-link" href="/instances/{instance.id}">Manage →</a>
						</div>
						</div>
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

	.host-stats {
		display: flex;
		flex-wrap: wrap;
		gap: 1.25rem;
	}

	.host-stat {
		display: flex;
		flex-direction: column;
		gap: 0.2rem;
		font-size: 0.85rem;
	}

	.host-stat-label {
		font-size: 0.75rem;
		opacity: 0.7;
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
		box-sizing: border-box;
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

	.instance-main {
		display: flex;
		align-items: center;
		gap: 0.5rem;
	}

	.server-icon {
		opacity: 0.6;
		flex-shrink: 0;
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

	.diagnostic-form {
		display: flex;
		gap: 0.5rem;
		margin-bottom: 0.75rem;
		flex-wrap: wrap;
	}

	.diagnostic-form select,
	.diagnostic-form input {
		padding: 0.5rem 0.625rem;
		border-radius: 0.375rem;
		border: 1px solid var(--axon-accent);
		background: var(--axon-background);
		color: var(--axon-text);
	}

	.diagnostic-form input {
		flex: 1;
		min-width: 10rem;
	}

	.console-log {
		list-style: none;
		padding: 0;
		margin: 0;
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
		max-height: 20rem;
		overflow-y: auto;
	}

	.console-log li {
		border-top: 1px solid var(--axon-accent);
		padding-top: 0.5rem;
	}

	.console-entry-header {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		flex-wrap: wrap;
	}

	.console-entry-header code {
		font-family: monospace;
		font-size: 0.85rem;
	}

	.console-output {
		font-family: monospace;
		font-size: 0.8rem;
		white-space: pre-wrap;
		word-break: break-word;
		background: var(--axon-background);
		border-radius: 0.375rem;
		padding: 0.5rem 0.625rem;
		margin: 0.4rem 0 0;
	}

	.empty {
		opacity: 0.7;
	}

	.success {
		color: var(--axon-status-success);
		font-size: 0.8rem;
		margin: 0.5rem 0 0;
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
