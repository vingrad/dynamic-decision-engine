"use client";

import { useEffect, useState } from "react";
import {
  LLM_PROVIDERS,
  type LLMProvider,
  getCreds,
  setCreds,
  clearCreds,
} from "@/lib/byok";

// KeyPanel lets a demo visitor supply their own LLM API key (bring-your-own-key).
// The key is stored only in sessionStorage and sent per-request to generate plans;
// with no key the engine falls back to a deterministic mock so the demo still runs.
export function KeyPanel() {
  const [open, setOpen] = useState(false);
  const [provider, setProvider] = useState<LLMProvider>("anthropic");
  const [key, setKey] = useState("");
  const [connected, setConnected] = useState<LLMProvider | null>(null);

  // Hydrate from sessionStorage on mount (client-only).
  useEffect(() => {
    const creds = getCreds();
    if (creds) {
      setProvider(creds.provider);
      setConnected(creds.provider);
    }
  }, []);

  function save() {
    if (!key.trim()) return;
    setCreds({ provider, key: key.trim() });
    setConnected(provider);
    setKey("");
    setOpen(false);
  }

  function disconnect() {
    clearCreds();
    setConnected(null);
    setKey("");
    setProvider("anthropic");
    setOpen(false);
  }

  const label = connected
    ? `Key: ${LLM_PROVIDERS.find((p) => p.id === connected)?.label ?? connected}`
    : "Mock mode — add LLM key";

  return (
    <div className="keypanel">
      <button className="btn secondary" onClick={() => setOpen((v) => !v)}>
        {connected ? "🔑 " : "⚪ "}
        {label}
      </button>

      {open && (
        <div className="keypanel-pop card">
          <p className="meta">
            Paste your own provider API key to generate real plans. It is kept only in
            this browser tab (sessionStorage) and sent to the engine to make the call —
            it is never stored server-side. Leave empty to use the deterministic mock.
          </p>
          <label>Provider</label>
          <select value={provider} onChange={(e) => setProvider(e.target.value as LLMProvider)}>
            {LLM_PROVIDERS.map((p) => (
              <option key={p.id} value={p.id}>
                {p.label}
              </option>
            ))}
          </select>
          <label>API key</label>
          <input
            type="password"
            value={key}
            onChange={(e) => setKey(e.target.value)}
            placeholder="sk-…"
            autoComplete="off"
          />
          <div className="row" style={{ marginTop: 12 }}>
            <button className="btn" type="button" onClick={save} disabled={!key.trim()}>
              Save key
            </button>
            {connected && (
              <button className="btn secondary" type="button" onClick={disconnect}>
                Disconnect
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
