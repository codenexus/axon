<script lang="ts">
	const THEMES = [
		{ id: 'classic', label: 'Java/Bedrock (Classic)' },
		{ id: 'end', label: 'The End' },
		{ id: 'nether', label: 'The Nether' }
	] as const;

	let current = $state('classic');

	$effect(() => {
		const stored = localStorage.getItem('axon-theme');
		if (stored) current = stored;
	});

	function applyTheme(id: string) {
		current = id;
		document.documentElement.setAttribute('data-theme', id);
		localStorage.setItem('axon-theme', id);
	}
</script>

<label class="theme-switcher">
	<span>Theme</span>
	<select value={current} onchange={(e) => applyTheme(e.currentTarget.value)}>
		{#each THEMES as theme (theme.id)}
			<option value={theme.id}>{theme.label}</option>
		{/each}
	</select>
</label>

<style>
	.theme-switcher {
		display: inline-flex;
		align-items: center;
		gap: 0.5rem;
		font-size: 0.875rem;
	}

	select {
		padding: 0.25rem 0.5rem;
		border-radius: 0.375rem;
		border: 1px solid var(--axon-accent);
		background: var(--axon-surface);
		color: var(--axon-text);
	}
</style>
