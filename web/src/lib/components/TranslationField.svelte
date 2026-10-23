<script lang="ts">
	import { TextArea, Button, Row, Stack, StatusChip, Chip, IconButton, Tooltip, Spinner, toast } from '@codeatlasdev/ui';
	import { focusTextField } from '@codeatlasdev/ui/actions';
	import { Sparkles, Check, X, CornerDownLeft } from '@lucide/svelte';
	import { api, type Key, type Provider, type Suggestion, type Translation } from '#lib/api.ts';
	import { useProject } from '#lib/project.svelte.ts';
	import { langName } from '#lib/session.svelte.ts';
	import { t, relativeTime } from '#lib/i18n.svelte.ts';
	import { stateStatus, originLabel } from '#lib/states.ts';

	let {
		key,
		lang,
		providers,
		focus = false,
		onsaved
	}: {
		key: Key;
		lang: string;
		providers: Provider[];
		focus?: boolean;
		onsaved: (tr: Translation | null) => void;
	} = $props();

	const project = useProject();
	const tr = $derived(key.translations[lang]);
	const isBase = $derived(lang === project.base);
	const locked = $derived(!project.canEdit(lang) || (tr?.state === 'reviewed' && !project.can.review));

	let text = $state('');
	let saving = $state(false);
	let suggesting = $state(false);
	let suggestions = $state<Suggestion[]>([]);
	let ref = $state<HTMLTextAreaElement | null>(null);

	// Reset the draft whenever another key or a new saved value comes in.
	$effect.pre(() => {
		text = key.translations[lang]?.text ?? '';
		suggestions = [];
	});

	// The drawer is a native <dialog>: showModal() focuses the field marked autofocus. When the drawer
	// is already open and another cell is picked, focus it here (desktop only, see focusTextField).
	$effect(() => {
		if (focus && ref && document.activeElement !== ref) focusTextField(ref);
	});

	const dirty = $derived(text !== (tr?.text ?? ''));

	async function save(state?: 'draft' | 'translated' | 'reviewed') {
		saving = true;
		try {
			const res = await api.put<{ translation: Translation | null }>(
				project.api(`/translations/${key.id}/${encodeURIComponent(lang)}`),
				{ text, state: state ?? (tr?.state === 'reviewed' && !dirty ? 'reviewed' : 'translated') }
			);
			onsaved(res.translation);
		} catch (e) {
			toast.error((e as Error).message);
		} finally {
			saving = false;
		}
	}

	async function suggest() {
		suggesting = true;
		suggestions = [];
		try {
			const res = await api.post<{ suggestions: Suggestion[] }>(project.api('/mt/suggest'), { key_id: key.id, lang });
			suggestions = res.suggestions;
		} catch (e) {
			toast.error((e as Error).message);
		} finally {
			suggesting = false;
		}
	}

	function onkeydown(e: KeyboardEvent) {
		if ((e.ctrlKey || e.metaKey) && e.key === 'Enter' && dirty && !locked) {
			e.preventDefault();
			save();
		}
	}
</script>

<section class="field" class:base={isBase} aria-label={langName(lang)}>
	<Row gap={8} align="center" justify="space-between" wrap>
		<Row gap={8} align="center" wrap>
			<strong>{langName(lang)}</strong>
			<span class="lx-mono lx-muted">{lang}</span>
			{#if isBase}<Chip size="sm" variant="brand" label={t('editor.source')} />{/if}
		</Row>
		<Row gap={6} align="center" wrap>
			{#if tr}
				<StatusChip status={stateStatus(tr)} label={t('state.' + tr.state)} />
				{#if tr.origin !== 'human'}<Chip size="sm" label={originLabel(tr.origin)} />{/if}
			{:else}
				<StatusChip status="neutral" label={t('state.missing')} />
			{/if}
		</Row>
	</Row>

	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div {onkeydown}>
		<TextArea
			label={t('editor.textFor', { lang: langName(lang) })}
			hideLabel
			bind:value={text}
			bind:ref
			minRows={2}
			rows={2}
			readonly={locked}
			autofocus={focus && !locked}
			placeholder={locked ? t('editor.locked') : t('editor.placeholder')}
		/>
	</div>

	{#if suggestions.length}
		<ul class="suggestions" aria-label={t('editor.suggestions')}>
			{#each suggestions as s (s.provider_id)}
				<li>
					<Stack gap="1">
						<span class="provider">{s.provider} · {s.ms} ms</span>
						{#if s.error}
							<span class="error">{s.error}</span>
						{:else}
							<span class="text">{s.text}</span>
						{/if}
					</Stack>
					{#if s.text}
						<Tooltip content={t('editor.useSuggestion')}>
							{#snippet triggerChild(props)}
								<IconButton {...props} variant="text" size="sm" aria-label={t('editor.useSuggestion')} onclick={() => { text = s.text!; suggestions = []; }}>
									<CornerDownLeft size={16} />
								</IconButton>
							{/snippet}
						</Tooltip>
					{/if}
				</li>
			{/each}
		</ul>
	{/if}

	<Row gap={8} align="center" justify="space-between" wrap>
		<span class="meta">
			{#if tr}{t('editor.updated', { when: relativeTime(tr.updated_at), who: tr.updated_by || '—' })}{/if}
		</span>
		{#if !locked}
			<Row gap={6} align="center" wrap>
				{#if !isBase && providers.length}
					<Button variant="ghost" size="sm" loading={suggesting} onclick={suggest} disabled={!key.translations[project.base]?.text}>
						{#snippet leading()}<Sparkles size={14} />{/snippet}
						{t('editor.suggest')}
					</Button>
				{/if}
				{#if project.can.review && tr && !dirty}
					{#if tr.state === 'reviewed'}
						<Button variant="ghost" size="sm" loading={saving} onclick={() => save('translated')}>
							{#snippet leading()}<X size={14} />{/snippet}
							{t('editor.unreview')}
						</Button>
					{:else}
						<Button variant="outlined" size="sm" loading={saving} onclick={() => save('reviewed')}>
							{#snippet leading()}<Check size={14} />{/snippet}
							{t('editor.review')}
						</Button>
					{/if}
				{/if}
				{#if dirty}
					<Button variant="ghost" size="sm" onclick={() => (text = tr?.text ?? '')}>{t('common.discard')}</Button>
					<Button variant="filled" size="sm" loading={saving} onclick={() => save()}>{t('common.save')}</Button>
				{/if}
			</Row>
		{/if}
	</Row>
	{#if suggesting}<span class="sr"><Spinner size={14} label={t('editor.suggesting')} /></span>{/if}
</section>

<style>
	.field {
		display: grid;
		gap: var(--ca-space-2);
		padding: var(--ca-space-4) 0;
		border-top: 1px solid var(--ca-outline-subtle);
	}
	.field.base {
		border-top: 0;
		padding-top: 0;
	}
	.meta,
	.provider {
		font-size: var(--ca-font-size-caption, 0.8rem);
		color: var(--ca-text-secondary);
	}
	.suggestions {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		gap: var(--ca-space-1);
	}
	.suggestions li {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--ca-space-2);
		padding: var(--ca-space-2) var(--ca-space-3);
		background: var(--ca-surface-sunken);
		border-left: 3px solid var(--lx-signal);
	}
	.text {
		white-space: pre-wrap;
	}
	.error {
		color: var(--ca-error-main, var(--ca-text-secondary));
		font-size: var(--ca-font-size-caption, 0.8rem);
	}
	.sr {
		position: absolute;
		width: 1px;
		height: 1px;
		overflow: hidden;
		clip: rect(0 0 0 0);
	}
</style>
