<!-- @codeatlasdev/ui demo #168 · MIT License (c) CodeAtlas · https://ui.codeatlas.com.br/docs/layout/empty-state#hero -->
<script lang="ts">
	import { PageShell, Card, Grid, Stack, Row, Button, EmptyState, Progress, Chip } from '@codeatlasdev/ui';
	import { Plus, FolderKanban, KeyRound } from '@lucide/svelte';
	import { session, langName } from '#lib/session.svelte.ts';
	import { t, num } from '#lib/i18n.svelte.ts';
	import NewProjectDialog from '#lib/components/NewProjectDialog.svelte';
	import type { ProjectSummary, LangStats } from '#lib/api.ts';

	let creating = $state(false);

	function pct(p: ProjectSummary, l: LangStats) {
		return p.keys ? Math.round((l.translated / p.keys) * 100) : 0;
	}
	function overall(p: ProjectSummary) {
		if (!p.keys || !p.languages.length) return 0;
		const done = p.languages.reduce((s, l) => s + l.translated, 0);
		return Math.round((done / (p.keys * p.languages.length)) * 100);
	}
</script>

<PageShell
	title={t('projects.title')}
	description={session.projects.length ? t('projects.count', { n: session.projects.length }) : t('projects.subtitle')}
>
	{#snippet actions()}
		{#if session.isAdmin}
			<Button variant="filled" size="sm" onclick={() => (creating = true)}>
				{#snippet leading()}<Plus size={14} />{/snippet}
				{t('projects.new')}
			</Button>
		{/if}
	{/snippet}

	{#if session.projects.length === 0}
		<EmptyState
			variant="hero"
			icon={FolderKanban}
			title={session.isAdmin ? t('projects.empty.title') : t('projects.empty.memberTitle')}
			description={session.isAdmin ? t('projects.empty.body') : t('projects.empty.memberBody')}
		>
			{#snippet actions()}
				{#if session.isAdmin}
					<Button variant="filled" onclick={() => (creating = true)}>{t('projects.new')}</Button>
				{/if}
			{/snippet}
		</EmptyState>
	{:else}
		<Grid columns={{ base: 1, md: 2, xl: 3 }} gap="4">
			{#each session.projects as p (p.id)}
				<Card href="/p/{p.slug}" title={p.name} headingLevel={2}>
					<Stack gap="4">
						{#if p.description}<p class="desc">{p.description}</p>{/if}
						<Row gap={8} wrap>
							<Chip size="sm" icon={KeyRound} label={t('projects.keys', { n: num(p.keys) })} />
							<Chip size="sm" label={t('projects.base', { lang: p.base_language })} />
							<Chip size="sm" variant="brand" label={t('projects.done', { n: overall(p) })} />
						</Row>
						<Stack gap="2">
							{#each p.languages.filter((l) => l.code !== p.base_language).slice(0, 4) as l (l.code)}
								<div class="lang">
									<span class="lang-row"><span>{langName(l.code)}</span><span class="lx-mono">{pct(p, l)}%</span></span>
									<Progress value={pct(p, l)} label="{langName(l.code)} {pct(p, l)}%" />
								</div>
							{/each}
							{#if p.languages.length - 1 > 4}
								<span class="more">{t('projects.moreLanguages', { n: p.languages.length - 5 })}</span>
							{/if}
						</Stack>
					</Stack>
				</Card>
			{/each}
		</Grid>
	{/if}
</PageShell>

<NewProjectDialog bind:open={creating} />

<style>
	.desc {
		margin: 0;
		color: var(--ca-text-secondary);
		display: -webkit-box;
		-webkit-line-clamp: 2;
		line-clamp: 2;
		-webkit-box-orient: vertical;
		overflow: hidden;
	}
	.lang {
		display: grid;
		gap: var(--ca-space-1);
	}
	.lang-row {
		display: flex;
		justify-content: space-between;
		gap: var(--ca-space-2);
		font-size: var(--ca-font-size-caption, 0.8rem);
		color: var(--ca-text-secondary);
	}
	.more {
		font-size: var(--ca-font-size-caption, 0.8rem);
		color: var(--ca-text-secondary);
	}
</style>
