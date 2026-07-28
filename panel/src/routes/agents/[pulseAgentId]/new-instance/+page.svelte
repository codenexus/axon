<script lang="ts">
	import { enhance } from '$app/forms';
	import { goto, invalidateAll } from '$app/navigation';
	import type { ActionResult } from '@sveltejs/kit';
	import type { ActionData, PageData } from './$types';

	let { data, form }: { data: PageData; form: ActionData } = $props();

	let selectedEdition = $state<'java' | 'bedrock'>('java');
	let selectedSoftwareType = $state<'vanilla' | 'paper' | 'fabric' | 'forge'>('vanilla');
	let selectedCatalogId = $state('');
	let bedrockUrlOverride = $state('');

	const javaVersionsBySoftware = $derived.by(() => {
		switch (selectedSoftwareType) {
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

	// Switching software type invalidates any catalog id picked from the
	// previous list — reset to whatever the newly-selected list's first
	// entry is (or empty, if it's not loaded yet).
	$effect(() => {
		selectedSoftwareType;
		selectedCatalogId = javaVersionsBySoftware[0]?.id ?? '';
	});

	let selectedDefinitionId = $state('');
	const usingDefinition = $derived(selectedDefinitionId !== '');
	const selectedDefinition = $derived(data.definitions.find((d) => d.id === selectedDefinitionId));

	// A definition pins *what* to install (see selectedDefinition above),
	// never *how it's configured* — heap size and property overrides below
	// are always chosen fresh at creation time regardless of whether a
	// definition supplied the software/version, so this derives which
	// edition's options (e.g. gamemode) to offer either way.
	const effectiveEdition = $derived(usingDefinition ? selectedDefinition?.gamePlatform : selectedEdition);

	// Pre-filled with the same defaults vanilla server software would pick
	// on its own first launch (see pulse/internal/provision) -- showing
	// them upfront rather than leaving the admin to discover and edit them
	// after the fact via the properties editor.
	let javaHeapMb = $state(2048);
	let gamemode = $state('survival');
	let difficulty = $state('easy');
	let maxPlayers = $state(20);
	let motd = $state('A Minecraft Server');

	// Bedrock has no "spectator" server.properties value -- reset off it if
	// the edition changes out from under an already-chosen gamemode.
	$effect(() => {
		if (effectiveEdition === 'bedrock' && gamemode === 'spectator') gamemode = 'survival';
	});

	// Pre-fill the Bedrock URL from the resolved catalog entry the first
	// time it arrives, without clobbering an admin's own edit afterward.
	let bedrockPrefilled = $state(false);
	$effect(() => {
		if (bedrockPrefilled || data.bedrockVersions.length === 0) return;
		bedrockUrlOverride = data.bedrockVersions[0].downloadUrl;
		selectedCatalogId = data.bedrockVersions[0].id;
		bedrockPrefilled = true;
	});

	const phaseLabel: Record<string, string> = {
		preparing: 'Preparing…',
		installing_java: 'Installing Java…',
		downloading: 'Downloading server software…',
		installing_loader: 'Running installer…',
		configuring: 'Configuring…',
		registering: 'Registering with Pulse…'
	};

	const isTerminal = $derived(
		data.pendingCommand?.status === 'completed' || data.pendingCommand?.status === 'failed'
	);

	$effect(() => {
		if (!data.pendingCommand || isTerminal) return;
		const interval = setInterval(() => invalidateAll(), 3000);
		return () => clearInterval(interval);
	});

	function handleCreateSubmit() {
		return async ({ result }: { result: ActionResult }) => {
			if (result.type === 'success' && result.data?.commandId) {
				await goto(`?commandId=${result.data.commandId}`, { invalidateAll: true });
			}
		};
	}
</script>

<svelte:head>
	<title>Axon Panel — Create Server</title>
</svelte:head>

<div class="page">
	<header>
		<p class="breadcrumb"><a href="/agents/{data.agent.id}">← {data.agent.hostname}</a></p>
		<h1>Create Server</h1>
	</header>

	<section class="card">
		{#if !data.configured}
			<p class="empty">Configure a port range and instances directory for this agent first.</p>
			<a class="ghost-link" href="/agents/{data.agent.id}">← Back to agent settings</a>
		{:else if data.pendingCommand}
			{#if data.pendingCommand.status === 'completed'}
				<p>Server created successfully.</p>
				<a class="ghost-link" href="/instances/{data.agent.id}:{data.pendingCommand.instanceId}">View server →</a>
			{:else if data.pendingCommand.status === 'failed'}
				<p class="error">{data.pendingCommand.resultMessage ?? 'Server creation failed.'}</p>
				<a class="ghost-link" href="/agents/{data.agent.id}/new-instance">← Try again</a>
			{:else}
				<span class="badge badge-warning badge-pulsing">
					{phaseLabel[data.pendingCommand.progressPhase ?? 'preparing'] ?? 'Working…'}
				</span>
				<p class="meta">
					This can take a few minutes — installing Java (if needed) and downloading server software.
				</p>
			{/if}
		{:else}
			<form method="POST" action="?/create" use:enhance={handleCreateSubmit} class="create-form">
				<label>
					Name
					<input type="text" name="name" required />
				</label>

				{#if data.definitions.length > 0}
					<label>
						Use a saved definition
						<select bind:value={selectedDefinitionId}>
							<option value="">— configure manually —</option>
							{#each data.definitions as def (def.id)}
								<option value={def.id}>{def.name}</option>
							{/each}
						</select>
					</label>
				{/if}

				{#if usingDefinition}
					<input type="hidden" name="definition_id" value={selectedDefinitionId} />
					<p class="meta">
						Will create: {selectedDefinition?.gamePlatform} · {selectedDefinition?.softwareType} ·
						{selectedDefinition?.version}
					</p>
				{:else}
					<label>
						Edition
						<select name="game_platform" bind:value={selectedEdition}>
							<option value="java">Java</option>
							<option value="bedrock">Bedrock</option>
						</select>
					</label>

					{#if selectedEdition === 'java'}
						<label>
							Software
							<select name="software_type" bind:value={selectedSoftwareType}>
								<option value="vanilla">Vanilla</option>
								<option value="paper">Paper</option>
								<option value="fabric">Fabric</option>
								<option value="forge">Forge</option>
							</select>
						</label>
						<label>
							Version
							<select name="catalog_id" bind:value={selectedCatalogId}>
								{#each javaVersionsBySoftware as opt (opt.id)}
									<option value={opt.id}>{opt.version} (Java {opt.javaMajorVersion})</option>
								{/each}
							</select>
						</label>
						{#if javaVersionsBySoftware.length === 0}
							<p class="error">No versions available right now for this software — try again shortly.</p>
						{/if}
					{:else}
						<label>
							Version
							<select name="catalog_id" bind:value={selectedCatalogId}>
								{#each data.bedrockVersions as opt (opt.id)}
									<option value={opt.id}>{opt.version}</option>
								{/each}
								{#if data.bedrockVersions.length === 0}
									<option value="">(unknown — enter URL manually below)</option>
								{/if}
							</select>
						</label>
						<label class="grow">
							Download URL
							<input
								type="text"
								name="download_url"
								bind:value={bedrockUrlOverride}
								placeholder="https://www.minecraft.net/.../bedrock-server-....zip"
							/>
						</label>
						<p class="meta">
							Automatically looked up from minecraft.net — that lookup is best-effort, verify this is the URL you
							want before creating.
						</p>
					{/if}
				{/if}

				<h2 class="section-label">Server settings</h2>
				<p class="meta">
					Pre-filled with the same defaults the server software would pick on its own first launch — change
					anything here, or leave it and edit server.properties later.
				</p>

				{#if effectiveEdition === 'java'}
					<label>
						Java heap size (MB)
						<input type="number" name="java_heap_mb" min="512" step="256" bind:value={javaHeapMb} />
					</label>
				{/if}
				<label>
					Gamemode
					<select name="gamemode" bind:value={gamemode}>
						<option value="survival">Survival</option>
						<option value="creative">Creative</option>
						<option value="adventure">Adventure</option>
						{#if effectiveEdition !== 'bedrock'}
							<option value="spectator">Spectator</option>
						{/if}
					</select>
				</label>
				<label>
					Difficulty
					<select name="difficulty" bind:value={difficulty}>
						<option value="peaceful">Peaceful</option>
						<option value="easy">Easy</option>
						<option value="normal">Normal</option>
						<option value="hard">Hard</option>
					</select>
				</label>
				<label>
					Max players
					<input type="number" name="max_players" min="1" step="1" bind:value={maxPlayers} />
				</label>
				<label class="grow">
					Server name / MOTD
					<input type="text" name="motd" bind:value={motd} />
				</label>

				{#if form?.error}
					<p class="error">{form.error}</p>
				{/if}

				<button type="submit">Create</button>
			</form>
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
		margin: 0.4rem 0 0;
	}

	.card {
		background: var(--axon-surface);
		border: 1px solid var(--axon-accent);
		border-radius: 0.75rem;
		padding: 1.25rem;
		margin-bottom: 1.25rem;
	}

	.create-form {
		display: flex;
		flex-direction: column;
		gap: 1rem;
		max-width: 28rem;
	}

	.create-form label {
		display: flex;
		flex-direction: column;
		gap: 0.25rem;
		font-size: 0.8rem;
		opacity: 0.85;
	}

	.section-label {
		font-size: 0.9rem;
		margin: 0.5rem 0 0;
		padding-top: 1rem;
		border-top: 1px solid var(--axon-accent);
	}

	.create-form input,
	.create-form select {
		padding: 0.5rem 0.625rem;
		border-radius: 0.375rem;
		border: 1px solid var(--axon-accent);
		background: var(--axon-background);
		color: var(--axon-text);
	}

	.badge {
		display: inline-block;
		padding: 0.1rem 0.5rem;
		border-radius: 999px;
		font-size: 0.75rem;
		font-weight: 600;
		color: white;
		background: var(--axon-status-warning);
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
		align-self: flex-start;
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
		margin-top: 0.75rem;
	}
</style>
