/** Rough strength from 0 to 4: length and character variety. The server enforces the real minimum. */
export function passwordScore(pw: string): number {
	if (pw.length < 10) return pw.length ? 1 : 0;
	let variety = 0;
	if (/[a-z]/.test(pw)) variety++;
	if (/[A-Z]/.test(pw)) variety++;
	if (/[0-9]/.test(pw)) variety++;
	if (/[^A-Za-z0-9]/.test(pw)) variety++;
	if (pw.length >= 16 && variety >= 3) return 4;
	if (pw.length >= 12 && variety >= 2) return 3;
	return 2;
}
