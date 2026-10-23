<!-- @codeatlasdev/ui demo #183 · MIT License (c) CodeAtlas · https://ui.codeatlas.com.br/docs/layout/list-page#applist -->
<!-- @codeatlasdev/ui demo #085 · MIT License (c) CodeAtlas · https://ui.codeatlas.com.br/docs/feedback/dialog#form -->
<script lang="ts">
	import {
		AppList, SimpleTable, UserCell, StatusChip, Chip, FilterGroup, Button, Row, Dialog, TextField, Select,
		Switch, Stack, Alert, IconButton, AlertDialog, CopyButton, toast
	} from '@codeatlasdev/ui';
	import { Plus, Pencil, Trash2, Dices } from '@lucide/svelte';
	import { onMount } from 'svelte';
	import { api, type User } from '#lib/api.ts';
	import { session } from '#lib/session.svelte.ts';
	import { t, relativeTime } from '#lib/i18n.svelte.ts';

	let users = $state<User[]>([]);
	let open = $state(false);
	let editing = $state<User | null>(null);
	let removing = $state<User | null>(null);

	let username = $state('');
	let name = $state('');
	let email = $state('');
	let role = $state('user');
	let password = $state('');
	let mustChange = $state(true);
	let disabled = $state(false);
	let error = $state('');
	let busy = $state(false);

	async function load() {
		users = (await api.get<{ users: User[] }>('/users')).users;
	}
	onMount(load);

	function generate() {
		const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789-_.';
		const bytes = crypto.getRandomValues(new Uint8Array(20));
		password = Array.from(bytes, (b) => chars[b % chars.length]).join('');
	}

	function openNew() {
		editing = null;
		username = name = email = password = '';
		role = 'user';
		mustChange = true;
		disabled = false;
		error = '';
		open = true;
	}

	function openEdit(u: User) {
		editing = u;
		username = u.username;
		name = u.name;
		email = u.email;
		role = u.role;
		password = '';
		mustChange = u.must_change_password;
		disabled = u.disabled;
		error = '';
		open = true;
	}

	async function save() {
		busy = true;
		error = '';
		try {
			if (editing) {
				const body: Record<string, unknown> = { name, email, role, disabled, must_change_password: mustChange };
				if (password) body.password = password;
				await api.patch('/users/' + editing.id, body);
				toast.success(t('users.saved'));
			} else {
				await api.post('/users', { username, name, email, role, password, must_change_password: mustChange });
				toast.success(t('users.created', { name: username }));
			}
			open = false;
			await load();
		} catch (e) {
			error = (e as Error).message;
		} finally {
			busy = false;
		}
	}

	async function remove(u: User) {
		try {
			await api.del('/users/' + u.id);
			toast.success(t('users.deleted', { name: u.username }));
			await load();
		} catch (e) {
			toast.error((e as Error).message);
		}
		removing = null;
	}

	const roleFilter = $derived([
		{ value: 'admin', label: t('users.role.admin') },
		{ value: 'user', label: t('users.role.user') }
	]);
	const rows = $derived(users.map((u) => ({ ...u, display: u.name || u.username })));
</script>

