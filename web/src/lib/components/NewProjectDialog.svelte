<!-- @codeatlasdev/ui demo #085 · MIT License (c) CodeAtlas · https://ui.codeatlas.com.br/docs/feedback/dialog#form -->
<script lang="ts">
	import { Dialog, Button, TextField, TextArea, Combobox, Stack, Alert, toast } from '@codeatlasdev/ui';
	import { goto } from '$app/navigation';
	import { api, type Project } from '#lib/api.ts';
	import { session, languageOptions } from '#lib/session.svelte.ts';
	import { t } from '#lib/i18n.svelte.ts';

	let { open = $bindable(false) }: { open?: boolean } = $props();

	let name = $state('');
	let slug = $state('');
	let slugEdited = $state(false);
	let description = $state('');
	let base = $state('en');
	let targets = $state<string[]>([]);
	let error = $state('');
	let busy = $state(false);

	const options = $derived(languageOptions());
	const autoSlug = $derived(
		name
			.toLowerCase()
			.normalize('NFD')
			.replace(/[̀-ͯ]/g, '')
			.replace(/[^a-z0-9]+/g, '-')
			.replace(/^-+|-+$/g, '')
			.slice(0, 63)
	);

	$effect(() => {
		if (!slugEdited) slug = autoSlug;
	});

	async function create() {
		error = '';
		busy = true;
		try {
			const res = await api.post<{ project: Project }>('/projects', {
				name,
				slug,
				description,
				base_language: base,
				languages: targets.filter((c) => c !== base)
			});
			await session.loadProjects();
			toast.success(t('projects.created', { name }));
			open = false;
			name = description = '';
			targets = [];
			slugEdited = false;
			goto('/p/' + res.project.slug);
		} catch (e) {
			error = (e as Error).message;
		} finally {
			busy = false;
		}
	}
</script>

<Dialog bind:open title={t('projects.new')} description={t('projects.newDescription')} size="md">
	<Stack gap="4">
		{#if error}<Alert severity="error">{error}</Alert>{/if}
		<TextField label={t('projects.field.name')} bind:value={name} placeholder="Web app" required />
		<TextField
			label={t('projects.field.slug')}
			bind:value={slug}
			oninput={() => (slugEdited = true)}
			helperText={t('projects.field.slugHint')}
			required
		/>
		<TextArea label={t('projects.field.description')} bind:value={description} rows={2} helperText={t('projects.field.descriptionHint')} />
		<Combobox label={t('projects.field.base')} bind:value={base} {options} searchPlaceholder={t('common.searchLanguages')} />
		<Combobox
			label={t('projects.field.targets')}
			multiple
			bind:value={targets}
			options={options.filter((o) => o.value !== base)}
			placeholder={t('projects.field.targetsPlaceholder')}
			searchPlaceholder={t('common.searchLanguages')}
		/>
	</Stack>
	{#snippet actions()}
		<Button variant="ghost" onclick={() => (open = false)}>{t('common.cancel')}</Button>
		<Button variant="filled" loading={busy} disabled={!name || !slug} onclick={create}>{t('projects.create')}</Button>
	{/snippet}
</Dialog>
