<!-- @codeatlasdev/ui demo #085 · MIT License (c) CodeAtlas · https://ui.codeatlas.com.br/docs/feedback/dialog#form -->
<script lang="ts">
	import { Dialog, Button, Select, Stack, Switch, Alert, Callout, Progress, toast } from '@codeatlasdev/ui';
	import { api, type Provider } from '#lib/api.ts';
	import { useProject } from '#lib/project.svelte.ts';
	import { langName } from '#lib/session.svelte.ts';
	import { t, num } from '#lib/i18n.svelte.ts';

	let {
		open = $bindable(false),
		providers,
		defaultLang = '',
		keyIds = [],
		ondone
	}: { open?: boolean; providers: Provider[]; defaultLang?: string; keyIds?: number[]; ondone: () => void } = $props();

	const project = useProject();
	const targets = $derived((project.detail?.editable_languages ?? []).filter((l) => l !== project.base));

	let lang = $state('');
	let providerId = $state('');
	let overwrite = $state(false);
	let asDraft = $state(true);
	let busy = $state(false);
	let error = $state('');

	$effect.pre(() => {
		if (open) {
			if (!lang || !targets.includes(lang)) lang = targets.includes(defaultLang) ? defaultLang : (targets[0] ?? '');
			if (!providerId && providers.length) providerId = String(providers.find((p) => p.ai)?.id ?? providers[0].id);
		}
	});

	const missing = $derived.by(() => {
		const s = project.detail?.languages.find((l) => l.code === lang);
		return s ? Math.max(0, (project.detail?.keys ?? 0) - s.translated - s.draft) : 0;
	});

	async function run() {
		busy = true;
		error = '';
		try {
			const res = await api.post<{ translated: number; failed: number; error?: string }>(project.api('/mt/batch'), {
				lang,
				provider_id: Number(providerId),
				overwrite,
				state: asDraft ? 'draft' : 'translated',
				key_ids: keyIds
			});
			if (res.error) error = res.error;
			if (res.translated) toast.success(t('batch.done', { n: num(res.translated), lang: langName(lang) }));
			else if (!res.error) toast.info(t('batch.nothing'));
			ondone();
			if (!res.error) open = false;
		} catch (e) {
			error = (e as Error).message;
		} finally {
			busy = false;
		}
	}
</script>

<Dialog bind:open title={t('batch.title')} description={t('batch.description')} size="md">
	<Stack gap="4">
		{#if error}<Alert severity="error">{error}</Alert>{/if}
		{#if !providers.length}
			<Callout type="note">{t('batch.noProviders')}</Callout>
		{:else}
			<Select label={t('batch.provider')} bind:value={providerId} options={providers.map((p) => ({ value: String(p.id), label: p.name }))} />
			<Select
				label={t('batch.language')}
				bind:value={lang}
				options={targets.map((c) => ({ value: c, label: `${langName(c)} (${c})` }))}
			/>
			{#if keyIds.length}
				<p class="lx-muted">{t('batch.selected', { n: keyIds.length })}</p>
			{:else if !overwrite}
				<p class="lx-muted">{t('batch.missing', { n: num(missing), lang: langName(lang) })}</p>
			{/if}
			<Switch label={t('batch.asDraft')} bind:checked={asDraft} />
			{#if project.can.review}
				<Switch label={t('batch.overwrite')} bind:checked={overwrite} />
			{/if}
			{#if busy}<Progress indeterminate label={t('batch.running')} />{/if}
		{/if}
	</Stack>
	{#snippet actions()}
		<Button variant="ghost" onclick={() => (open = false)}>{t('common.cancel')}</Button>
		<Button variant="filled" loading={busy} disabled={!providers.length || !lang} onclick={run}>{t('batch.run')}</Button>
	{/snippet}
</Dialog>
