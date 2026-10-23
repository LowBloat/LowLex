<!-- @codeatlasdev/ui demo #245 · MIT License (c) CodeAtlas · https://ui.codeatlas.com.br/docs/layout/record-list#apps -->
<!-- @codeatlasdev/ui demo #093 · MIT License (c) CodeAtlas · https://ui.codeatlas.com.br/docs/feedback/progress#determinate -->
<script lang="ts">
	import {
		PageShell, RecordList, RecordRow, RecordCell, Progress, StatusChip, Button, Row, Stack, EmptyState, Skeleton, toast
	} from '@codeatlasdev/ui';
	import { ListChecks, RotateCcw, X } from '@lucide/svelte';
	import { onMount } from 'svelte';
	import { api, type Job } from '#lib/api.ts';
	import { watchJobs, isActive, percent, eta, duration, rateLimitWait } from '#lib/jobs.svelte.ts';
	import { t, num, relativeTime, formatDate } from '#lib/i18n.svelte.ts';

	let jobs = $state<Job[] | null>(null);

	onMount(() => watchJobs('?limit=100', (list) => (jobs = list)));

	function status(j: Job): 'success' | 'warning' | 'error' | 'info' | 'neutral' {
		return { done: 'success', failed: 'error', running: 'info', queued: 'neutral', cancelled: 'warning' }[j.status] as never;
	}

	function summary(j: Job): string {
		const r = j.result as Record<string, number> | undefined;
		if (j.kind === 'tolgee.import' && r) return t('jobs.result.tolgee', { keys: num(r.keys ?? 0), texts: num(r.translations ?? 0) });
		if (j.kind === 'mt.batch' && r) return t('jobs.result.mt', { n: num(r.translated ?? 0) });
		return '';
	}

	async function cancel(j: Job) {
		try {
			await api.post(`/jobs/${j.id}/cancel`);
			toast.info(t('jobs.cancelling'));
		} catch (e) {
			toast.error((e as Error).message);
		}
	}

	async function retry(j: Job) {
		try {
			await api.post(`/jobs/${j.id}/retry`);
			toast.success(t('jobs.retried'));
			jobs = (await api.get<{ jobs: Job[] }>('/jobs?limit=100')).jobs;
		} catch (e) {
			toast.error((e as Error).message);
		}
	}
</script>

<PageShell title={t('jobs.title')} description={t('jobs.subtitle')}>
	{#if jobs === null}
		<Skeleton rows={5} />
	{:else if !jobs.length}
		<EmptyState icon={ListChecks} title={t('jobs.empty.title')} description={t('jobs.empty.body')} />
	{:else}
		<RecordList
			columns="minmax(0,1.3fr) minmax(0,1.6fr) 8rem minmax(0,1fr) 5.5rem"
			labels={[t('jobs.job'), t('jobs.progress'), t('jobs.status'), t('jobs.when')]}
			aria-label={t('jobs.title')}
		>
			{#each jobs as j (j.id)}
				{@const left = eta(j)}
				<RecordRow>
					{#snippet identity()}
						<Stack gap="1">
							<strong>{j.title || t('jobs.kind.' + j.kind)}</strong>
							<span class="meta">
								{t('jobs.kind.' + j.kind)}
								{#if j.project_slug}· <a href="/p/{j.project_slug}">{j.project_name}</a>{/if}
							</span>
						</Stack>
					{/snippet}
					<RecordCell label={t('jobs.progress')}>
						<Stack gap="1">
							<Progress
								value={percent(j)}
								indeterminate={j.status === 'running' && j.total === 0}
								label={t('jobs.progressLabel', { done: num(j.done), total: num(j.total) })}
							/>
							<span class="meta lx-mono">
								{num(j.done)} / {num(j.total)} · {percent(j)}%{#if left !== null} · {t('jobs.eta', { time: duration(left) })}{/if}
							</span>
							{#if rateLimitWait(j) !== null}<span class="meta">{t('jobs.rateLimited', { time: duration(rateLimitWait(j)!) })}</span>{/if}
							{#if j.status === 'failed' && j.message}<span class="error">{j.message}</span>{/if}
							{#if j.status === 'done' && summary(j)}<span class="meta">{summary(j)}</span>{/if}
						</Stack>
					</RecordCell>
					<RecordCell label={t('jobs.status')}>
						<StatusChip status={status(j)} label={j.cancelling && isActive(j) ? t('jobs.cancelling') : t('jobs.state.' + j.status)} />
					</RecordCell>
					<RecordCell label={t('jobs.when')}>
						<span title={formatDate(j.created_at)}>{relativeTime(j.finished_at ?? j.started_at ?? j.created_at)}</span>
						<span class="meta"> · {j.created_by}</span>
					</RecordCell>
					{#snippet actions()}
						<Row gap={2}>
							{#if isActive(j) && !j.cancelling}
								<Button variant="ghost" size="sm" onclick={() => cancel(j)} aria-label={t('jobs.cancelFor', { name: j.title })}>
									{#snippet leading()}<X size={14} />{/snippet}
								</Button>
							{:else if j.status === 'failed' || j.status === 'cancelled'}
								<Button variant="ghost" size="sm" onclick={() => retry(j)} aria-label={t('jobs.retryFor', { name: j.title })}>
									{#snippet leading()}<RotateCcw size={14} />{/snippet}
								</Button>
							{/if}
						</Row>
					{/snippet}
				</RecordRow>
			{/each}
		</RecordList>
	{/if}
</PageShell>

<style>
	.meta {
		font-size: var(--ca-font-size-caption, 0.8rem);
		color: var(--ca-text-secondary);
	}
	.error {
		font-size: var(--ca-font-size-caption, 0.8rem);
		color: var(--ca-error-main, var(--ca-text-primary));
		overflow-wrap: anywhere;
	}
</style>
