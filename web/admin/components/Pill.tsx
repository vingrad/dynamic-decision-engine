import type { Level } from "@/lib/api";

// Pill renders a small coloured badge for a low/medium/high level value.
export function Pill({ level, label }: { level: Level; label?: string }) {
  return <span className={`pill ${level}`}>{label ? `${label}: ${level}` : level}</span>;
}
