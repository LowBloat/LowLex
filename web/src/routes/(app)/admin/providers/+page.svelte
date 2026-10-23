<!-- @codeatlasdev/ui demo #168 · MIT License (c) CodeAtlas · https://ui.codeatlas.com.br/docs/layout/empty-state#hero -->
<!-- @codeatlasdev/ui demo #085 · MIT License (c) CodeAtlas · https://ui.codeatlas.com.br/docs/feedback/dialog#form -->
<script lang="ts">
	import {
		PageShell, Card, Grid, Stack, Row, Button, Chip, Switch, Dialog, Select, TextField, TextArea, Alert,
		AlertDialog, EmptyState, Callout, toast
	} from '@codeatlasdev/ui';
	import { Plus, Sparkles, Languages, Pencil, Trash2, FlaskConical } from '@lucide/svelte';
	import { onMount } from 'svelte';
	import { api, type Provider, type ProviderType } from '#lib/api.ts';
	import { t, tOr } from '#lib/i18n.svelte.ts';

	let providers = $state<Provider[]>([]);
	let types = $state<ProviderType[]>([]);
	let open = $state(false);
	let editing = $state<Provider | null>(null);
	let removing = $state<Provider | null>(null);
	let type = $state('openai');
	let name = $state('');
	let values = $state<Record<string, string>>({});
	let error = $state('');
	let busy = $state(false);
	let tests = $state<Record<number, { ok: boolean; text?: string; error?: string; ms?: number; busy?: boolean }>>({});

	const info = $derived(types.find((x) => x.type === type));

	async function load() {
		const [p, ty] = await Promise.all([
			api.get<{ providers: Provider[] }>('/providers'),
			api.get<{ types: ProviderType[] }>('/provider-types')
		]);
		providers = p.providers;
		types = ty.types;
	}
	onMount(load);

	function typeLabel(tp: string) {
		return tOr('providerType.' + tp, types.find((x) => x.type === tp)?.label ?? tp);
	}

	function openNew() {
		editing = null;
		type = 'openai';
		name = '';
		values = {};
		error = '';
		applyDefaults();
		open = true;
	}

	function applyDefaults() {
		const ti = types.find((x) => x.type === type);
		const v: Record<string, string> = {};
		for (const f of ti?.fields ?? []) v[f.name] = f.default ?? '';
		values = v;
		name = ti ? tOr('providerType.' + ti.type, ti.label) : '';
	}

	function openEdit(p: Provider) {
		editing = p;
		type = p.type;
		name = p.name;
		values = {
			base_url: p.config?.base_url ?? '',
			region: p.config?.region ?? '',
			model: p.config?.model ?? '',
			prompt: p.config?.prompt ?? '',
			api_key: ''
		};
		error = '';
		open = true;
	}

	async function save() {
		busy = true;
		error = '';
		const config: Record<string, string> = {};
		for (const f of info?.fields ?? []) config[f.name] = values[f.name] ?? '';
		try {
			if (editing) {
				await api.patch('/providers/' + editing.id, { name, config });
			} else {
				await api.post('/providers', { type, name, config });
			}
			toast.success(t('providers.saved'));
			open = false;
			await load();
		} catch (e) {
			error = (e as Error).message;
		} finally {
			busy = false;
		}
	}

	async function toggle(p: Provider, enabled: boolean) {
		try {
			await api.patch('/providers/' + p.id, { enabled });
			p.enabled = enabled;
		} catch (e) {
			toast.error((e as Error).message);
		}
	}

	async function test(p: Provider) {
		tests[p.id] = { ok: false, busy: true };
		try {
			tests[p.id] = await api.post('/providers/' + p.id + '/test', { text: t('providers.testText'), source: 'en', target: 'pt-BR' });
		} catch (e) {
			tests[p.id] = { ok: false, error: (e as Error).message };
		}
	}

	async function remove(p: Provider) {
		await api.del('/providers/' + p.id);
		toast.success(t('providers.deleted', { name: p.name }));
		removing = null;
		await load();
	}
</script>

