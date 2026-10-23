<script lang="ts">
	import { Drawer, TextField, TextArea, Combobox, Button, Stack, Row, Collapsible, toast } from '@codeatlasdev/ui';
	import { api, type Key, type Provider, type Translation } from '#lib/api.ts';
	import { useProject } from '#lib/project.svelte.ts';
	import { t } from '#lib/i18n.svelte.ts';
	import TranslationField from './TranslationField.svelte';

	let {
		open = $bindable(false),
		key = $bindable(null),
		focusLang = '',
		languages,
		providers,
		tags,
		onchange
	}: {
		open?: boolean;
		key: Key | null;
		focusLang?: string;
		languages: string[];
		providers: Provider[];
		tags: string[];
		onchange: () => void;
	} = $props();

	const project = useProject();

	// Source language first, then the rest in project order.
	const ordered = $derived([project.base, ...languages.filter((l) => l !== project.base)]);

	let name = $state('');
	let namespace = $state('');
	let description = $state('');
	let keyTags = $state<string[]>([]);
	let savingKey = $state(false);
	let lastId = 0;

	$effect.pre(() => {
		if (key && key.id !== lastId) {
			lastId = key.id;
			name = key.name;
			namespace = key.namespace;
			description = key.description;
			keyTags = [...key.tags];
		}
	});

	const keyDirty = $derived(
		!!key &&
			(name !== key.name ||
				namespace !== key.namespace ||
				description !== key.description ||
				keyTags.join('\n') !== key.tags.join('\n'))
	);

	const tagOptions = $derived([...new Set([...tags, ...keyTags])].map((x) => ({ value: x, label: x })));

	async function saveKey() {
		if (!key) return;
		savingKey = true;
		try {
			const res = await api.patch<{ key: Key }>(project.api('/keys/' + key.id), { name, namespace, description, tags: keyTags });
			key = res.key;
			lastId = 0;
			onchange();
			toast.success(t('editor.keySaved'));
		} catch (e) {
			toast.error((e as Error).message);
		} finally {
			savingKey = false;
		}
	}

	function saved(lang: string, tr: Translation | null) {
		if (!key) return;
		const translations = { ...key.translations };
		if (tr) translations[lang] = tr;
		else delete translations[lang];
		key = { ...key, translations };
		onchange();
	}
</script>

<Drawer bind:open title={key?.name ?? ''} width="min(640px, 100vw)">
	{#if key}
		<Stack gap="4">
			{#if project.can.manage_keys}
				<Collapsible title={t('editor.keyDetails')}>
					<Stack gap="3">
						<TextField label={t('keys.field.name')} bind:value={name} class="lx-mono" />
						<TextField label={t('keys.field.namespace')} bind:value={namespace} helperText={t('keys.field.namespaceHint')} />
						<TextArea label={t('keys.field.description')} bind:value={description} rows={2} helperText={t('keys.field.descriptionHint')} />
						<Combobox
							label={t('keys.field.tags')}
							multiple
							creatable
							bind:value={keyTags}
							options={tagOptions}
							oncreate={(v) => (keyTags = [...keyTags, v])}
						/>
						{#if keyDirty}
							<Row gap={8} justify="flex-end">
								<Button variant="filled" size="sm" loading={savingKey} onclick={saveKey}>{t('editor.saveKey')}</Button>
							</Row>
						{/if}
					</Stack>
				</Collapsible>
			{:else if key.description}
				<p class="desc">{key.description}</p>
			{/if}

			<div>
				{#each ordered as lang (lang)}
					<TranslationField {key} {lang} {providers} focus={lang === focusLang} onsaved={(tr) => saved(lang, tr)} />
				{/each}
			</div>
			<p class="hint">{t('editor.shortcut')}</p>
		</Stack>
	{/if}
</Drawer>

<style>
	.desc {
		margin: 0;
		color: var(--ca-text-secondary);
		white-space: pre-wrap;
	}
	.hint {
		margin: 0;
		font-size: var(--ca-font-size-caption, 0.8rem);
		color: var(--ca-text-secondary);
	}
</style>
