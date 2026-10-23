<!-- @codeatlasdev/ui demo #245 · MIT License (c) CodeAtlas · https://ui.codeatlas.com.br/docs/layout/record-list#apps -->
<script lang="ts">
	import { RecordList, RecordRow, RecordCell, EmptyState, Skeleton } from '@codeatlasdev/ui';
	import { History } from '@lucide/svelte';
	import { onMount } from 'svelte';
	import { api, type Activity } from '#lib/api.ts';
	import { useProject } from '#lib/project.svelte.ts';
	import { t, relativeTime, formatDate } from '#lib/i18n.svelte.ts';

	const project = useProject();
	let items = $state<Activity[] | null>(null);

	onMount(async () => {
		items = (await api.get<{ activity: Activity[] }>(project.api('/activity?limit=200'))).activity;
	});

	function label(action: string) {
		const key = 'activity.' + action;
		const s = t(key);
		return s === key ? action : s;
	}
</script>

{#if items === null}
	<Skeleton rows={6} />
{:else if !items.length}
	<EmptyState icon={History} title={t('activity.empty')} />
{:else}
	<RecordList
		columns="minmax(0,1fr) minmax(0,1.2fr) minmax(0,2fr)"
		labels={[t('activity.when'), t('activity.what'), t('activity.detail')]}
		aria-label={t('project.tab.activity')}
	>
		{#each items as a (a.id)}
			<RecordRow>
				{#snippet identity()}
					<span title={formatDate(a.created_at)}>{relativeTime(a.created_at)}</span>
				{/snippet}
				<RecordCell label={t('activity.what')}>
					<strong>{a.username || t('activity.apiKey')}</strong> · {label(a.action)}
				</RecordCell>
				<RecordCell label={t('activity.detail')}><span class="lx-mono detail">{a.detail}</span></RecordCell>
			</RecordRow>
		{/each}
	</RecordList>
{/if}

<style>
	.detail {
		overflow-wrap: anywhere;
	}
</style>
