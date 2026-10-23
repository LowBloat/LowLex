<!-- @codeatlasdev/ui demo #085 · MIT License (c) CodeAtlas · https://ui.codeatlas.com.br/docs/feedback/dialog#form -->
<script lang="ts">
	import { Dialog, Button, TextField, TextArea, Stack, Alert, toast } from '@codeatlasdev/ui';
	import { api, type Key } from '#lib/api.ts';
	import { useProject } from '#lib/project.svelte.ts';
	import { langName } from '#lib/session.svelte.ts';
	import { t } from '#lib/i18n.svelte.ts';

	let { open = $bindable(false), oncreated }: { open?: boolean; oncreated: (k: Key) => void } = $props();

	const project = useProject();
	let name = $state('');
	let namespace = $state('');
	let description = $state('');
	let source = $state('');
	let error = $state('');
	let busy = $state(false);
	let another = false;

	async function create() {
		error = '';
		busy = true;
		try {
			const res = await api.post<{ key: Key }>(project.api('/keys'), {
				name,
				namespace,
				description,
				translations: source ? { [project.base]: source } : {}
			});
			toast.success(t('keys.created', { name }));
			oncreated(res.key);
			name = description = source = '';
			if (!another) open = false;
		} catch (e) {
			error = (e as Error).message;
		} finally {
			busy = false;
			another = false;
		}
	}
</script>

<Dialog bind:open title={t('keys.new')} size="md">
	<Stack gap="4">
		{#if error}<Alert severity="error">{error}</Alert>{/if}
		<TextField label={t('keys.field.name')} bind:value={name} placeholder="home.title" helperText={t('keys.field.nameHint')} required />
		<TextField label={t('keys.field.namespace')} bind:value={namespace} helperText={t('keys.field.namespaceHint')} />
		<TextArea label={t('keys.field.source', { lang: langName(project.base) })} bind:value={source} rows={2} />
		<TextArea label={t('keys.field.description')} bind:value={description} rows={2} helperText={t('keys.field.descriptionHint')} />
	</Stack>
	{#snippet actions()}
		<Button variant="ghost" onclick={() => (open = false)}>{t('common.cancel')}</Button>
		<Button variant="outlined" disabled={!name || busy} onclick={() => { another = true; create(); }}>{t('keys.createAnother')}</Button>
		<Button variant="filled" loading={busy} disabled={!name} onclick={create}>{t('keys.create')}</Button>
	{/snippet}
</Dialog>
