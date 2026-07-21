<script lang="ts">
	import { enhance } from '$app/forms';
	import type { ActionData, PageData } from './$types';

	let { data, form }: { data: PageData; form: ActionData } = $props();
</script>

<svelte:head>
	<title>Axon Panel — {data.firstRun ? 'Set up admin' : 'Log in'}</title>
</svelte:head>

<div class="login-page">
	<form class="login-card" method="POST" use:enhance>
		<h1>Axon Panel</h1>
		<p class="subtitle">
			{data.firstRun ? 'Set an admin password to finish setup.' : 'Log in to continue.'}
		</p>

		<label for="password">Password</label>
		<input id="password" name="password" type="password" autocomplete={data.firstRun ? 'new-password' : 'current-password'} required minlength="8" />

		{#if form?.error}
			<p class="error">{form.error}</p>
		{/if}

		<button type="submit">{data.firstRun ? 'Create admin account' : 'Log in'}</button>
	</form>
</div>

<style>
	.login-page {
		min-height: 100vh;
		display: flex;
		align-items: center;
		justify-content: center;
		background: var(--axon-background);
		color: var(--axon-text);
	}

	.login-card {
		width: min(360px, 90vw);
		display: flex;
		flex-direction: column;
		gap: 0.75rem;
		padding: 2rem;
		border-radius: 0.75rem;
		background: var(--axon-surface);
		border: 1px solid var(--axon-accent);
	}

	h1 {
		margin: 0;
	}

	.subtitle {
		margin: 0 0 0.5rem;
		opacity: 0.8;
	}

	label {
		font-size: 0.875rem;
	}

	input {
		padding: 0.5rem 0.625rem;
		border-radius: 0.375rem;
		border: 1px solid var(--axon-accent);
		background: var(--axon-background);
		color: var(--axon-text);
	}

	button {
		margin-top: 0.5rem;
		padding: 0.625rem;
		border: none;
		border-radius: 0.375rem;
		background: var(--axon-primary);
		color: var(--axon-background);
		font-weight: 600;
		cursor: pointer;
	}

	.error {
		color: var(--axon-status-error);
		margin: 0;
		font-size: 0.875rem;
	}
</style>
