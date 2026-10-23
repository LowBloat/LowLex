<!-- @codeatlasdev/ui demo #460 · MIT License (c) CodeAtlas · https://ui.codeatlas.com.br/docs/navigation/tabs#line -->
<script lang="ts">
	import { PageShell, Tabs, TabsList, TabsTrigger, EmptyState, Button, Chip, Row, Skeleton, Stack, Progress } from '@codeatlasdev/ui';
	import { watchJobs, percent } from '#lib/jobs.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import type { Job } from '#lib/api.ts';
	import { FolderX } from '@lucide/svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { ProjectState, provideProject } from '#lib/project.svelte.ts';
	import { t } from '#lib/i18n.svelte.ts';

	let { children } = $props();

	// One state object per slug; the pages below read it through context.
	const proj = new ProjectState(page.params.slug!);
	provideProject(proj);

	$effect(() => {
		const slug = page.params.slug!;
		if (slug !== proj.slug) {
			proj.slug = slug;
			proj.detail = null;
		}
		proj.load();
	});

	const tabs = $derived.by(() => {
		const can = proj.detail?.permissions;
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

	// running jobs of this project show as a strip under the tabs; when one ends, numbers refresh
	let active = $state<Job[]>([]);
	$effect(() => {
		const slug = page.params.slug!;
		let had = 0;
		return watchJobs('?active=1&project=' + encodeURIComponent(slug), (jobs) => {
			if (jobs.length < had) {
				proj.load();
				session.loadProjects();
			}
			had = jobs.length;
			active = jobs;
		}, 2000);
	});
</script>

{#if proj.error && !proj.detail}
	<PageShell>
		<EmptyState icon={FolderX} title={t('project.notFound')} description={proj.error}>
			{#snippet actions()}
				<Button variant="filled" href="/projects">{t('nav.allProjects')}</Button>
			{/snippet}
		</EmptyState>
	</PageShell>
{:else if !proj.detail}
	<PageShell>
		<Stack gap="4">
			<Skeleton width="40%" height={32} />
			<Skeleton rows={6} />
		</Stack>
	</PageShell>
{:else}
	<PageShell title={proj.project.name} description={proj.project.description || undefined}>
		{#snippet actions()}
			<Row gap={8} wrap>
				<Chip size="sm" label={t('project.role.' + proj.detail!.role)} />
				<Chip size="sm" label={proj.project.slug} />
			</Row>
		{/snippet}
		<Tabs value={current} variant="line" onchange={(v) => goto(proj.path + (v ? '/' + v : ''))}>
			<TabsList aria-label={t('project.sections')} fade>
				{#each tabs as tab (tab.value)}
					<TabsTrigger value={tab.value}>{tab.label}</TabsTrigger>
				{/each}
			</TabsList>
		</Tabs>
		{#each active as j (j.id)}
			<div class="job" role="status">
				<Row gap={12} align="center" justify="space-between" wrap>
					<strong>{j.title}</strong>
					<a href="/jobs">{t('jobs.see')}</a>
				</Row>
				<Progress value={percent(j)} label="{j.title} {percent(j)}%" />
				<span class="lx-mono lx-muted small">{j.done.toLocaleString()} / {j.total.toLocaleString()} · {percent(j)}%</span>
			</div>
		{/each}
		{@render children()}
	</PageShell>
{/if}

<style>
	.job {
		display: grid;
		gap: var(--ca-space-2);
		padding: var(--ca-space-3) var(--ca-space-4);
		background: var(--ca-surface-sunken);
		border-left: 3px solid var(--lx-signal);
	}
	.small {
		font-size: var(--ca-font-size-caption, 0.8rem);
	}
</style>
