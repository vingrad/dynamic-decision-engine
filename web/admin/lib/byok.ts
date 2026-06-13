// Bring-your-own-key (BYOK) credential store for the public demo.
//
// The engine runs with DDE_PLANNER=byok: plan generation uses the API key the
// visitor supplies here, sent per-request as X-LLM-Provider / X-LLM-Key headers.
// The key is held only in sessionStorage (memory for the tab, cleared when it
// closes) and is never persisted by the frontend. It IS sent to the engine to
// make the LLM call — surface that to the user wherever the key is collected.

const STORAGE_KEY = "dde.byok";

export type LLMProvider = "anthropic" | "openai" | "deepseek";

export const LLM_PROVIDERS: { id: LLMProvider; label: string }[] = [
  { id: "anthropic", label: "Anthropic" },
  { id: "openai", label: "OpenAI" },
  { id: "deepseek", label: "DeepSeek" },
];

export interface LLMCreds {
  provider: LLMProvider;
  key: string;
}

// isProvider narrows an arbitrary value to a known LLMProvider.
function isProvider(v: unknown): v is LLMProvider {
  return LLM_PROVIDERS.some((p) => p.id === v);
}

// getCreds returns the stored credentials, or null when none/empty/off-browser, or
// when the stored provider is not one we support (e.g. a stale value from an older
// build) — returning it verbatim would send an X-LLM-Provider the engine rejects.
export function getCreds(): LLMCreds | null {
  if (typeof window === "undefined") return null;
  try {
    const raw = window.sessionStorage.getItem(STORAGE_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as Partial<LLMCreds>;
    if (!parsed.key || !isProvider(parsed.provider)) return null;
    return { provider: parsed.provider, key: parsed.key };
  } catch {
    return null;
  }
}

// setCreds persists the credentials for the current tab session.
export function setCreds(creds: LLMCreds): void {
  if (typeof window === "undefined") return;
  window.sessionStorage.setItem(STORAGE_KEY, JSON.stringify(creds));
}

// clearCreds removes any stored credentials.
export function clearCreds(): void {
  if (typeof window === "undefined") return;
  window.sessionStorage.removeItem(STORAGE_KEY);
}

// llmHeaders returns the BYOK headers to attach to a plan-generating request, or
// an empty object when no key is set (the engine then uses its mock fallback).
export function llmHeaders(): Record<string, string> {
  const creds = getCreds();
  if (!creds) return {};
  return { "X-LLM-Provider": creds.provider, "X-LLM-Key": creds.key };
}
