import { goto } from '$app/navigation';

export class ApiError extends Error {
	constructor(
		public status: number,
		public code: string,
		message: string
	) {
		super(message);
	}
}

type Body = Record<string, unknown> | unknown[] | FormData | undefined;

async function request<T>(method: string, path: string, body?: Body): Promise<T> {
	const init: RequestInit = { method, credentials: 'same-origin', headers: {} };
	if (body instanceof FormData) {
		init.body = body;
	} else if (body !== undefined) {
		init.body = JSON.stringify(body);
		(init.headers as Record<string, string>)['Content-Type'] = 'application/json';
	}
	const res = await fetch('/api' + path, init);
	const data = res.headers.get('content-type')?.includes('application/json') ? await res.json() : null;
	if (!res.ok) {
		const err = new ApiError(res.status, data?.code ?? 'error', data?.error ?? res.statusText);
		if (res.status === 401 && !path.startsWith('/auth/login')) {
			goto('/login?next=' + encodeURIComponent(location.pathname + location.search));
		} else if (err.code === 'password_change_required') {
			goto('/password');
		}
		throw err;
	}
	return data as T;
}

export const api = {
	get: <T>(path: string) => request<T>('GET', path),
	post: <T>(path: string, body?: Body) => request<T>('POST', path, body ?? {}),
	put: <T>(path: string, body?: Body) => request<T>('PUT', path, body ?? {}),
	patch: <T>(path: string, body?: Body) => request<T>('PATCH', path, body ?? {}),
	del: <T>(path: string) => request<T>('DELETE', path)
};

export function errorText(e: unknown): string {
	return e instanceof Error ? e.message : String(e);
}

// --- shapes returned by the API ---

export type GlobalRole = 'admin' | 'user';
export type ProjectRole = 'manager' | 'developer' | 'reviewer' | 'translator' | 'viewer';
export type State = 'draft' | 'translated' | 'reviewed';

export interface User {
	id: number;
	username: string;
	email: string;
	name: string;
	role: GlobalRole;
	must_change_password: boolean;
	disabled: boolean;
	created_at: string;
	last_login_at: string | null;
	projects?: number;
}

export interface LangStats {
	code: string;
	name: string;
	translated: number;
	reviewed: number;
	draft: number;
}

export interface Project {
	id: number;
	slug: string;
	name: string;
	description: string;
	base_language: string;
	created_at: string;
}

export interface ProjectSummary extends Project {
	role: ProjectRole | 'admin';
	keys: number;
	languages: LangStats[];
}

export interface ProjectDetail {
	project: Project;
	role: ProjectRole | 'admin';
	keys: number;
	languages: LangStats[];
	editable_languages: string[];
	permissions: {
		translate: boolean;
		review: boolean;
		manage_keys: boolean;
		manage_project: boolean;
		delete_project: boolean;
	};
}

export interface Translation {
	text: string;
	state: State;
	origin: string;
	updated_at: string;
	updated_by?: string;
}

export interface Key {
	id: number;
	namespace: string;
	name: string;
	description: string;
	tags: string[];
	created_at: string;
	updated_at: string;
	translations: Record<string, Translation>;
}

export interface Member {
	user_id: number;
	username: string;
	name: string;
	email: string;
	role: ProjectRole;
	languages: string[];
}

export interface ProviderField {
	name: string;
	label: string;
	secret?: boolean;
	required?: boolean;
	default?: string;
	multiline?: boolean;
}

export interface ProviderType {
	type: string;
	label: string;
	ai: boolean;
	fields: ProviderField[];
}

export interface Provider {
	id: number;
	type: string;
	name: string;
	enabled: boolean;
	ai: boolean;
	created_at: string;
	config?: {
		base_url: string;
		region: string;
		model: string;
		prompt: string;
		has_api_key: boolean;
	};
}

export interface Suggestion {
	provider_id: number;
	provider: string;
	type: string;
	ai: boolean;
	text?: string;
	error?: string;
	ms: number;
}

export interface ApiKey {
	id: number;
	name: string;
	prefix: string;
	scope: 'read' | 'write';
	created_at: string;
	last_used_at: string | null;
	created_by: string;
}

export interface Activity {
	id: number;
	action: string;
	detail: string;
	created_at: string;
	username: string;
	name: string;
}

export interface Meta {
	version: string;
	languages: Record<string, string>;
	formats: { id: string; label: string; ext: string }[];
}