<PageShell title={t('providers.title')} description={t('providers.subtitle')}>
	{#snippet actions()}
		<Button variant="filled" size="sm" onclick={openNew} disabled={!types.length}>
			{#snippet leading()}<Plus size={14} />{/snippet}
			{t('providers.add')}
		</Button>
	{/snippet}

	{#if !providers.length}
		<EmptyState variant="hero" icon={Sparkles} title={t('providers.empty.title')} description={t('providers.empty.body')}>
			{#snippet actions()}
				<Button variant="filled" onclick={openNew} disabled={!types.length}>{t('providers.add')}</Button>
			{/snippet}
		</EmptyState>
	{:else}
		<Grid columns={{ base: 1, md: 2, xl: 3 }} gap="4">
			{#each providers as p (p.id)}
				<Card title={p.name} headingLevel={2}>
					{#snippet headerActions()}
						<Switch aria-label={t('providers.enabledFor', { name: p.name })} checked={p.enabled} onchange={(v) => toggle(p, v)} />
					{/snippet}
					<Stack gap="3">
						<Row gap={6} wrap>
							<Chip size="sm" icon={p.ai ? Sparkles : Languages} variant={p.ai ? 'brand' : 'default'} label={typeLabel(p.type)} />
							{#if p.config?.model}<Chip size="sm" label={p.config.model} />{/if}
							{#if !p.enabled}<Chip size="sm" variant="warning" label={t('providers.disabled')} />{/if}
						</Row>
						{#if p.config?.base_url}<span class="lx-mono lx-muted url">{p.config.base_url}</span>{/if}
						<span class="lx-muted small">
							{p.config?.has_api_key ? t('providers.keySaved') : t('providers.noKey')}
						</span>
						{#if tests[p.id] && !tests[p.id].busy}
							{#if tests[p.id].ok}
								<Alert severity="success">“{tests[p.id].text}” · {tests[p.id].ms} ms</Alert>
							{:else}
								<Alert severity="error">{tests[p.id].error}</Alert>
							{/if}
						{/if}
						<Row gap={6} wrap>
							<Button variant="outlined" size="sm" loading={tests[p.id]?.busy} onclick={() => test(p)}>
								{#snippet leading()}<FlaskConical size={14} />{/snippet}
								{t('providers.test')}
							</Button>
							<Button variant="ghost" size="sm" onclick={() => openEdit(p)}>
								{#snippet leading()}<Pencil size={14} />{/snippet}
								{t('common.edit')}
							</Button>
							<Button variant="ghost" size="sm" onclick={() => (removing = p)}>
								{#snippet leading()}<Trash2 size={14} />{/snippet}
								{t('common.delete')}
							</Button>
						</Row>
					</Stack>
				</Card>
			{/each}
		</Grid>
	{/if}

	<Callout type="tip" title={t('providers.tipTitle')}>{t('providers.tipBody')}</Callout>
</PageShell>

<Dialog bind:open title={editing ? t('providers.edit') : t('providers.add')} size="md">
	<Stack gap="4">
		{#if error}<Alert severity="error">{error}</Alert>{/if}
		{#if !editing}
			<Select
				label={t('providers.type')}
				bind:value={type}
				options={types.map((x) => ({ value: x.type, label: tOr('providerType.' + x.type, x.label) }))}
				onchange={applyDefaults}
			/>
		{/if}
		<TextField label={t('providers.name')} bind:value={name} />
		{#each info?.fields ?? [] as f (f.name)}
			{#if f.multiline}
				<TextArea label={t('providers.field.' + f.name)} bind:value={values[f.name]} rows={3} helperText={t('providers.promptHint')} />
			{:else}
				<TextField
					label={t('providers.field.' + f.name)}
					type={f.secret ? 'password' : 'text'}
					bind:value={values[f.name]}
					required={f.required && !(f.secret && editing?.config?.has_api_key)}
					autocomplete="off"
					placeholder={f.secret && editing?.config?.has_api_key ? t('providers.keepKey') : (f.default ?? '')}
					helperText={f.name === 'base_url' && type === 'openai' ? t('providers.baseUrlHint') : undefined}
				/>
			{/if}
		{/each}
	</Stack>
	{#snippet actions()}
		<Button variant="ghost" onclick={() => (open = false)}>{t('common.cancel')}</Button>
		<Button variant="filled" loading={busy} onclick={save}>{t('common.save')}</Button>
	{/snippet}
</Dialog>

<AlertDialog
	open={removing !== null}
	title={t('providers.deleteTitle', { name: removing?.name ?? '' })}
	description={t('providers.deleteBody')}
	confirmLabel={t('common.delete')}
	destructive
	onconfirm={() => remove(removing!)}
	oncancel={() => (removing = null)}
/>

<style>
	.url {
		overflow-wrap: anywhere;
	}
	.small {
		font-size: var(--ca-font-size-caption, 0.8rem);
	}
</style>
