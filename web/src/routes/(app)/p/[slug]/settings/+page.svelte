<script lang="ts">
	import { FormSection, TextField, TextArea, Button, Row, Stack, Card, AlertDialog, Alert, toast } from '@codeatlasdev/ui';
	import { Trash2 } from '@lucide/svelte';
	import { goto } from '$app/navigation';
	import { api, type Project } from '#lib/api.ts';
	import { useProject } from '#lib/project.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { t } from '#lib/i18n.svelte.ts';

	const project = useProject();
	const p = $derived(project.project);

	let name = $state(project.project.name);
	let slug = $state(project.project.slug);
	let description = $state(project.project.description);
	let busy = $state(false);
	let error = $state('');
	let deleting = $state(false);
	let confirmText = $state('');

	const dirty = $derived(name !== p.name || slug !== p.slug || description !== p.description);

	async function save() {
		busy = true;
		error = '';
		try {
			const res = await api.patch<{ project: Project }>(project.api(), { name, slug, description });
			toast.success(t('settings.saved'));
			await session.loadProjects();
			if (res.project.slug !== project.slug) {
				goto('/p/' + res.project.slug + '/settings', { replace: true });
			} else {
				await project.load();
			}
		} catch (e) {
			error = (e as Error).message;
		} finally {
			busy = false;
		}
	}

	async function remove() {
		await api.del(project.api());
		await session.loadProjects();
		toast.success(t('settings.deleted', { name: p.name }));
		goto('/projects');
	}
</script>

<Stack gap="6">
	<Card>
		<Stack gap="4">
			{#if error}<Alert severity="error">{error}</Alert>{/if}
			<FormSection title={t('settings.general')} description={t('settings.generalHint')}>
				<TextField label={t('projects.field.name')} bind:value={name} />
				<TextField label={t('projects.field.slug')} bind:value={slug} helperText={t('settings.slugHint')} />
			</FormSection>
			<FormSection title={t('projects.field.description')} description={t('projects.field.descriptionHint')} layout="stack">
				<TextArea label={t('projects.field.description')} hideLabel bind:value={description} rows={3} />
			</FormSection>
			<Row gap={8} justify="flex-end">
				<Button variant="ghost" disabled={!dirty} onclick={() => { name = p.name; slug = p.slug; description = p.description; }}>{t('common.discard')}</Button>
				<Button variant="filled" loading={busy} disabled={!dirty || !name || !slug} onclick={save}>{t('common.save')}</Button>
			</Row>
		</Stack>
	</Card>

	{#if project.can.delete_project}
		<Card title={t('settings.danger')} headingLevel={2}>
			<Row gap={8} align="center" justify="space-between" wrap>
				<p class="lx-muted p">{t('settings.deleteHint')}</p>
				<Button variant="destructive" onclick={() => { deleting = true; confirmText = ''; }}>
					{#snippet leading()}<Trash2 size={14} />{/snippet}
					{t('settings.delete')}
				</Button>
			</Row>
		</Card>
	{/if}
</Stack>

<AlertDialog
	bind:open={deleting}
	title={t('settings.deleteTitle', { name: p.name })}
	description={t('settings.deleteBody', { slug: p.slug })}
	confirmLabel={t('settings.delete')}
	destructive
	confirmDisabled={confirmText !== p.slug}
	onconfirm={remove}
>
	<TextField label={t('settings.typeSlug', { slug: p.slug })} bind:value={confirmText} class="lx-mono" />
</AlertDialog>

<style>
	.p {
		margin: 0;
		max-width: 60ch;
	}
</style>
