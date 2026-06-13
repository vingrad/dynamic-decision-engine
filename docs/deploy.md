# Deploying the public demo

This guide publishes the dynamic-decision-engine as a public, HTTPS demo on a
single host (e.g. a Hetzner VPS) using the same Docker Compose stack you run
locally. Visitors **bring their own LLM key** (BYOK): the operator holds no key,
pays no LLM cost, and carries no LLM-abuse risk. With no key supplied the engine
falls back to a deterministic mock so the demo still works end-to-end.

## How it fits together

A [Caddy](https://caddyserver.com/) reverse proxy is the single front door. The
browser always talks to one origin and Caddy routes by path:

- `/v1/*`, `/health*` → the Go API (`api:8080`)
- `/mcp*` → the API, with response buffering disabled (SSE streaming)
- `/grafana*` → Grafana (subpath)
- everything else → the Next.js admin UI (`admin:3000`)
- `/metrics` is **not** routed publicly — Prometheus scrapes it on the internal network.

Because the browser is same-origin, there is no build-time API URL to bake and
CORS is a non-issue. Configuration comes from `.env.prod` (see below).

## Compose files

Each environment is one self-contained file (no shared base / overrides) — read
it top to bottom, run it with a single `-f`:

| File | Run | Role |
| --- | --- | --- |
| `docker-compose.dev.yml` | `docker compose -f docker-compose.dev.yml up` | Local dev: publishes service ports, Caddy on `:80`, planner `byok` |
| `docker-compose.prod.yml` | `docker compose -f docker-compose.prod.yml --env-file .env.prod up -d` | VPS: Caddy `:80`/`:443` + TLS, no other host ports |

The two files duplicate the common service definitions on purpose: there is no
merge to reason about, so the prod file can never accidentally inherit a local
host-port mapping — only Caddy publishes ports in production.

## VPS setup (one time)

1. **DNS:** point an A/AAAA record for `DDE_DOMAIN` at the VPS IP. This must
   resolve *before* the first start or Caddy can't complete the Let's Encrypt
   HTTP-01 challenge.
2. **Docker:** install Docker Engine + the Compose plugin.
3. **Firewall:** allow inbound `22` (SSH, ideally your IP only), `80`, `443`;
   deny everything else. Use the Hetzner Cloud Firewall and/or `ufw`. Postgres,
   the API, the admin UI, Prometheus and Grafana are **not** published — they are
   reachable only on the internal Compose network.

## Configure and deploy

```bash
cp .env.prod.example .env.prod
# Edit .env.prod: DDE_DOMAIN, DDE_CORS_ALLOWED_ORIGINS, strong POSTGRES_PASSWORD
# and GF_SECURITY_ADMIN_PASSWORD. chmod 600 .env.prod.
chmod 600 .env.prod

docker compose -f docker-compose.prod.yml --env-file .env.prod up -d --build
```

Database migrations run automatically on API startup (they are embedded in the
binary and gated on Postgres being healthy). To update later:

```bash
git pull && docker compose -f docker-compose.prod.yml --env-file .env.prod up -d --build
```

## Using the demo (BYOK)

1. Open `https://DDE_DOMAIN`.
2. Create a goal and generate a plan — with no key you get a deterministic mock
   plan; inject a signal and watch it replan to a new immutable version.
3. Click the key panel in the header, pick a provider (Anthropic / OpenAI /
   DeepSeek) and paste your own API key. The key is held only in the browser tab
   (`sessionStorage`) and sent per-request as `X-LLM-Provider` / `X-LLM-Key`
   headers to generate **real** plans. It is never stored server-side or logged.

Direct API callers can do the same:

```bash
curl -X POST https://DDE_DOMAIN/v1/goals/<id>/plans \
  -H 'X-LLM-Provider: anthropic' -H 'X-LLM-Key: sk-...'
```

## Demo data lifecycle

Seed a few example goals (point the URL at the proxy front door):

```bash
for f in examples/founder-growth.json examples/career-strategy.json; do
  curl -fsS -X POST https://DDE_DOMAIN/v1/goals \
    -H 'Content-Type: application/json' --data @"$f"
done
```

Wipe and start clean (drops the DB volume, then migrations re-run on startup):

```bash
docker compose -f docker-compose.prod.yml --env-file .env.prod down -v
docker compose -f docker-compose.prod.yml --env-file .env.prod up -d --build
```

For a public demo, consider a nightly wipe-and-reseed cron to keep
accumulated/abusive data from piling up.

## Hardening notes / follow-ups

- **Grafana** is exposed under `/grafana` with a login. Set a strong
  `GF_SECURITY_ADMIN_PASSWORD`, or add Caddy `basic_auth` in front of `/grafana`,
  or drop the nav link entirely for a public demo.
- **Rate limiting** is not enabled in the Caddyfile because the `rate_limit`
  directive needs a custom Caddy build (`xcaddy`). BYOK removes LLM cost as a
  vector, so this is DoS/spam protection — add it (or an app-level limiter) before
  opening the demo to heavy traffic.
- **Client IPs:** chi's `RealIP` is left off (see `internal/api/routes.go`). Behind
  the trusted Caddy proxy you may re-enable it for accurate per-IP logging.
- **Upgrade path:** for faster/reproducible deploys, build the `api` and `admin`
  images in CI, push to a registry (e.g. GHCR), switch the prod compose `build:`
  to `image:`, and deploy with
  `docker compose -f docker-compose.prod.yml --env-file .env.prod pull && docker compose -f docker-compose.prod.yml --env-file .env.prod up -d`.
