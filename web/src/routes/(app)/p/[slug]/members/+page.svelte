<!-- @codeatlasdev/ui demo #245 · MIT License (c) CodeAtlas · https://ui.codeatlas.com.br/docs/layout/record-list#apps -->
<!-- @codeatlasdev/ui demo #085 · MIT License (c) CodeAtlas · https://ui.codeatlas.com.br/docs/feedback/dialog#form -->
<script lang="ts">
	import {
		RecordList, RecordRow, RecordCell, UserCell, Button, Row, Stack, Chip, Dialog, Combobox, Select,
		IconButton, AlertDialog, Alert, EmptyState, Callout, toast
	} from '@codeatlasdev/ui';
	import { UserPlus, Pencil, Trash2, Users } from '@lucide/svelte';
	import { onMount } from 'svelte';
	import { api, type Member, type ProjectRole, type User } from '#lib/api.ts';
	import { useProject } from '#lib/project.svelte.ts';
	import { langName } from '#lib/session.svelte.ts';
	import { t } from '#lib/i18n.svelte.ts';

	const project = useProject();
	const ROLES: ProjectRole[] = ['manager', 'developer', 'reviewer', 'translator', 'viewer'];

	let members = $state<Member[]>([]);
	let users = $state<User[]>([]);
	let dialogOpen = $state(false);
	let editingId = $state<number | null>(null);
	let userId = $state('');
	let role = $state<string>('translator');
	let langs = $state<string[]>([]);
	let error = $state('');
	let busy = $state(false);
	let removing = $state<Member | null>(null);

	const roleOptions = $derived(ROLES.map((r) => ({ value: r, label: t('project.role.' + r) })));
	const langOptions = $derived(project.detail!.languages.map((l) => ({ value: l.code, label: `${langName(l.code)} (${l.code})` })));
	const candidates = $derived(
		users
			.filter((u) => !u.disabled && u.role !== 'admin' && !members.some((m) => m.user_id === u.id))
			.map((u) => ({ value: String(u.id), label: u.name ? `${u.name} (${u.username})` : u.username }))
	);
	const admins = $derived(users.filter((u) => u.role === 'admin' && !u.disabled));
	const limited = $derived(role === 'translator' || role === 'reviewer');

	async function load() {
		const [m, u] = await Promise.all([
			api.get<{ members: Member[] }>(project.api('/members')),
			api.get<{ users: User[] }>('/users')
		]);
		members = m.members;
		users = u.users;
	}
	onMount(load);

	function openAdd() {
		editingId = null;
		userId = '';
		role = 'translator';
		langs = [];
		error = '';
		dialogOpen = true;
	}

	function openEdit(m: Member) {
		editingId = m.user_id;
		userId = String(m.user_id);
		role = m.role;
		langs = [...m.languages];
		error = '';
		dialogOpen = true;
	}

	async function save() {
		busy = true;
		error = '';
		try {
			await api.put(project.api('/members/' + userId), { role, languages: limited ? langs : [] });
			toast.success(t('members.saved'));
			dialogOpen = false;
			await load();
		} catch (e) {
			error = (e as Error).message;
		} finally {
			busy = false;
		}
	}

	async function remove(m: Member) {
		await api.del(project.api('/members/' + m.user_id));
		toast.success(t('members.removed', { name: m.name || m.username }));
		removing = null;
		await load();
	}
</script>

<Stack gap="4">
	<Row gap={8} align="center" justify="space-between" wrap>
		<p class="lx-muted p">{t('members.intro')}</p>
		<Button variant="filled" size="sm" onclick={openAdd}>
			{#snippet leading()}<UserPlus size={14} />{/snippet}
			{t('members.add')}
		</Button>
	</Row>

	{#if members.length}
		<RecordList
			columns="minmax(0,1.4fr) 8rem minmax(0,1.4fr) 5.5rem"
			labels={[t('members.person'), t('members.role'), t('members.languages')]}
			aria-label={t('project.tab.members')}
		>
			{#each members as m (m.user_id)}
				<RecordRow>
					{#snippet identity()}<UserCell name={m.name || m.username} email={m.email || '@' + m.username} />{/snippet}
					<RecordCell label={t('members.role')}><Chip size="sm" label={t('project.role.' + m.role)} /></RecordCell>
					<RecordCell label={t('members.languages')}>
						{#if (m.role === 'translator' || m.role === 'reviewer') && m.languages.length}
							<span class="lx-mono">{m.languages.join(', ')}</span>
						{:else}
							<span class="lx-muted">{t('members.allLanguages')}</span>
						{/if}
					</RecordCell>
					{#snippet actions()}
						<Row gap={2}>
							<IconButton variant="text" size="sm" aria-label={t('members.editFor', { name: m.username })} onclick={() => openEdit(m)}><Pencil size={16} /></IconButton>
							<IconButton variant="text" size="sm" aria-label={t('members.removeFor', { name: m.username })} onclick={() => (removing = m)}><Trash2 size={16} /></IconButton>
						</Row>
					{/snippet}
				</RecordRow>
			{/each}
		</RecordList>
	{:else}
		<EmptyState icon={Users} title={t('members.empty.title')} description={t('members.empty.body')} />
	{/if}

	{#if admins.length}
		<p class="lx-muted p small">{t('members.admins', { list: admins.map((a) => a.username).join(', ') })}</p>
	{/if}
</Stack>

<Dialog bind:open={dialogOpen} title={editingId ? t('members.edit') : t('members.add')} size="md">
	<Stack gap="4">
		{#if error}<Alert severity="error">{error}</Alert>{/if}
		{#if editingId}
			<p class="p"><strong>{users.find((u) => u.id === editingId)?.username}</strong></p>
		{:else if candidates.length}
			<Combobox label={t('members.person')} bind:value={userId} options={candidates} searchPlaceholder={t('common.search')} />
		{:else}
			<Callout type="note">{t('members.noCandidates')}</Callout>
		{/if}
		<Select label={t('members.role')} bind:value={role} options={roleOptions} description={t('members.roleHelp.' + role)} />
		{#if limited}
			<Combobox
				label={t('members.languages')}
				multiple
				bind:value={langs}
				options={langOptions}
				placeholder={t('members.allLanguages')}
				description={t('members.languagesHint')}
			/>
		{/if}
	</Stack>
	{#snippet actions()}
		<Button variant="ghost" onclick={() => (dialogOpen = false)}>{t('common.cancel')}</Button>
		<Button variant="filled" loading={busy} disabled={!userId} onclick={save}>{t('common.save')}</Button>
	{/snippet}
</Dialog>

<AlertDialog
	open={removing !== null}
	title={t('members.removeTitle', { name: removing?.username ?? '' })}
	description={t('members.removeBody')}
	confirmLabel={t('members.remove')}
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
</style>
