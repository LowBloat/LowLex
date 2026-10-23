<!-- @codeatlasdev/ui demo #166 · MIT License (c) CodeAtlas · https://ui.codeatlas.com.br/docs/layout/app-shell#live-shell -->
<script lang="ts">
	import { AppShellLayout, Avatar, ThemeToggle, IconButton, Spinner, type NavModule } from '@codeatlasdev/ui';
	import { FolderKanban, LayoutGrid, Languages, Shield, Users, Sparkles, UserRound, LogOut, ListChecks } from '@lucide/svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { session } from '#lib/session.svelte.ts';
	import { t } from '#lib/i18n.svelte.ts';
	import Brand from '#lib/components/Brand.svelte';

	let { children } = $props();
	let ready = $state(false);

	onMount(async () => {
		const u = await session.load();
		if (!u) {
			goto('/login?next=' + encodeURIComponent(page.url.pathname + page.url.search), { replace: true });
			return;
		}
		if (u.must_change_password) {
			goto('/password', { replace: true });
			return;
		}
		await Promise.all([session.loadMeta(), session.loadProjects()]);
		ready = true;
	});

	const modules: NavModule[] = $derived.by(() => {
		const list: NavModule[] = [
			{
				id: 'projects',
				label: t('nav.projects'),
				icon: FolderKanban,
				basePath: '/projects',
				// ca-ui-workaround(#408): a nav item stays active on every path under it, so "All projects"
				// at /projects would light up beside each project; project pages live under /p instead.
				pathPrefixes: ['/p', '/jobs'],
				items: [
					{ id: 'all-projects', label: t('nav.allProjects'), icon: LayoutGrid, path: '/projects' },
					{ id: 'jobs', label: t('nav.jobs'), icon: ListChecks, path: '/jobs' },
					...(session.projects.length
						? [
								{
									label: t('nav.yourProjects'),
									items: session.projects.map((p) => ({
										id: 'project-' + p.slug,
										label: p.name,
										icon: Languages,
										path: '/p/' + p.slug,
										count: p.keys
									}))
								}
							]
						: [])
				]
			}
		];
		if (session.isAdmin) {
			list.push({
				id: 'admin',
				label: t('nav.admin'),
				icon: Shield,
				basePath: '/admin',
				items: [
					{ id: 'users', label: t('nav.users'), icon: Users, path: '/admin/users' },
					{ id: 'providers', label: t('nav.providers'), icon: Sparkles, path: '/admin/providers' }
				]
			});
		}
		list.push({ id: 'account', label: t('nav.account'), icon: UserRound, basePath: '/account', items: [] });
		return list;
	});

	async function signOut() {
		await session.logout();
		goto('/login');
	}
</script>

{#if ready}
	<AppShellLayout {modules} pathname={page.url.pathname} onNavigate={(path) => goto(path)}>
		{#snippet brand()}
			<a href="/projects" aria-label="LowLex"><Brand name={false} /></a>
		{/snippet}
		{#snippet railBottom()}
			<ThemeToggle label={t('nav.theme')} />
			<IconButton variant="text" aria-label={t('nav.signOut')} onclick={signOut}><LogOut size={18} /></IconButton>
			<a href="/account" class="ca-rail-avatar" aria-label={t('nav.account')}>
				<Avatar alt={session.user?.name || session.user?.username} size="sm" />
			</a>
		{/snippet}
		{#snippet mobileHeaderActions()}
			<ThemeToggle label={t('nav.theme')} />
			<IconButton variant="text" aria-label={t('nav.signOut')} onclick={signOut}><LogOut size={18} /></IconButton>
		{/snippet}
		{@render children()}
	</AppShellLayout>
{:else}
	<div class="boot" aria-busy="true"><Spinner label={t('common.loading')} /></div>
{/if}

<style>
	.boot {
		display: grid;
		place-items: center;
		height: var(--ca-vh, 100vh);
	}
</style>
