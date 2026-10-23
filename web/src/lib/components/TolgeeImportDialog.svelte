<!-- @codeatlasdev/ui demo #085 · MIT License (c) CodeAtlas · https://ui.codeatlas.com.br/docs/feedback/dialog#form -->
<script lang="ts">
	import {
		Dialog, Button, TextField, Select, SegmentedControl, Switch, Stack, Row, Alert, Callout, Chip, Card, toast
	} from '@codeatlasdev/ui';
	import { PlugZap } from '@lucide/svelte';
	import { goto } from '$app/navigation';
	import { api, type TolgeeInfo } from '#lib/api.ts';
	import { session } from '#lib/session.svelte.ts';
	import { t, num } from '#lib/i18n.svelte.ts';

	let { open = $bindable(false) }: { open?: boolean } = $props();

	let url = $state('');
	let apiKey = $state('');
	let projectId = $state('');
	let info = $state<TolgeeInfo | null>(null);
	let target = $state('new');
	let name = $state('');
	let slug = $state('');
	let existing = $state('');
	let overwrite = $state(false);
	let error = $state('');
	let connecting = $state(false);
	let starting = $state(false);

	const source = $derived({ url, api_key: apiKey, project_id: Number(projectId) || 0 });
	const projectOptions = $derived(session.projects.map((p) => ({ value: p.slug, label: p.name })));
	const isPAT = $derived(apiKey.trim().startsWith('tgpat_'));

	function slugify(s: string) {
		return s
			.toLowerCase()
			.normalize('NFD')
			.replace(/[̀-ͯ]/g, '')
			.replace(/[^a-z0-9]+/g, '-')
			.replace(/^-+|-+$/g, '')
			.slice(0, 63);
	}

	async function connect() {
		connecting = true;
		error = '';
		info = null;
		try {
			info = (await api.post<{ tolgee: TolgeeInfo }>('/tolgee/inspect', source)).tolgee;
			name = info.project_name;
			slug = slugify(info.project_name);
		} catch (e) {
			error = (e as Error).message;
		} finally {
			connecting = false;
		}
	}

	async function start() {
		starting = true;
		error = '';
		try {
			const res = await api.post<{ job_id: number; project_slug: string }>('/tolgee/import', {
				...source,
				target,
				name,
				slug,
				project_slug: existing,
				overwrite
			});
			await session.loadProjects();
			toast.success(t('tolgee.started'));
			open = false;
			info = null;
			apiKey = '';
			goto('/jobs');
		} catch (e) {
			error = (e as Error).message;
		} finally {
			starting = false;
		}
	}
</script>

<Dialog bind:open title={t('tolgee.title')} description={t('tolgee.description')} size="md">
	<Stack gap="4">
		{#if error}<Alert severity="error">{error}</Alert>{/if}
		<TextField label={t('tolgee.url')} bind:value={url} placeholder="https://tolgee.example.com" helperText={t('tolgee.urlHint')} required />
		<TextField
			label={t('tolgee.apiKey')}
			type="password"
			bind:value={apiKey}
			autocomplete="off"
			placeholder="tgpak_…"
			helperText={t('tolgee.apiKeyHint')}
			required
		/>
		<TextField
			label={t('tolgee.projectId')}
			bind:value={projectId}
			inputmode="numeric"
			helperText={isPAT ? t('tolgee.projectIdRequired') : t('tolgee.projectIdHint')}
			required={isPAT}
		/>
		<Row gap={8}>
			<Button variant="outlined" loading={connecting} disabled={!url || !apiKey} onclick={connect}>
				{#snippet leading()}<PlugZap size={14} />{/snippet}
				{t('tolgee.connect')}
			</Button>
		</Row>

		{#if info}
			<Card title={info.project_name} padding="sm">
				<Stack gap="2">
					<span class="lx-muted">{t('tolgee.found', { keys: num(info.keys), langs: info.languages.length })}</span>
					<Row gap={6} wrap>
						{#each info.languages as l (l.tag)}
							<Chip size="sm" variant={l.base ? 'brand' : 'default'} label={l.tag} />
						{/each}
					</Row>
				</Stack>
			</Card>
			<SegmentedControl
				aria-label={t('tolgee.target')}
				bind:value={target}
				fullWidth
				items={[
					{ value: 'new', label: t('tolgee.targetNew') },
					{ value: 'existing', label: t('tolgee.targetExisting') }
				]}
			/>
			{#if target === 'new'}
				<TextField label={t('projects.field.name')} bind:value={name} />
				<TextField label={t('projects.field.slug')} bind:value={slug} helperText={t('projects.field.slugHint')} />
			{:else}
				<Select label={t('tolgee.existingProject')} bind:value={existing} options={projectOptions} />
				<Switch label={t('tolgee.overwrite')} bind:checked={overwrite} />
			{/if}
			<Callout type="note">{t('tolgee.background', { keys: num(info.keys) })}</Callout>
		{/if}
	</Stack>
	{#snippet actions()}
		<Button variant="ghost" onclick={() => (open = false)}>{t('common.cancel')}</Button>
		<Button
			variant="filled"
			loading={starting}
			disabled={!info || (target === 'new' ? !name || !slug : !existing)}
			onclick={start}
		>
			{t('tolgee.start')}
		</Button>
	{/snippet}
</Dialog>
