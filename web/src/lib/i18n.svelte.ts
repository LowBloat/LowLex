import { setLocale } from '@codeatlasdev/ui';
import en from './locales/en.json';
import ptBR from './locales/pt-BR.json';

// The UI's own strings are flat JSON files, the same shape LowLex exports: they can be managed in LowLex itself.
const catalogs: Record<string, Record<string, string>> = { en, 'pt-BR': ptBR };
export const LOCALES = [
	{ value: 'en', label: 'English' },
	{ value: 'pt-BR', label: 'Português (Brasil)' }
];
const STORAGE = 'lowlex-locale';

function detect(): string {
	try {
		const saved = localStorage.getItem(STORAGE);
		if (saved && catalogs[saved]) return saved;
	} catch {
		/* storage blocked */
	}
	const nav = typeof navigator !== 'undefined' ? navigator.language : 'en';
	return nav.toLowerCase().startsWith('pt') ? 'pt-BR' : 'en';
}

let current = $state(detect());

export const i18n = {
	get locale() {
		return current;
	},
	set(next: string) {
		if (!catalogs[next]) return;
		current = next;
		setLocale(next);
		document.documentElement.lang = next;
		try {
			localStorage.setItem(STORAGE, next);
		} catch {
			/* storage blocked */
		}
	},
	init() {
		setLocale(current);
		document.documentElement.lang = current;
	}
};

/** Translate a key; {name} placeholders are filled from vars. */
export function t(key: string, vars?: Record<string, string | number>): string {
	let s = catalogs[current]?.[key] ?? en[key as keyof typeof en] ?? key;
	if (vars) for (const [k, v] of Object.entries(vars)) s = s.replaceAll(`{${k}}`, String(v));
	return s;
}

export function formatDate(iso: string | null | undefined): string {
	if (!iso) return '—';
	return new Date(iso).toLocaleString(current, { dateStyle: 'medium', timeStyle: 'short' });
}

export function relativeTime(iso: string | null | undefined): string {
	if (!iso) return '—';
	const diff = (Date.parse(iso) - Date.now()) / 1000;
	const rtf = new Intl.RelativeTimeFormat(current, { numeric: 'auto' });
	const steps: [number, Intl.RelativeTimeFormatUnit][] = [
		[60, 'second'],
		[3600, 'minute'],
		[86400, 'hour'],
		[604800, 'day'],
		[2629800, 'week'],
		[31557600, 'month'],
		[Infinity, 'year']
	];
	const divisors = [1, 60, 3600, 86400, 604800, 2629800, 31557600];
	for (let i = 0; i < steps.length; i++) {
		if (Math.abs(diff) < steps[i][0]) return rtf.format(Math.round(diff / divisors[i]), steps[i][1]);
	}
	return formatDate(iso);
}

export function num(n: number): string {
	return n.toLocaleString(current);
}

/** Like t, but returns fallback when the key has no translation (server-provided labels). */
export function tOr(key: string, fallback: string): string {
	const s = t(key);
	return s === key ? fallback : s;
}
