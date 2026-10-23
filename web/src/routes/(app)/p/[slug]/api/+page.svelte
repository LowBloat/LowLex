<!-- @codeatlasdev/ui demo #245 · MIT License (c) CodeAtlas · https://ui.codeatlas.com.br/docs/layout/record-list#apps -->
<!-- @codeatlasdev/ui demo #085 · MIT License (c) CodeAtlas · https://ui.codeatlas.com.br/docs/feedback/dialog#form -->
<script lang="ts">
	import {
		RecordList, RecordRow, RecordCell, Button, Row, Stack, Chip, Dialog, TextField, SegmentedControl,
		IconButton, AlertDialog, Alert, Card, CodeTabs, CodeTab, CopyButton, EmptyState, toast
	} from '@codeatlasdev/ui';
	import { Plus, Trash2, KeyRound } from '@lucide/svelte';
	import { onMount } from 'svelte';
	import { api, type ApiKey } from '#lib/api.ts';
	import { useProject } from '#lib/project.svelte.ts';
	import { t, relativeTime } from '#lib/i18n.svelte.ts';

	const project = useProject();
	let keys = $state<ApiKey[]>([]);
	let creating = $state(false);
	let name = $state('');
	let scope = $state('read');
	let busy = $state(false);
	let error = $state('');
	let created = $state<string | null>(null);
	let removing = $state<ApiKey | null>(null);

	const origin = typeof location !== 'undefined' ? location.origin : '';
	const lang = $derived(project.detail!.languages.find((l) => l.code !== project.base)?.code ?? project.base);
	const sample = $derived(created ?? 'lx_…');
	const curlExport = $derived(
		`curl -H "X-API-Key: ${sample}" \\\n  "${origin}/api/projects/${project.slug}/export?format=json&lang=${lang}" -o ${lang}.json`
	);
	const curlImport = $derived(
		`curl -H "X-API-Key: ${sample}" \\\n  -F file=@locales/${project.base}.json \\\n  "${origin}/api/projects/${project.slug}/import"`
	);
	const jsFetch = $derived(
		`const res = await fetch('${origin}/api/projects/${project.slug}/export?format=json-nested&lang=${lang}', {\n  headers: { 'X-API-Key': '${sample}' }\n});\nconst messages = await res.json();`
	);

	async function load() {
		keys = (await api.get<{ api_keys: ApiKey[] }>(project.api('/api-keys'))).api_keys;
	}
	onMount(load);

	async function create() {
		busy = true;
		error = '';
		try {
			const res = await api.post<{ key: string }>(project.api('/api-keys'), { name, scope });
			created = res.key;
			name = '';
			await load();
		} catch (e) {
			error = (e as Error).message;
		} finally {
			busy = false;
		}
	}

	async function remove(k: ApiKey) {
		await api.del(project.api('/api-keys/' + k.id));
		toast.success(t('apikeys.revoked', { name: k.name }));
		removing = null;
		await load();
	}
</script>

