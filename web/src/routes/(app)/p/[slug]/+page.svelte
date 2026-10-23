<!-- @codeatlasdev/ui demo #245 · MIT License (c) CodeAtlas · https://ui.codeatlas.com.br/docs/layout/record-list#apps -->
<!-- @codeatlasdev/ui demo #400 · MIT License (c) CodeAtlas · https://ui.codeatlas.com.br/docs/navigation/menu#checkbox-and-radio-items -->
<!-- @codeatlasdev/ui demo #168 · MIT License (c) CodeAtlas · https://ui.codeatlas.com.br/docs/layout/empty-state#hero -->
<script lang="ts">
	import {
		RecordList, RecordRow, RecordCell, TextField, Select, Button, Row, Stack, Chip, Checkbox,
		DropdownMenu, DropdownMenuItem, DropdownMenuCheckboxItem, DropdownMenuLabel, IconButton, Pagination,
		EmptyState, Skeleton, AlertDialog, toast
	} from '@codeatlasdev/ui';
	import { Search, Plus, Sparkles, MoreHorizontal, Pencil, Trash2, KeyRound, SearchX, Upload, Columns3 } from '@lucide/svelte';
	import { onMount, untrack } from 'svelte';
	import { api, type Key, type Provider } from '#lib/api.ts';
	import { useProject } from '#lib/project.svelte.ts';
	import { langName } from '#lib/session.svelte.ts';
	import { t, num } from '#lib/i18n.svelte.ts';
	import { stateStatus } from '#lib/states.ts';
	import KeyEditor from '#lib/components/KeyEditor.svelte';
	import NewKeyDialog from '#lib/components/NewKeyDialog.svelte';
	import BatchTranslateDialog from '#lib/components/BatchTranslateDialog.svelte';

	const project = useProject();
	const PER = 50;

	let keys = $state<Key[]>([]);
	let total = $state(0);
	let loading = $state(true);
	let pageNo = $state(1);
	let search = $state('');
	let query = $state('');
	let namespace = $state('*');
	let filter = $state('all');
	let filterLang = $state('');
	let namespaces = $state<{ name: string; keys: number }[]>([]);
	let allTags = $state<string[]>([]);
	let providers = $state<Provider[]>([]);
	let shown = $state<string[]>([]);
	let selected = $state<number[]>([]);

	let editorOpen = $state(false);
	let editing = $state<Key | null>(null);
	let focusLang = $state('');
	let newKeyOpen = $state(false);
	let batchOpen = $state(false);
	let confirmDelete = $state<number[] | null>(null);

	const allLangs = $derived(project.detail!.languages.map((l) => l.code));
	const storageKey = $derived('lowlex-shown-' + project.slug);

	onMount(() => {
		let saved: string[] = [];
		try {
			saved = JSON.parse(localStorage.getItem(storageKey) ?? '[]');
		} catch {
			/* ignore */
		}
		saved = saved.filter((c) => allLangs.includes(c));
		shown = saved.length ? saved : allLangs.length <= 4 ? allLangs : [project.base, ...allLangs.filter((l) => l !== project.base).slice(0, 3)];
		filterLang = allLangs.find((l) => l !== project.base) ?? project.base;
		api.get<{ providers: Provider[] }>('/providers').then((r) => (providers = r.providers.filter((p) => p.enabled))).catch(() => {});
		loadMeta();
	});

	function toggleShown(code: string, on: boolean) {
		const next = on ? allLangs.filter((c) => c === code || shown.includes(c)) : shown.filter((c) => c !== code);
		if (!next.length) return;
		shown = next;
		try {
			localStorage.setItem(storageKey, JSON.stringify(next));
		} catch {
			/* ignore */
		}
	}

	// Search waits for a pause in typing.
	$effect(() => {
		const v = search;
		const id = setTimeout(() => {
			if (v !== query) {
				query = v;
				pageNo = 1;
			}
		}, 250);
		return () => clearTimeout(id);
	});

	function params() {
		const p = new URLSearchParams({ page: String(pageNo), per: String(PER) });
		if (query) p.set('q', query);
		if (namespace !== '*') p.set('ns', namespace);
		if (filter !== 'all' && filterLang) {
			p.set('state', filter);
			p.set('lang', filterLang);
		}
		return p;
	}

	$effect(() => {
		load(params());
	});

	let seq = 0;
	async function load(params: URLSearchParams) {
		const mine = ++seq;
		loading = true;
		try {
			const res = await api.get<{ keys: Key[]; total: number }>(project.api('/keys?' + params));
			if (mine !== seq) return;
			keys = res.keys;
			total = res.total;
			selected = selected.filter((id) => res.keys.some((k) => k.id === id));
		} catch (e) {
			toast.error((e as Error).message);
		} finally {
			if (mine === seq) loading = false;
		}
	}

	async function loadMeta() {
		const res = await api.get<{ namespaces: { name: string; keys: number }[]; tags: string[] }>(project.api('/namespaces'));
		namespaces = res.namespaces;
		allTags = res.tags;
	}

	function refresh() {
		load(params());
		project.load();
		loadMeta();
	}

	function open(k: Key, lang = '') {
		editing = k;
		focusLang = lang;
		editorOpen = true;
	}

	// Keep the list in sync with edits made in the drawer.
	$effect(() => {
		const k = editing;
		if (k) untrack(() => (keys = keys.map((x) => (x.id === k.id ? k : x))));
	});

	async function remove(ids: number[]) {
		await api.post(project.api('/keys/delete'), { ids });
		toast.success(t('keys.deleted', { n: ids.length }));
		selected = [];
		refresh();
	}

	function toggle(id: number, on: boolean) {
		selected = on ? [...selected, id] : selected.filter((x) => x !== id);
	}

	const columns = $derived(
		'minmax(10rem,1.1fr) ' +
			shown.map(() => 'minmax(0,1.4fr)').join(' ') +
			' 2.75rem'
	);
	const nsOptions = $derived([
		{ value: '*', label: t('keys.allNamespaces') },
		...namespaces.map((n) => ({ value: n.name, label: n.name || t('keys.defaultNamespace') }))
	]);
	const filterOptions = $derived([
		{ value: 'all', label: t('filter.all') },
		{ value: 'missing', label: t('filter.missing') },
		{ value: 'draft', label: t('filter.draft') },
		{ value: 'unreviewed', label: t('filter.unreviewed') },
		{ value: 'reviewed', label: t('filter.reviewed') }
	]);
	const langOptions = $derived(allLangs.map((c) => ({ value: c, label: `${langName(c)} (${c})` })));
	const hasFilters = $derived(!!query || namespace !== '*' || filter !== 'all');