<AppList title={t('users.title')} data={rows} searchKeys={['username', 'name', 'email']} perPage={20} searchPlaceholder={t('common.search')}>
	{#snippet actions()}
		<Button variant="filled" size="sm" onclick={openNew}>
			{#snippet leading()}<Plus size={14} />{/snippet}
			{t('users.new')}
		</Button>
	{/snippet}
	{#snippet filters(list)}
		<FilterGroup options={roleFilter} value={list.filters['role'] ?? 'all'} onchange={(v) => list.setFilter('role', v)} />
	{/snippet}
	{#snippet children(list)}
		<SimpleTable responsive="cards">
			<thead>
				<tr>
					<th>{t('users.user')}</th>
					<th>{t('users.roleLabel')}</th>
					<th>{t('users.status')}</th>
					<th>{t('users.projects')}</th>
					<th>{t('users.lastLogin')}</th>
					<th><span class="ca-sr-only">{t('common.actions')}</span></th>
				</tr>
			</thead>
			<tbody>
				{#each list.rows as u (u.id)}
					<tr>
						<td><UserCell name={u.display} email={u.email || '@' + u.username} /></td>
						<td><Chip size="sm" variant={u.role === 'admin' ? 'brand' : 'default'} label={t('users.role.' + u.role)} /></td>
						<td>
							{#if u.disabled}
								<StatusChip status="neutral" label={t('users.disabled')} />
							{:else if u.must_change_password}
								<StatusChip status="warning" label={t('users.mustChange')} />
							{:else}
								<StatusChip status="success" label={t('users.active')} />
							{/if}
						</td>
						<td>{u.projects ?? 0}</td>
						<td>{u.last_login_at ? relativeTime(u.last_login_at) : t('users.never')}</td>
						<td>
							<Row gap={2}>
								<IconButton variant="text" size="sm" aria-label={t('users.editFor', { name: u.username })} onclick={() => openEdit(u as User)}><Pencil size={16} /></IconButton>
								{#if u.id !== session.user?.id}
									<IconButton variant="text" size="sm" aria-label={t('users.deleteFor', { name: u.username })} onclick={() => (removing = u as User)}><Trash2 size={16} /></IconButton>
								{/if}
							</Row>
						</td>
					</tr>
				{/each}
			</tbody>
		</SimpleTable>
	{/snippet}
</AppList>

<Dialog bind:open title={editing ? t('users.edit', { name: editing.username }) : t('users.new')} size="md">
	<Stack gap="4">
		{#if error}<Alert severity="error">{error}</Alert>{/if}
		{#if !editing}
			<TextField label={t('users.username')} bind:value={username} autocomplete="off" required helperText={t('users.usernameHint')} />
		{/if}
		<TextField label={t('users.name')} bind:value={name} />
		<TextField label={t('users.email')} type="email" bind:value={email} />
		<Select
			label={t('users.roleLabel')}
			bind:value={role}
			options={[
				{ value: 'user', label: t('users.role.user') },
				{ value: 'admin', label: t('users.role.admin') }
			]}
			description={t('users.roleHelp.' + role)}
		/>
		<Stack gap="2">
			<TextField
				label={editing ? t('users.resetPassword') : t('users.password')}
				bind:value={password}
				autocomplete="new-password"
				helperText={editing ? t('users.resetHint') : t('users.passwordHint')}
			>
				{#snippet trailing()}
					<IconButton variant="text" size="sm" aria-label={t('users.generate')} onclick={generate}><Dices size={16} /></IconButton>
				{/snippet}
			</TextField>
			{#if password}
				<Row gap={8} align="center">
					<CopyButton value={password} label={t('users.copyPassword')} size="sm" toast={t('common.copied')} />
				</Row>
			{/if}
		</Stack>
		<Switch label={t('users.mustChangeLabel')} bind:checked={mustChange} />
		{#if editing && editing.id !== session.user?.id}
			<Switch label={t('users.disableLabel')} bind:checked={disabled} />
		{/if}
	</Stack>
	{#snippet actions()}
		<Button variant="ghost" onclick={() => (open = false)}>{t('common.cancel')}</Button>
		<Button variant="filled" loading={busy} disabled={!editing && (!username || password.length < 10)} onclick={save}>
			{editing ? t('common.save') : t('users.create')}
		</Button>
	{/snippet}
</Dialog>

<AlertDialog
	open={removing !== null}
	title={t('users.deleteTitle', { name: removing?.username ?? '' })}
	description={t('users.deleteBody')}
	confirmLabel={t('common.delete')}
	destructive
	onconfirm={() => remove(removing!)}
	oncancel={() => (removing = null)}
/>
