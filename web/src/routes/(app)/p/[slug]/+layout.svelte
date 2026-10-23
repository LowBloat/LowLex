<!-- @codeatlasdev/ui demo #460 · MIT License (c) CodeAtlas · https://ui.codeatlas.com.br/docs/navigation/tabs#line -->
<script lang="ts">
	import { PageShell, Tabs, TabsList, TabsTrigger, EmptyState, Button, Chip, Row, Skeleton, Stack } from '@codeatlasdev/ui';
	import { FolderX } from '@lucide/svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { ProjectState, provideProject } from '#lib/project.svelte.ts';
	import { t } from '#lib/i18n.svelte.ts';

	let { children } = $props();

	// One state object per slug; the pages below read it through context.
	const state = new ProjectState(page.params.slug!);
	provideProject(state);

	$effect(() => {
		const slug = page.params.slug!;
		if (slug !== state.slug) {
			state.slug = slug;
			state.detail = null;
		}
		state.load();
	});

	const tabs = $derived.by(() => {
		const can = state.detail?.permissions;
		const list = [{ value: '', label: t('project.tab.translations') }];
		list.push({ value: 'languages', label: t('project.tab.languages') });
		if (can?.manage_keys) list.push({ value: 'transfer', label: t('project.tab.transfer') });
		else list.push({ value: 'transfer', label: t('project.tab.export') });
		if (can?.manage_project) list.push({ value: 'members', label: t('project.tab.members') });
		if (can?.manage_keys) list.push({ value: 'api', label: t('project.tab.api') });
		list.push({ value: 'activity', label: t('project.tab.activity') });
		if (can?.manage_project) list.push({ value: 'settings', label: t('project.tab.settings') });
		return list;
	});

	const current = $derived(page.url.pathname.split('/')[3] ?? '');
</script>

{#if state.error && !state.detail}
	<PageShell>
		<EmptyState icon={FolderX} title={t('project.notFound')} description={state.error}>
			{#snippet actions()}
				<Button variant="filled" href="/projects">{t('nav.allProjects')}</Button>
			{/snippet}
		</EmptyState>
	</PageShell>
{:else if !state.detail}
	<PageShell>
		<Stack gap="4">
			<Skeleton width="40%" height={32} />
			<Skeleton rows={6} />
		</Stack>
	</PageShell>
{:else}
	<PageShell title={state.project.name} description={state.project.description || undefined}>
		{#snippet actions()}
			<Row gap={8} wrap>
				<Chip size="sm" label={t('project.role.' + state.detail!.role)} />
				<Chip size="sm" label={state.project.slug} />
			</Row>
		{/snippet}
		<Tabs value={current} variant="line" onchange={(v) => goto(state.path + (v ? '/' + v : ''))}>
			<TabsList aria-label={t('project.sections')} fade>
				{#each tabs as tab (tab.value)}
					<TabsTrigger value={tab.value}>{tab.label}</TabsTrigger>
				{/each}
			</TabsList>
		</Tabs>
		{@render children()}
	</PageShell>
{/if}
