<script lang="ts">
	import logo from '#lib/assets/lowlex.svg';
	let { size = 28, name = true }: { size?: number; name?: boolean } = $props();
</script>

<span class="brand">
	<img src={logo} alt="" width={size} height={size} />
	{#if name}<span class="name">LowLex</span>{/if}
</span>

<style>
	.brand {
		display: inline-flex;
		align-items: center;
		gap: var(--ca-space-2);
		color: var(--ca-text-primary);
	}
	.name {
		font-weight: 750;
		letter-spacing: -0.02em;
		font-size: 1.05rem;
	}
	img {
		display: block;
	}
</style>
