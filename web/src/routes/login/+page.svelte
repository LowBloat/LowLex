<script lang="ts">
	import { AuthLayout, AuthCard, TextField, Button, Alert, Stack } from '@codeatlasdev/ui';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import { api, ApiError, type User } from '#lib/api.ts';
	import { session } from '#lib/session.svelte.ts';
	import { t } from '#lib/i18n.svelte.ts';
	import Brand from '#lib/components/Brand.svelte';
	import AuthPanel from '#lib/components/AuthPanel.svelte';

	let username = $state('');
	let password = $state('');
	let error = $state('');
	let busy = $state(false);

	function next() {
		const n = page.url.searchParams.get('next');
		return n && n.startsWith('/') && !n.startsWith('//') ? n : '/projects';
	}

	onMount(async () => {
		const u = await session.load();
		if (u) goto(u.must_change_password ? '/password' : next(), { replace: true });
	});

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		error = '';
		busy = true;
		try {
			const res = await api.post<{ user: User }>('/auth/login', { username, password });
			session.set(res.user);
			goto(res.user.must_change_password ? '/password' : next(), { replace: true });
		} catch (err) {
			error = err instanceof ApiError && err.code === 'invalid_credentials' ? t('login.invalid') : (err as Error).message;
		} finally {
			busy = false;
		}
	}
</script>

<AuthLayout logoName="LowLex" logoHref="/login">
	{#snippet brand()}<Brand name={false} />{/snippet}
	{#snippet panel()}<AuthPanel />{/snippet}
	<AuthCard title={t('login.title')} subtitle={t('login.subtitle')}>
		<Stack as="form" gap="4" onsubmit={submit} novalidate>
			{#if error}<Alert severity="error">{error}</Alert>{/if}
			<TextField label={t('login.username')} bind:value={username} autocomplete="username" required />
			<TextField label={t('login.password')} type="password" bind:value={password} autocomplete="current-password" required />
			<Button type="submit" variant="filled" fullWidth loading={busy} disabled={!username || !password}>{t('login.submit')}</Button>
		</Stack>
		{#snippet footer()}
			<span class="lx-muted">{t('login.footer')}</span>
		{/snippet}
	</AuthCard>
</AuthLayout>
