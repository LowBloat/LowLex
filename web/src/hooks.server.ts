import { sequence } from "@sveltejs/kit/hooks";
import { caThemeHandle, createErrorHandler } from "@codeatlasdev/ui/kit";
import { theme } from "#lib/theme.ts";

// Puts the theme init script in every SSR/prerendered page so the first paint is already correct.
export const handle = sequence(caThemeHandle(theme));

// Sanitizes unexpected server errors (no stack or 5xx message reaches the user) and gives each an id.
export const handleError = createErrorHandler();
