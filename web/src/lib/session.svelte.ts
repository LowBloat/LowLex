import { api, type Meta, type ProjectSummary, type User } from './api.ts';
import { i18n } from './i18n.svelte.ts';

let user = $state<User | null>(null);
let meta = $state<Meta | null>(null);
let projects = $state<ProjectSummary[]>([]);

export const session = {
	get user() {
		return user;
	},
	get isAdmin() {
		return user?.role === 'admin';
	},
	get meta() {
		return meta;
	},
	set(u: User | null) {
		user = u;
	},
	/** Loads the signed-in user; returns null when signed out. */
	async load(): Promise<User | null> {
		try {
			const res = await fetch('/api/auth/me', { credentials: 'same-origin' });
			user = res.ok ? (await res.json()).user : null;
		} catch {
			user = null;
		}
		return user;
	},
	/** Projects the user can open, for the navigation panel and the projects page. */
	get projects() {
		return projects;
	},
	async loadProjects(): Promise<ProjectSummary[]> {
		projects = (await api.get<{ projects: ProjectSummary[] }>('/projects')).projects;
		return projects;
	},
	async loadMeta(): Promise<Meta> {
		if (!meta) meta = await api.get<Meta>('/meta');
		return meta;
	},
	async logout() {
		await api.post('/auth/logout').catch(() => {});
		user = null;
		projects = [];
	}
};

const displayNames = new Map<string, Intl.DisplayNames>();

/** "pt-BR" → "português (Brasil)" in the interface language, falling back to the server's English list. */
export function langName(code: string): string {
	const ui = i18n.locale;
	try {
		let dn = displayNames.get(ui);
		if (!dn) displayNames.set(ui, (dn = new Intl.DisplayNames([ui], { type: 'language', fallback: 'none' })));
		const name = dn.of(code);
		if (name) return name.charAt(0).toLocaleUpperCase(ui) + name.slice(1);
	} catch {
		/* not a valid tag for Intl */
	}
	return meta?.languages[code] ?? code;
}

/** Options for language pickers, sorted by name. */
export function languageOptions(exclude: string[] = []) {
	const all = meta?.languages ?? {};
	return Object.entries(all)
		.filter(([code]) => !exclude.includes(code))
		.map(([code]) => ({ value: code, label: `${langName(code)} (${code})` }))
		.sort((a, b) => a.label.localeCompare(b.label));
}
