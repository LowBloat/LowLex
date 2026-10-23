import { api, type Job } from './api.ts';

export const isActive = (j: Job) => j.status === 'queued' || j.status === 'running';

export function percent(j: Job): number {
	if (j.status === 'done') return 100;
	return j.total > 0 ? Math.min(100, Math.floor((j.done / j.total) * 100)) : 0;
}

/**
 * Keeps a list of jobs fresh: polls every `ms` while any job is queued or running,
 * every 15 s otherwise. Returns a stop function.
 */
export function watchJobs(query: string, onUpdate: (jobs: Job[]) => void, ms = 1500): () => void {
	let stopped = false;
	let timer: ReturnType<typeof setTimeout>;
	async function tick() {
		let next = 15000;
		try {
			const res = await api.get<{ jobs: Job[] }>('/jobs' + query);
			if (stopped) return;
			onUpdate(res.jobs);
			if (res.jobs.some(isActive)) next = ms;
		} catch {
			next = 5000;
		}
		if (!stopped) timer = setTimeout(tick, next);
	}
	tick();
	return () => {
		stopped = true;
		clearTimeout(timer);
	};
}

/** Polls one job until it finishes. */
export function watchJob(id: number, onUpdate: (job: Job) => void, ms = 1000): () => void {
	let stopped = false;
	let timer: ReturnType<typeof setTimeout>;
	async function tick() {
		try {
			const res = await api.get<{ job: Job }>('/jobs/' + id);
			if (stopped) return;
			onUpdate(res.job);
			if (!isActive(res.job)) return;
		} catch {
			/* retry */
		}
		if (!stopped) timer = setTimeout(tick, ms);
	}
	tick();
	return () => {
		stopped = true;
		clearTimeout(timer);
	};
}

/** Rough time left from the rate since the job started. */
export function eta(j: Job): number | null {
	if (j.status !== 'running' || !j.started_at || j.done <= 0 || j.total <= j.done) return null;
	const elapsed = (Date.now() - Date.parse(j.started_at)) / 1000;
	if (elapsed < 3) return null;
	return Math.round(((j.total - j.done) * elapsed) / j.done);
}

export function duration(seconds: number): string {
	if (seconds < 60) return `${seconds}s`;
	const m = Math.floor(seconds / 60);
	if (m < 60) return `${m} min ${seconds % 60}s`;
	return `${Math.floor(m / 60)} h ${m % 60} min`;
}

/** Seconds the job is waiting for a remote rate limit, from its status line. */
export function rateLimitWait(j: Job): number | null {
	const m = /^rate_limited:(\d+)$/.exec(j.message);
	return m && j.status === 'running' ? Number(m[1]) : null;
}
