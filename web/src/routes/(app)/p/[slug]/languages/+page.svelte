<!-- @codeatlasdev/ui demo #245 · MIT License (c) CodeAtlas · https://ui.codeatlas.com.br/docs/layout/record-list#apps -->
<!-- @codeatlasdev/ui demo #085 · MIT License (c) CodeAtlas · https://ui.codeatlas.com.br/docs/feedback/dialog#form -->
<script lang="ts">
	import {
		RecordList, RecordRow, RecordCell, Progress, Button, Row, Stack, Chip, Dialog, Combobox, TextField,
		DropdownMenu, DropdownMenuItem, IconButton, AlertDialog, Alert, toast
	} from '@codeatlasdev/ui';
	import { Plus, MoreHorizontal, Trash2, Star } from '@lucide/svelte';
	import { api } from '#lib/api.ts';
	import { useProject } from '#lib/project.svelte.ts';
	import { languageOptions, langName, session } from '#lib/session.svelte.ts';
	import { t, num } from '#lib/i18n.svelte.ts';

	const project = useProject();
	const d = $derived(project.detail!);

	let adding = $state(false);
	let code = $state('');
	let name = $state('');
	let error = $state('');
	let busy = $state(false);
	let removing = $state<string | null>(null);

	const options = $derived(languageOptions(d.languages.map((l) => l.code)));

	$effect.pre(() => {
		if (code) name = langName(code) === code ? name : langName(code);
	});

	function pct(n: number) {
		return d.keys ? Math.round((n / d.keys) * 100) : 0;
	}

	async function add() {
		busy = true;
		error = '';
		try {
			await api.post(project.api('/languages'), { code, name });
			toast.success(t('languages.added', { lang: name || code }));
			adding = false;
			code = name = '';
			await Promise.all([project.load(), session.loadProjects()]);
		} catch (e) {
			error = (e as Error).message;
		} finally {
			busy = false;
		}
	}

	async function remove(c: string) {
		await api.del(project.api('/languages/' + encodeURIComponent(c)));
		toast.success(t('languages.removed', { lang: langName(c) }));
		removing = null;
		await Promise.all([project.load(), session.loadProjects()]);
	}

	async function makeBase(c: string) {
		try {
			await api.patch(project.api(), { base_language: c });
			await project.load();
			toast.success(t('languages.baseChanged', { lang: langName(c) }));
		} catch (e) {
			toast.error((e as Error).message);
		}
	}
</script>

<Stack gap="4">
	<Row gap={8} align="center" justify="space-between" wrap>
		<p class="lx-muted intro">{t('languages.intro', { n: d.languages.length, keys: num(d.keys) })}</p>
		{#if d.permissions.manage_keys}
			<Button variant="filled" size="sm" onclick={() => (adding = true)}>
				{#snippet leading()}<Plus size={14} />{/snippet}
				{t('languages.add')}
			</Button>
		{/if}
	</Row>

	<RecordList
		columns="minmax(0,1.2fr) minmax(0,1.6fr) 6rem 6rem 6rem 2.75rem"
		labels={[t('languages.language'), t('languages.progress'), t('state.translated'), t('state.reviewed'), t('state.draft')]}
		aria-label={t('project.tab.languages')}
	>
		{#each d.languages as l (l.code)}
			<RecordRow>
				{#snippet identity()}
					<span class="who">
						<strong>{langName(l.code)}</strong>
						<span class="lx-mono lx-muted">{l.code}</span>
						{#if l.code === project.base}<Chip size="sm" variant="brand" label={t('languages.base')} />{/if}
					</span>
				{/snippet}
				<RecordCell label={t('languages.progress')}>
					<Progress value={pct(l.translated)} label="{pct(l.translated)}%" />
				</RecordCell>
				<RecordCell label={t('state.translated')}>{num(l.translated)}</RecordCell>
				<RecordCell label={t('state.reviewed')}>{num(l.reviewed)}</RecordCell>
				<RecordCell label={t('state.draft')}>{num(l.draft)}</RecordCell>
				{#snippet actions()}
					{#if l.code !== project.base && (d.permissions.manage_keys || d.permissions.manage_project)}
						<DropdownMenu align="end">
							{#snippet triggerChild(props)}
								<IconButton {...props} variant="text" aria-label={t('languages.actionsFor', { lang: langName(l.code) })}>
									<MoreHorizontal size={18} />
								</IconButton>
							{/snippet}
							{#if d.permissions.manage_project}
								<DropdownMenuItem onclick={() => makeBase(l.code)}>
									{#snippet icon()}<Star size={16} />{/snippet}
									{t('languages.makeBase')}
								</DropdownMenuItem>
							{/if}
							{#if d.permissions.manage_keys}
								<DropdownMenuItem variant="destructive" onclick={() => (removing = l.code)}>
									{#snippet icon()}<Trash2 size={16} />{/snippet}
									{t('languages.remove')}
								</DropdownMenuItem>
							{/if}
						</DropdownMenu>
					{/if}
				{/snippet}
			</RecordRow>
		{/each}
	</RecordList>
</Stack>

<Dialog bind:open={adding} title={t('languages.add')} size="sm">
	<Stack gap="4">
		{#if error}<Alert severity="error">{error}</Alert>{/if}
		<Combobox
			label={t('languages.language')}
			bind:value={code}
			{options}
			creatable
			oncreate={(v) => (code = v)}
			searchPlaceholder={t('common.searchLanguages')}
			description={t('languages.codeHint')}
		/>
		<TextField label={t('languages.displayName')} bind:value={name} />
	</Stack>
	{#snippet actions()}
		<Button variant="ghost" onclick={() => (adding = false)}>{t('common.cancel')}</Button>
		<Button variant="filled" loading={busy} disabled={!code} onclick={add}>{t('languages.add')}</Button>
	{/snippet}
</Dialog>

<AlertDialog
	open={removing !== null}
	title={t('languages.removeTitle', { lang: removing ? langName(removing) : '' })}
	description={t('languages.removeBody')}
	confirmLabel={t('languages.remove')}
	destructive
	onconfirm={() => remove(removing!)}
	oncancel={() => (removing = null)}
/>

<style>
	.intro {
		margin: 0;
	}
	.who {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: var(--ca-space-2);
		min-width: 0;
	}
</style>
