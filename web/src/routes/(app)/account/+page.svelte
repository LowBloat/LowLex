<!-- @codeatlasdev/ui demo #184 · MIT License (c) CodeAtlas · https://ui.codeatlas.com.br/docs/layout/list-page#appsettings -->
<script lang="ts">
	import { AppSettings, FormSection, TextField, Button, Select, SettingItem, SegmentedControl, Alert, Stack, Row, Progress, colorScheme, toast } from '@codeatlasdev/ui';
	import { UserRound, KeyRound, SlidersHorizontal } from '@lucide/svelte';
	import { api, type User } from '#lib/api.ts';
	import { session } from '#lib/session.svelte.ts';
	import { t, i18n, LOCALES } from '#lib/i18n.svelte.ts';
	import { passwordScore } from '#lib/password.ts';

	const tabs = $derived([
		{ value: 'profile', label: t('account.profile'), icon: UserRound },
		{ value: 'password', label: t('account.password'), icon: KeyRound },
		{ value: 'preferences', label: t('account.preferences'), icon: SlidersHorizontal }
	]);

	let name = $state(session.user?.name ?? '');
	let email = $state(session.user?.email ?? '');
	let savingProfile = $state(false);

	let current = $state('');
	let next = $state('');
	let confirm = $state('');
	let pwError = $state('');
	let savingPw = $state(false);
	const score = $derived(passwordScore(next));

	let scheme = $state<string>(colorScheme.preference);

	async function saveProfile() {
		savingProfile = true;
		try {
			const res = await api.patch<{ user: User }>('/auth/me', { name, email });
			session.set(res.user);
			toast.success(t('account.saved'));
		} catch (e) {
			toast.error((e as Error).message);
		} finally {
			savingProfile = false;
		}
	}

	async function changePassword() {
		savingPw = true;
		pwError = '';
		try {
			await api.post('/auth/password', { current_password: current, new_password: next });
			current = next = confirm = '';
			toast.success(t('account.passwordChanged'));
		} catch (e) {
			pwError = (e as Error).message;
		} finally {
			savingPw = false;
		}
	}
</script>

<AppSettings title={t('account.title')} description={session.user ? '@' + session.user.username : ''} {tabs}>
	{#snippet children(tab)}
		{#if tab === 'profile'}
			<FormSection title={t('account.profile')} description={t('account.profileHint')}>
				<TextField label={t('users.name')} bind:value={name} autocomplete="name" />
				<TextField label={t('users.email')} type="email" bind:value={email} autocomplete="email" />
			</FormSection>
			<Row gap={8} justify="flex-end">
				<Button variant="filled" loading={savingProfile} onclick={saveProfile}>{t('common.save')}</Button>
			</Row>
		{:else if tab === 'password'}
			<FormSection title={t('account.password')} description={t('password.hint')} layout="stack">
				{#if pwError}<Alert severity="error">{pwError}</Alert>{/if}
				<TextField label={t('password.current')} type="password" bind:value={current} autocomplete="current-password" />
				<Stack gap="2">
					<TextField label={t('password.new')} type="password" bind:value={next} autocomplete="new-password" />
					{#if next}<Progress value={score * 25} label={t('password.strength.' + score)} />{/if}
				</Stack>
				<TextField
					label={t('password.confirm')}
					type="password"
					bind:value={confirm}
					autocomplete="new-password"
					error={confirm && confirm !== next ? t('password.mismatch') : undefined}
				/>
			</FormSection>
			<Row gap={8} justify="flex-end">
				<Button variant="filled" loading={savingPw} disabled={!current || next.length < 10 || next !== confirm} onclick={changePassword}>
					{t('password.submit')}
				</Button>
			</Row>
		{:else}
			<FormSection title={t('account.preferences')} layout="list">
				<SettingItem label={t('account.language')} description={t('account.languageHint')}>
					<Select aria-label={t('account.language')} value={i18n.locale} options={LOCALES} onchange={(v) => i18n.set(v)} size="sm" />
				</SettingItem>
				<SettingItem label={t('account.theme')} description={t('account.themeHint')}>
					<SegmentedControl
						aria-label={t('account.theme')}
						size="sm"
						bind:value={scheme}
						items={[
							{ value: 'system', label: t('account.theme.system') },
							{ value: 'light', label: t('account.theme.light') },
							{ value: 'dark', label: t('account.theme.dark') }
						]}
						onchange={(v) => colorScheme.setPreference(v as 'system' | 'light' | 'dark')}
					/>
				</SettingItem>
			</FormSection>
		{/if}
	{/snippet}
</AppSettings>
