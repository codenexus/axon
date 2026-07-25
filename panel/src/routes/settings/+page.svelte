<script lang="ts">
	import { enhance } from '$app/forms';
	import ThemeSwitcher from '$lib/theme/ThemeSwitcher.svelte';
	import type { ActionData } from './$types';

	let { form }: { form: ActionData } = $props();
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
