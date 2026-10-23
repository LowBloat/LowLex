import type { ThemeOptions } from '@codeatlasdev/ui/vite';

/**
 * LowBloat identity: paper and ink neutrals, one lime signal, square corners, no blur,
 * no decorative shadows and no entrance animation. Shared by vite.config and hooks.server.
 */
export const theme: ThemeOptions = {
	presets: [
		{
			id: 'lowbloat',
			label: 'LowBloat',
			color: '#B8F238',
			colors: {
				light: {
					background: '#F1F3EE',
					sunken: '#E8EBE3',
					surface: '#FBFCF8',
					overlay: '#FBFCF8',
					text: '#171A17',
					textSecondary: '#4F584F',
					borderSubtle: '#D4D9D0',
					border: '#D4D9D0',
					borderStrong: '#A9B1A5'
				},
				dark: {
					background: '#121412',
					sunken: '#0D0F0D',
					surface: '#191C19',
					overlay: '#1E221E',
					text: '#ECEFE7',
					textSecondary: '#AEB7AA',
					borderSubtle: '#2B302A',
					border: '#353B34',
					borderStrong: '#4F584F'
				}
			}
		}
	],
	defaultPreset: 'lowbloat',
	defaultScheme: 'system',
	defaultStyle: 'flat',
	fonts: {
		body: '"Inter Variable", Inter, system-ui, sans-serif',
		display: '"Inter Variable", Inter, system-ui, sans-serif',
		mono: '"Cascadia Mono", "SFMono-Regular", Consolas, ui-monospace, monospace'
	},
	radius: 0,
	controlRadius: 'xs',
	elevation: 'none',
	typography: { weights: { heading: 700 }, headingTracking: '-0.02em' },
	motion: { enter: 'none' },
	overrides: {
		// ca-ui-workaround(#410): the light scheme derives brand text from the lime signal at about 1.6:1
		// on paper; LowBloat uses its dark lime (the site's focus ring) for text and links instead.
		light: { '--ca-brand-text': '#4F7400', '--ca-brand-dark': '#4F7400' }
	}
};
