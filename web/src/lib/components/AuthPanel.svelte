<script lang="ts">
	import { Stack } from '@codeatlasdev/ui';
	import { t } from '#lib/i18n.svelte.ts';
</script>

<Stack gap="6">
	<h2>{t('auth.panel.title')}</h2>
	<p>{t('auth.panel.body')}</p>
	<ul>
		<li>{t('auth.panel.point1')}</li>
		<li>{t('auth.panel.point2')}</li>
		<li>{t('auth.panel.point3')}</li>
	</ul>
</Stack>

<style>
	h2 {
		margin: 0;
		color: var(--panel-text-primary);
		font-size: clamp(2rem, 3.4vw, 3rem);
		line-height: 1.05;
		font-weight: 750;
		letter-spacing: -0.035em;
		white-space: pre-line;
	}
	p {
		margin: 0;
		max-width: 34ch;
		color: var(--panel-text-secondary);
		font-size: 1.05rem;
	}
	ul {
		margin: 0;
		padding: 0;
		list-style: none;
		display: grid;
		gap: var(--ca-space-2);
		font-family: var(--ca-font-mono);
		font-size: 0.875rem;
		color: var(--panel-text-primary);
	}
	li::before {
		content: '';
		display: inline-block;
		width: 8px;
		height: 8px;
		margin-right: var(--ca-space-2);
		background: var(--lx-signal);
	}
</style>
