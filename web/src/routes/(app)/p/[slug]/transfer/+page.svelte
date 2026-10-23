<script lang="ts">
	import {
		Card, Grid, Stack, Row, Select, Combobox, Switch, SegmentedControl, Button, FileUpload, TextField,
		Alert, Callout, Chip, toast
	} from '@codeatlasdev/ui';
	import { Download, Upload } from '@lucide/svelte';
	import { api } from '#lib/api.ts';
	import { useProject } from '#lib/project.svelte.ts';
	import { session, langName } from '#lib/session.svelte.ts';
	import { t, tOr, num } from '#lib/i18n.svelte.ts';

	const project = useProject();
	const d = $derived(project.detail!);
	const langOptions = $derived(d.languages.map((l) => ({ value: l.code, label: `${langName(l.code)} (${l.code})` })));
	const formatOptions = $derived((session.meta?.formats ?? []).map((f) => ({ value: f.id, label: tOr('format.' + f.id, f.label) })));

	// export
	let format = $state('json');
	let langs = $state<string[]>([]);
	let minState = $state('');
	let fallback = $state(false);

	const exportUrl = $derived.by(() => {
		const p = new URLSearchParams({ format });
		if (langs.length) p.set('lang', langs.join(','));
		if (minState) p.set('state', minState);
		if (fallback) p.set('fallback', 'base');
		return '/api' + project.api('/export?' + p);
	});
	const isZip = $derived(format !== 'csv' && langs.length !== 1);

	// import
	let files = $state<File[]>([]);
	let importLang = $state('');
	let namespace = $state('');
	let importFormat = $state('auto');
	let importState = $state('translated');
	let overwrite = $state(false);
	let addLanguages = $state(true);
	let busy = $state(false);
	let result = $state<{
		files: number;
		keys_created: number;
		translations_added: number;
		translations_updated: number;
		skipped: number;
		languages_added: string[];
		warnings: string[];
	} | null>(null);
	let error = $state('');

	const isArchive = $derived(files[0]?.name.toLowerCase().endsWith('.zip') ?? false);

	async function runImport() {
		if (!files[0]) return;
		busy = true;
		error = '';
		result = null;
		const fd = new FormData();
		fd.set('file', files[0]);
		if (importLang) fd.set('lang', importLang);
		if (namespace) fd.set('namespace', namespace);
		fd.set('format', importFormat);
		fd.set('state', importState);
		fd.set('overwrite', String(overwrite));
		fd.set('add_languages', String(addLanguages));
		try {
			result = await api.post(project.api('/import'), fd);
			toast.success(t('transfer.imported'));
			files = [];
			await Promise.all([project.load(), session.loadProjects()]);
		} catch (e) {
			error = (e as Error).message;
		} finally {
			busy = false;
		}
	}
</script>

<Grid columns={{ base: 1, lg: 2 }} gap="4">
	<Card title={t('transfer.export')} headingLevel={2}>
		<Stack gap="4">
			<p class="lx-muted p">{t('transfer.exportIntro')}</p>
			<Select label={t('transfer.format')} bind:value={format} options={formatOptions} />
			<Combobox
				label={t('transfer.languages')}
				multiple
				bind:value={langs}
				options={langOptions}
				placeholder={t('transfer.allLanguages')}
			/>
			<SegmentedControl
				aria-label={t('transfer.include')}
				bind:value={minState}
				items={[
					{ value: '', label: t('transfer.includeAll') },
					{ value: 'translated', label: t('transfer.includeTranslated') },
					{ value: 'reviewed', label: t('transfer.includeReviewed') }
				]}
				fullWidth
				size="sm"
			/>
			<Switch label={t('transfer.fallback', { lang: langName(project.base) })} bind:checked={fallback} />
			<Row gap={8} align="center" wrap>
				<Button variant="filled" href={exportUrl} download>
					{#snippet leading()}<Download size={14} />{/snippet}
					{t('transfer.download')}
				</Button>
				<Chip size="sm" label={isZip ? 'ZIP' : format.toUpperCase()} />
			</Row>
		</Stack>
	</Card>

	{#if d.permissions.manage_keys}
		<Card title={t('transfer.import')} headingLevel={2}>
			<Stack gap="4">
				<p class="lx-muted p">{t('transfer.importIntro')}</p>
				{#if error}<Alert severity="error">{error}</Alert>{/if}
				{#if result}
					<Alert severity={result.warnings.length ? 'warning' : 'success'} title={t('transfer.result')} closable onclose={() => (result = null)}>
						{t('transfer.resultBody', {
							files: result.files,
							keys: num(result.keys_created),
							added: num(result.translations_added),
							updated: num(result.translations_updated),
							skipped: num(result.skipped)
						})}
						{#if result.languages_added.length}<br />{t('transfer.languagesAdded', { list: result.languages_added.join(', ') })}{/if}
						{#each result.warnings as w (w)}<br />{w}{/each}
					</Alert>
				{/if}
				<FileUpload
					bind:files
					accept=".json,.csv,.po,.pot,.xml,.strings,.properties,.zip"
					label={t('transfer.file')}
					hint={t('transfer.fileHint')}
				/>
				<Grid columns={{ base: 1, sm: 2 }} gap="3">
					<Select
						label={t('transfer.language')}
						bind:value={importLang}
						options={[{ value: '', label: t('transfer.fromFileName') }, ...langOptions]}
						disabled={isArchive}
					/>
					<Select
						label={t('transfer.format')}
						bind:value={importFormat}
						options={[{ value: 'auto', label: t('transfer.detect') }, ...formatOptions.filter((f) => f.value !== 'json-nested')]}
					/>
					<TextField label={t('keys.field.namespace')} bind:value={namespace} placeholder={isArchive ? t('transfer.fromFolder') : ''} />
					<Select
						label={t('transfer.state')}
						bind:value={importState}
						options={[
							{ value: 'translated', label: t('state.translated') },
							{ value: 'draft', label: t('state.draft') },
							...(d.permissions.review ? [{ value: 'reviewed', label: t('state.reviewed') }] : [])
						]}
					/>
				</Grid>
				<Switch label={t('transfer.overwrite')} bind:checked={overwrite} />
				<Switch label={t('transfer.addLanguages')} bind:checked={addLanguages} />
				<Row gap={8}>
					<Button variant="filled" loading={busy} disabled={!files.length} onclick={runImport}>
						{#snippet leading()}<Upload size={14} />{/snippet}
						{t('transfer.importButton')}
					</Button>
				</Row>
				<Callout type="tip" title={t('transfer.tolgeeTitle')}>{t('transfer.tolgeeBody')}</Callout>
			</Stack>
		</Card>
	{/if}
</Grid>

<style>
	.p {
		margin: 0;
	}
</style>
