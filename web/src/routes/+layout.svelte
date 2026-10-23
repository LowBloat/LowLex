<script lang="ts">
	import '@fontsource-variable/inter';
	import '@codeatlasdev/ui/styles/global.css';
	import '#lib/app.css';
	import { colorScheme, Toaster } from '@codeatlasdev/ui';
	import { Seo } from '@codeatlasdev/ui/seo';
	import { onMount } from 'svelte';
	import { i18n } from '#lib/i18n.svelte.ts';
	import type { LayoutProps } from './$types';

	let { children }: LayoutProps = $props();

	onMount(() => {
		i18n.init();
		return colorScheme.init();
	});
</script>

<Seo title="LowLex" noindex lang={i18n.locale} />

{@render children()}

<Toaster />