</script>

<Stack gap="4">
	<Row gap={8} align="end" wrap>
		<div class="search">
			<TextField label={t('keys.search')} hideLabel placeholder={t('keys.searchPlaceholder')} bind:value={search} size="sm">
				{#snippet leading()}<Search size={16} />{/snippet}
			</TextField>
		</div>
		{#if namespaces.length > 1 || (namespaces.length === 1 && namespaces[0].name)}
			<div class="filter">
				<Select aria-label={t('keys.namespace')} size="sm" bind:value={namespace} options={nsOptions} onchange={() => (pageNo = 1)} />
			</div>
		{/if}
		<div class="filter">
			<Select aria-label={t('filter.label')} size="sm" bind:value={filter} options={filterOptions} onchange={() => (pageNo = 1)} />
		</div>
		{#if filter !== 'all'}
			<div class="filter">
				<Select aria-label={t('filter.in')} size="sm" bind:value={filterLang} options={langOptions} onchange={() => (pageNo = 1)} />
			</div>
		{/if}
		<DropdownMenu>
			{#snippet triggerChild(props)}
				<Button variant="outlined" size="sm" {...props}>
					{#snippet leading()}<Columns3 size={14} />{/snippet}
					{t('keys.shownCount', { n: shown.length, total: allLangs.length })}
				</Button>
			{/snippet}
			<DropdownMenuLabel>{t('keys.shownLanguages')}</DropdownMenuLabel>
			{#each allLangs as code (code)}
				<DropdownMenuCheckboxItem
					checked={shown.includes(code)}
					disabled={shown.length === 1 && shown.includes(code)}
					onCheckedChange={(on) => toggleShown(code, on)}
				>
					{langName(code)} <span class="lx-mono lx-muted">{code}</span>
				</DropdownMenuCheckboxItem>
			{/each}
		</DropdownMenu>
		<span class="spacer"></span>
		{#if project.can.translate && providers.length}
			<Button variant="outlined" size="sm" onclick={() => (batchOpen = true)}>
				{#snippet leading()}<Sparkles size={14} />{/snippet}
				{t('batch.open')}
			</Button>
		{/if}
		{#if project.can.manage_keys}
			<Button variant="filled" size="sm" onclick={() => (newKeyOpen = true)}>
				{#snippet leading()}<Plus size={14} />{/snippet}
				{t('keys.new')}
			</Button>
		{/if}
	</Row>

	{#if selected.length}
		<Row gap={8} align="center" wrap class="bulk">
			<strong>{t('keys.selectedCount', { n: selected.length })}</strong>
			{#if providers.length}
				<Button variant="outlined" size="sm" onclick={() => (batchOpen = true)}>
					{#snippet leading()}<Sparkles size={14} />{/snippet}
					{t('batch.translateSelected')}
				</Button>
			{/if}
			<Button variant="destructive" size="sm" onclick={() => (confirmDelete = [...selected])}>
				{#snippet leading()}<Trash2 size={14} />{/snippet}
				{t('common.delete')}
			</Button>
			<Button variant="ghost" size="sm" onclick={() => (selected = [])}>{t('common.clearSelection')}</Button>
		</Row>
	{/if}

	<p class="count lx-muted" aria-live="polite">
		{loading ? t('common.loading') : t('keys.count', { n: num(total), total: num(project.detail!.keys) })}
	</p>

	{#if loading && !keys.length}
		<Skeleton rows={8} />
	{:else if !keys.length}
		{#if hasFilters}
			<EmptyState icon={SearchX} title={t('keys.noResults')} description={t('keys.noResultsBody')}>
				{#snippet actions()}
					<Button variant="outlined" onclick={() => { search = ''; query = ''; namespace = '*'; filter = 'all'; }}>{t('common.clearFilters')}</Button>
				{/snippet}
			</EmptyState>
		{:else}
			<EmptyState variant="hero" icon={KeyRound} title={t('keys.empty.title')} description={t('keys.empty.body')}>
				{#snippet actions()}
					{#if project.can.manage_keys}
						<Button variant="filled" onclick={() => (newKeyOpen = true)}>{t('keys.new')}</Button>
						<Button variant="ghost" href={project.path + '/transfer'}>
							{#snippet leading()}<Upload size={14} />{/snippet}
							{t('keys.empty.import')}
						</Button>
					{/if}
				{/snippet}
			</EmptyState>
		{/if}
	{:else}
		<RecordList {columns} labels={[t('keys.key'), ...shown.map((c) => langName(c))]} aria-label={t('keys.title')}>
			{#each keys as k (k.id)}
				<RecordRow>
					{#snippet identity()}
						<div class="identity">
							{#if project.can.manage_keys}
								<Checkbox
									aria-label={t('keys.select', { name: k.name })}
									checked={selected.includes(k.id)}
									onchange={(on) => toggle(k.id, on)}
								/>
							{/if}
							<button type="button" class="key" onclick={() => open(k)}>
								<span class="name lx-mono">{#each k.name.split(".") as part, i (i)}{i ? "." : ""}<wbr />{part}{/each}</span>
								{#if k.namespace || k.tags.length}
									<span class="chips">
										{#if k.namespace}<Chip size="sm" label={k.namespace} />{/if}
										{#each k.tags as tag (tag)}<Chip size="sm" variant="info" label={tag} />{/each}
									</span>
								{/if}
								{#if k.description}<span class="kdesc">{k.description}</span>{/if}
							</button>
						</div>
					{/snippet}
					{#each shown as lang (lang)}
						{@const tr = k.translations[lang]}
						<RecordCell label={langName(lang)}>
							<button type="button" class="cell" data-state={tr?.state ?? 'missing'} onclick={() => open(k, lang)} aria-label={t('keys.editCell', { key: k.name, lang: langName(lang) })}>
								<span class="dot" data-status={stateStatus(tr)} aria-hidden="true"></span>
								{#if tr}
									<span class="text">{tr.text}</span>
								{:else}
									<span class="empty">{t('state.missing')}</span>
								{/if}
							</button>
						</RecordCell>
					{/each}
					{#snippet actions()}
						<DropdownMenu align="end">
							{#snippet triggerChild(props)}
								<IconButton {...props} variant="text" aria-label={t('keys.actionsFor', { name: k.name })}><MoreHorizontal size={18} /></IconButton>
							{/snippet}
							<DropdownMenuItem onclick={() => open(k)}>
								{#snippet icon()}<Pencil size={16} />{/snippet}
								{t('common.edit')}
							</DropdownMenuItem>
							{#if project.can.manage_keys}
								<DropdownMenuItem variant="destructive" onclick={() => (confirmDelete = [k.id])}>
									{#snippet icon()}<Trash2 size={16} />{/snippet}
									{t('common.delete')}
								</DropdownMenuItem>
							{/if}
						</DropdownMenu>
					{/snippet}
				</RecordRow>
			{/each}
		</RecordList>

		{#if total > PER}
			<Row justify="center"><Pagination count={total} perPage={PER} bind:page={pageNo} /></Row>
		{/if}
	{/if}
</Stack>

<KeyEditor bind:open={editorOpen} bind:key={editing} {focusLang} languages={allLangs} {providers} tags={allTags} onchange={() => { project.load(); loadMeta(); }} />
<NewKeyDialog bind:open={newKeyOpen} oncreated={(k) => { refresh(); open(k); }} />
<BatchTranslateDialog bind:open={batchOpen} {providers} defaultLang={filter !== 'all' ? filterLang : ''} keyIds={selected} ondone={refresh} />
<AlertDialog
	open={confirmDelete !== null}
	title={t('keys.deleteTitle', { n: confirmDelete?.length ?? 0 })}
	description={t('keys.deleteBody')}
	confirmLabel={t('common.delete')}
	destructive
	onconfirm={() => remove(confirmDelete ?? [])}
	oncancel={() => (confirmDelete = null)}
/>

<style>
	.search {
		flex: 1 1 16rem;
		min-width: 12rem;
	}
	.filter {
		width: 11rem;
	}
	.spacer {
		flex: 1 1 0;
	}
	.count {
		margin: 0;
		font-size: var(--ca-font-size-caption, 0.8rem);
	}
	:global(.bulk) {
		padding: var(--ca-space-2) var(--ca-space-3);
		background: var(--ca-surface-sunken);
		border-left: 3px solid var(--lx-signal);
	}
	.identity {
		display: flex;
		align-items: flex-start;
		gap: var(--ca-space-2);
		min-width: 0;
	}
	.key,
	.cell {
		all: unset;
		box-sizing: border-box;
		cursor: pointer;
		min-width: 0;
		width: 100%;
	}
	.key {
		display: grid;
		gap: var(--ca-space-1);
	}
	.key:focus-visible,
	.cell:focus-visible {
		outline: 2px solid var(--ca-focus-ring, #4f7400);
		outline-offset: 2px;
	}
	.name {
		font-weight: 600;
		overflow-wrap: break-word;
	}
	.key:hover .name {
		text-decoration: underline;
	}
	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: var(--ca-space-1);
	}
	.kdesc {
		color: var(--ca-text-secondary);
		font-size: var(--ca-font-size-caption, 0.8rem);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.cell {
		display: flex;
		align-items: flex-start;
		gap: var(--ca-space-2);
		padding: var(--ca-space-1) var(--ca-space-2);
		margin: calc(-1 * var(--ca-space-1)) calc(-1 * var(--ca-space-2));
		min-height: 2rem;
	}
	.cell:hover {
		background: var(--ca-surface-sunken);
	}
	.text {
		display: -webkit-box;
		-webkit-line-clamp: 3;
		line-clamp: 3;
		-webkit-box-orient: vertical;
		overflow: hidden;
		white-space: pre-wrap;
		overflow-wrap: break-word;
	}
	.empty {
		color: var(--ca-text-secondary);
		font-style: italic;
	}
	.dot {
		flex: none;
		width: 8px;
		height: 8px;
		margin-top: 0.45em;
		background: var(--ca-outline-strong, #a9b1a5);
	}
	.dot[data-status='info'] {
		background: var(--ca-text-secondary);
	}
	.dot[data-status='warning'] {
		background: var(--ca-warning-main, #c98a00);
	}
	.dot[data-status='success'] {
		background: var(--lx-signal);
		box-shadow: inset 0 0 0 1px #5d8a00;
	}
	.cell[data-state='missing'] .dot {
		background: transparent;
		box-shadow: inset 0 0 0 1px var(--ca-outline-strong, #a9b1a5);
	}
</style>
