<script lang="ts">
	import { AuthLayout, AuthCard, TextField, Button, Alert, Stack, Progress } from '@codeatlasdev/ui';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { api, type User } from '#lib/api.ts';
	import { session } from '#lib/session.svelte.ts';
	import { t } from '#lib/i18n.svelte.ts';
	import { passwordScore } from '#lib/password.ts';
	import Brand from '#lib/components/Brand.svelte';
	import AuthPanel from '#lib/components/AuthPanel.svelte';

	let current = $state('');
	let next = $state('');
	let confirm = $state('');
	let error = $state('');
	let busy = $state(false);
	const score = $derived(passwordScore(next));
	const mismatch = $derived(confirm.length > 0 && confirm !== next);

	onMount(async () => {
		const u = session.user ?? (await session.load());
		if (!u) goto('/login', { replace: true });
	});

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (next !== confirm) return;
		error = '';
		busy = true;
		try {
			const res = await api.post<{ user: User }>('/auth/password', { current_password: current, new_password: next });
			session.set(res.user);
			goto('/projects', { replace: true });
		} catch (err) {
			error = (err as Error).message;
		} finally {
			busy = false;
		}
	}
</script>

<AuthLayout logoName="LowLex" logoHref="/login">
	{#snippet brand()}<Brand name={false} />{/snippet}
	{#snippet panel()}<AuthPanel />{/snippet}
	<AuthCard
		title={t('password.title')}
		subtitle={session.user?.must_change_password ? t('password.required') : t('password.subtitle')}
	>
		<Stack as="form" gap="4" onsubmit={submit} novalidate>
			{#if error}<Alert severity="error">{error}</Alert>{/if}
			<TextField label={t('password.current')} type="password" bind:value={current} autocomplete="current-password" required />
			<Stack gap="2">
				<TextField
					label={t('password.new')}
					type="password"
					bind:value={next}
					autocomplete="new-password"
					required
					helperText={t('password.hint')}
				/>
				{#if next}<Progress value={score * 25} label={t('password.strength.' + score)} />{/if}
			</Stack>
			<TextField
				label={t('password.confirm')}
				type="password"
				bind:value={confirm}
				autocomplete="new-password"
				required
				error={mismatch ? t('password.mismatch') : undefined}
			/>
			<Button type="submit" variant="filled" fullWidth loading={busy} disabled={!current || next.length < 10 || next !== confirm}>
				{t('password.submit')}
			</Button>
		</Stack>
		{#snippet footer()}
			<Button variant="ghost" size="sm" onclick={async () => { await session.logout(); goto('/login'); }}>{t('nav.signOut')}</Button>
		{/snippet}
	</AuthCard>
</AuthLayout>