<Stack gap="4">
	<Row gap={8} align="center" justify="space-between" wrap>
		<p class="lx-muted p">{t('apikeys.intro')}</p>
		<Button variant="filled" size="sm" onclick={() => { creating = true; created = null; error = ''; }}>
			{#snippet leading()}<Plus size={14} />{/snippet}
			{t('apikeys.new')}
		</Button>
	</Row>

	{#if keys.length}
		<RecordList
			columns="minmax(0,1.2fr) 7rem 6rem minmax(0,1fr) minmax(0,1fr) 2.75rem"
			labels={[t('apikeys.name'), t('apikeys.prefix'), t('apikeys.scope'), t('apikeys.created'), t('apikeys.lastUsed')]}
			aria-label={t('project.tab.api')}
		>
			{#each keys as k (k.id)}
				<RecordRow>
					{#snippet identity()}<strong>{k.name}</strong>{/snippet}
					<RecordCell label={t('apikeys.prefix')}><span class="lx-mono">{k.prefix}…</span></RecordCell>
					<RecordCell label={t('apikeys.scope')}>
						<Chip size="sm" variant={k.scope === 'write' ? 'warning' : 'default'} label={t('apikeys.scope.' + k.scope)} />
					</RecordCell>
					<RecordCell label={t('apikeys.created')}>{relativeTime(k.created_at)}{k.created_by ? ' · ' + k.created_by : ''}</RecordCell>
					<RecordCell label={t('apikeys.lastUsed')}>{k.last_used_at ? relativeTime(k.last_used_at) : t('apikeys.never')}</RecordCell>
					{#snippet actions()}
						<IconButton variant="text" size="sm" aria-label={t('apikeys.revokeFor', { name: k.name })} onclick={() => (removing = k)}>
							<Trash2 size={16} />
						</IconButton>
					{/snippet}
				</RecordRow>
			{/each}
		</RecordList>
	{:else}
		<EmptyState icon={KeyRound} title={t('apikeys.empty.title')} description={t('apikeys.empty.body')} />
	{/if}

	<Card title={t('apikeys.usage')} headingLevel={2}>
		<Stack gap="3">
			<p class="lx-muted p">{t('apikeys.usageIntro')}</p>
			<CodeTabs>
				<CodeTab label="Export (curl)"><pre><code>{curlExport}</code></pre></CodeTab>
				<CodeTab label="Import (curl)"><pre><code>{curlImport}</code></pre></CodeTab>
				<CodeTab label="JavaScript"><pre><code>{jsFetch}</code></pre></CodeTab>
			</CodeTabs>
		</Stack>
	</Card>
</Stack>

<Dialog bind:open={creating} title={t('apikeys.new')} size="md">
	<Stack gap="4">
		{#if error}<Alert severity="error">{error}</Alert>{/if}
		{#if created}
			<Alert severity="success" title={t('apikeys.createdTitle')}>{t('apikeys.createdBody')}</Alert>
			<Row gap={8} align="center">
				<code class="secret">{created}</code>
				<CopyButton value={created} label="" aria-label={t('apikeys.copy')} toast={t('common.copied')} />
			</Row>
		{:else}
			<TextField label={t('apikeys.name')} bind:value={name} placeholder="CI, website, mobile app" />
			<SegmentedControl
				aria-label={t('apikeys.scope')}
				bind:value={scope}
				items={[
					{ value: 'read', label: t('apikeys.scope.read') },
					{ value: 'write', label: t('apikeys.scope.write') }
				]}
				fullWidth
			/>
			<p class="lx-muted p small">{t('apikeys.scopeHelp.' + scope)}</p>
		{/if}
	</Stack>
	{#snippet actions()}
		{#if created}
			<Button variant="filled" onclick={() => { creating = false; created = null; }}>{t('common.done')}</Button>
		{:else}
			<Button variant="ghost" onclick={() => (creating = false)}>{t('common.cancel')}</Button>
			<Button variant="filled" loading={busy} disabled={!name} onclick={create}>{t('apikeys.create')}</Button>
		{/if}
	{/snippet}
</Dialog>

<AlertDialog
	open={removing !== null}
	title={t('apikeys.revokeTitle', { name: removing?.name ?? '' })}
	description={t('apikeys.revokeBody')}
	confirmLabel={t('apikeys.revoke')}
	destructive
	onconfirm={() => remove(removing!)}
	oncancel={() => (removing = null)}
/>

<style>
	.p {
		margin: 0;
	}
	.small {
		font-size: var(--ca-font-size-caption, 0.8rem);
	}
	pre {
		margin: 0;
		overflow-x: auto;
		font-family: var(--ca-font-mono);
		font-size: 0.8125rem;
		line-height: 1.6;
	}
	.secret {
		flex: 1;
		min-width: 0;
		overflow-wrap: anywhere;
		padding: var(--ca-space-2) var(--ca-space-3);
		background: var(--ca-surface-sunken);
		font-family: var(--ca-font-mono);
	}
</style>
