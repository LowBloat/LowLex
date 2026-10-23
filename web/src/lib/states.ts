import type { Translation } from './api.ts';

/** StatusChip colour for a translation state. */
export function stateStatus(tr: Translation | undefined): 'success' | 'warning' | 'neutral' | 'info' {
	if (!tr) return 'neutral';
	if (tr.state === 'reviewed') return 'success';
	if (tr.state === 'draft') return 'warning';
	return 'info';
}

/** "ai:gpt-4o-mini" → "AI · gpt-4o-mini", "mt:deepl" → "MT · deepl". */
export function originLabel(origin: string): string {
	const [kind, detail] = origin.split(':');
	if (kind === 'ai') return 'AI · ' + detail;
	if (kind === 'mt') return 'MT · ' + detail;
	return origin;
}
