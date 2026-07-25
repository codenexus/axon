<script lang="ts">
	let {
		open,
		title,
		message,
		confirmLabel = 'Confirm',
		cancelLabel = 'Cancel',
		danger = false,
		onConfirm,
		onCancel
	}: {
		open: boolean;
		title: string;
		message: string;
		confirmLabel?: string;
		cancelLabel?: string;
		danger?: boolean;
		onConfirm: () => void;
		onCancel: () => void;
	} = $props();
</script>

{#if open}
	<div
		class="backdrop"
		role="presentation"
		onclick={onCancel}
		onkeydown={(e) => e.key === 'Escape' && onCancel()}
	>
		<div
			class="modal"
			role="alertdialog"
			aria-modal="true"
			aria-labelledby="confirm-modal-title"
			tabindex="-1"
			onclick={(e) => e.stopPropagation()}
			onkeydown={(e) => e.stopPropagation()}
		>
			<h2 id="confirm-modal-title">{title}</h2>
			<p>{message}</p>
			<div class="actions">
				<button type="button" class="ghost" onclick={onCancel}>{cancelLabel}</button>
				<button type="button" class:danger onclick={onConfirm}>{confirmLabel}</button>
			</div>
		</div>
	</div>
{/if}

<style>
	.backdrop {
		position: fixed;
		inset: 0;
		background: rgba(0, 0, 0, 0.5);
		display: flex;
		align-items: center;
		justify-content: center;
		z-index: 100;
		padding: 1rem;
	}

	.modal {
		background: var(--axon-surface);
		border: 1px solid var(--axon-accent);
		border-radius: 0.75rem;
		padding: 1.5rem;
		max-width: 420px;
		width: 100%;
		color: var(--axon-text);
	}

	.modal h2 {
		margin: 0 0 0.75rem;
	}

	.modal p {
		margin: 0;
		font-size: 0.9rem;
		opacity: 0.85;
		line-height: 1.5;
	}

	.actions {
		display: flex;
		justify-content: flex-end;
		gap: 0.5rem;
		margin-top: 1.5rem;
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
	}

	button.danger {
		background: var(--axon-status-error);
		color: white;
	}
</style>
