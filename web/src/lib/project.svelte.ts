import { getContext, setContext } from 'svelte';
import { api, type ProjectDetail } from './api.ts';

/** The open project, shared by the project layout and its pages. */
export class ProjectState {
	detail = $state<ProjectDetail | null>(null);
	error = $state('');

	constructor(public slug: string) {}

	get project() {
		return this.detail!.project;
	}
	get can() {
		return this.detail!.permissions;
	}
	get base() {
		return this.detail!.project.base_language;
	}
	get path() {
		return '/p/' + encodeURIComponent(this.slug);
	}
	/** API path under this project. */
	api(sub = '') {
		return '/projects/' + encodeURIComponent(this.slug) + sub;
	}
	canEdit(lang: string) {
		return this.detail?.editable_languages.includes(lang) ?? false;
	}

	async load() {
		try {
			this.detail = await api.get<ProjectDetail>(this.api());
			this.error = '';
		} catch (e) {
			this.error = (e as Error).message;
		}
	}
}

const KEY = Symbol('project');

export function provideProject(p: ProjectState) {
	setContext(KEY, p);
}

export function useProject(): ProjectState {
	return getContext(KEY);
}
