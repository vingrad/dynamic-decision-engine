import "./globals.css";
import type { Metadata } from "next";
import Link from "next/link";
import { KeyPanel } from "@/components/KeyPanel";

export const metadata: Metadata = {
  title: "DDE Admin",
  description: "Admin console for the dynamic-decision-engine",
};

// Grafana URL is configurable; the admin UI links out to it for ops dashboards
// rather than re-implementing operational monitoring. Defaults to the same-origin
// /grafana subpath (served by the reverse proxy) so one built image works on any
// host; nullish (??) so an explicit "" can disable rebaking.
const grafanaURL = process.env.NEXT_PUBLIC_GRAFANA_URL ?? "/grafana";

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>
        <header className="topbar">
          <div className="inner">
            <span className="brand">
              dynamic-decision-engine
              <small>admin</small>
            </span>
            <nav className="nav">
              <Link href="/">Goals</Link>
              <a href={grafanaURL} target="_blank" rel="noreferrer">
                Grafana ↗
              </a>
              <KeyPanel />
            </nav>
          </div>
        </header>
        <main className="container">{children}</main>
      </body>
    </html>
  );
}
