# Running YouTrack behind Traefik on Debian

A complete, copy‑paste‑ready guide to run **JetBrains YouTrack** (Docker) behind
**Traefik v3** as a reverse proxy on **Debian**, with **Let's Encrypt** TLS
certificates issued two different ways:

1. **DNS‑01 challenge** using **Namecheap** as the DNS provider — supports
   wildcards and works even when port 80 is closed to the internet.
2. **HTTP‑01 challenge** — the simplest option when the server is publicly
   reachable on port 80.

Both setups are provided as self‑contained `docker compose` stacks so you can
stand the whole thing up with a single command.

> **This has been proven end‑to‑end, not just written.** Both challenge types
> were run until real certificates were issued and YouTrack loaded over HTTPS
> through Traefik — see [`verification/VERIFICATION.md`](verification/VERIFICATION.md)
> for the logs, certificate details, and a screenshot.

```
youtrack-traefik-guide/
├── dns-challenge/          # Stack #1 — Let's Encrypt via DNS-01 (Namecheap)
│   ├── docker-compose.yml
│   ├── traefik.yml         # Traefik static config
│   ├── dynamic/
│   │   └── middlewares.yml # headers / HSTS / TLS options
│   └── .env.example
└── http-challenge/         # Stack #2 — Let's Encrypt via HTTP-01
    ├── docker-compose.yml
    ├── traefik.yml
    ├── dynamic/
    │   └── middlewares.yml
    └── .env.example
```

---

## Table of contents

1. [How it fits together](#1-how-it-fits-together)
2. [Prerequisites](#2-prerequisites)
3. [Step 1 — Prepare the Debian server](#3-step-1--prepare-the-debian-server)
4. [Step 2 — Install Docker](#4-step-2--install-docker)
5. [Step 3 — Install YouTrack (Docker)](#5-step-3--install-youtrack-docker)
6. [Step 4 — Set the YouTrack Base URL](#6-step-4--set-the-youtrack-base-url)
7. [Reverse‑proxy requirements — and how Traefik satisfies them](#7-reverse-proxy-requirements--and-how-traefik-satisfies-them)
8. [Option A — TLS via DNS‑01 challenge (Namecheap)](#8-option-a--tls-via-dns-01-challenge-namecheap)
9. [Option B — TLS via HTTP‑01 challenge](#9-option-b--tls-via-http-01-challenge)
10. [Step 5 — Complete the YouTrack setup wizard](#10-step-5--complete-the-youtrack-setup-wizard)
11. [Verifying everything works](#11-verifying-everything-works)
12. [Certificate renewal](#12-certificate-renewal)
13. [Troubleshooting](#13-troubleshooting)
14. [DNS‑01 vs HTTP‑01 — which should I use?](#14-dns-01-vs-http-01--which-should-i-use)

---

## 1. How it fits together

```
                          :443 (HTTPS)          docker network "web"
   Browser  ───────────►  ┌─────────┐  ─────────────────────────►  ┌──────────┐
                          │ Traefik │   http://youtrack:8080        │ YouTrack │
   :80 ──redirect──►:443  │  v3     │  ◄─────────────────────────   │  :8080   │
                          └────┬────┘                               └──────────┘
                               │ ACME (Let's Encrypt)
                               │   • DNS-01  → Namecheap API (TXT record)
                               │   • HTTP-01 → token served on :80
                               ▼
                        acme.json (certs, persisted in a volume)
```

* **Traefik** terminates TLS, redirects http→https, forwards the required proxy
  headers, and transparently handles WebSockets and Server‑Sent‑Events.
* **YouTrack** runs as the official Docker image and is *never* exposed directly
  to the host — Traefik reaches it privately over the `web` docker network on
  port `8080`.
* **Let's Encrypt** certificates are obtained and auto‑renewed by Traefik and
  stored in `acme.json` inside a named volume.

---

## 2. Prerequisites

| Requirement | Detail |
|---|---|
| Server | Debian 12 (bookworm) or 11, 64‑bit. 2 vCPU / 4 GB RAM minimum for YouTrack; 8 GB recommended. |
| Access | `root` or a `sudo`‑capable user. |
| Domain | A hostname you control, e.g. `youtrack.example.com`. |
| DNS | For **DNS‑01**: the domain must use **Namecheap BasicDNS** (Namecheap's own nameservers) and you must be able to enable Namecheap API access. For **HTTP‑01**: an `A`/`AAAA` record pointing at the server, with **port 80 open** to the internet. |
| Firewall | Ports **80** and **443** open inbound. (DNS‑01 technically only needs 443, but we keep 80 for the http→https redirect.) |

---

## 3. Step 1 — Prepare the Debian server

```bash
# Update the base system
sudo apt-get update && sudo apt-get -y upgrade

# Basic tooling used in this guide
sudo apt-get -y install curl ca-certificates gnupg git ufw

# Firewall: allow SSH + web
sudo ufw allow OpenSSH
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw --force enable
sudo ufw status
```

Point your DNS record at the server now so it has time to propagate:

* `youtrack.example.com  A   <server-public-IPv4>`
* (optional) `youtrack.example.com  AAAA  <server-public-IPv6>`

Confirm it resolves:

```bash
dig +short youtrack.example.com
```

---

## 4. Step 2 — Install Docker

Install Docker Engine + the Compose plugin from Docker's official repository:

```bash
# Add Docker's GPG key
sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/debian/gpg | \
  sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
sudo chmod a+r /etc/apt/keyrings/docker.gpg

# Add the repository
echo \
  "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] \
  https://download.docker.com/linux/debian $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | \
  sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

# Install
sudo apt-get update
sudo apt-get -y install docker-ce docker-ce-cli containerd.io \
  docker-buildx-plugin docker-compose-plugin

# Verify
sudo docker run --rm hello-world
docker compose version
```

(Optional) run docker without `sudo`:

```bash
sudo usermod -aG docker "$USER"
newgrp docker
```

---

## 5. Step 3 — Install YouTrack (Docker)

We deploy YouTrack with Docker **Compose** together with Traefik (see the two
stacks below). This section explains the YouTrack half so you understand what
the compose file is doing; you do **not** need to run these `docker run`
commands by hand — the compose stack handles it.

Reference: <https://www.jetbrains.com/help/youtrack/server/youtrack-docker-installation.html>

Key facts baked into the compose files:

* Image: `jetbrains/youtrack:<version>` (pick a tag from
  <https://hub.docker.com/r/jetbrains/youtrack/tags>).
* YouTrack listens on **port 8080 inside the container**. We do **not** publish
  it to the host — only Traefik talks to it, over the internal docker network.
* Four persistent directories are bind‑mounted from the host so the data is
  visible on the machine, easy to back up, and never lost on restart/upgrade:

  | Host directory | Container path | Purpose |
  |---|---|---|
  | `/opt/youtrack/data` | `/opt/youtrack/data` | database & attachments |
  | `/opt/youtrack/conf` | `/opt/youtrack/conf` | configuration (incl. base‑url) |
  | `/opt/youtrack/logs` | `/opt/youtrack/logs` | logs |
  | `/opt/youtrack/backups` | `/opt/youtrack/backups` | backups |

The official image runs as the dedicated `13001` user, so **create these
directories once and hand them to that user before the first start**:

```bash
mkdir -p /opt/youtrack/{data,conf,logs,backups}
chown -R 13001:13001 /opt/youtrack/{data,conf,logs,backups}
```

(Prefer Docker‑managed named volumes instead? Swap each `/opt/youtrack/X:/opt/youtrack/X`
line in the compose file for a named volume like `youtrack-X:/opt/youtrack/X` and
declare them under the bottom `volumes:` key. Data still persists either way;
host bind mounts just keep it visible on the machine. Note that YouTrack prints
a harmless "non‑anonymous volume" warning with named volumes — bind mounts avoid it.)

Choose **one** of the two stacks (`dns-challenge/` or `http-challenge/`) — do
not run both at once, since they both bind ports 80/443.

---

## 6. Step 4 — Set the YouTrack Base URL

This is the single most important reverse‑proxy step. YouTrack must know the
**public HTTPS URL** users reach it on, otherwise links, redirects and OAuth
callbacks point at the wrong host.

Run the one‑off `configure` command against the same conf volume the stack uses
(run it from inside the chosen stack directory, e.g. `dns-challenge/`):

```bash
cd dns-challenge          # or: cd http-challenge
docker compose run --rm --no-deps youtrack \
  configure --base-url=https://youtrack.example.com
```

* Use your real hostname.
* No port on the URL — we serve on the standard 443.
* If you ever serve YouTrack from a sub‑path, include it, e.g.
  `--base-url=https://example.com/youtrack`.

Reference: <https://www.jetbrains.com/help/youtrack/server/reverse-proxy-configuration.html>

---

## 7. Reverse‑proxy requirements — and how Traefik satisfies them

The JetBrains reverse‑proxy page lists a number of requirements. Here is each
one and exactly how this setup meets it, so nothing is left to chance.

| YouTrack requirement | How this stack satisfies it |
|---|---|
| **Base URL** set to the public HTTPS address | `configure --base-url=…` (Step 4). |
| Forward `X-Forwarded-For`, `X-Forwarded-Host`, `X-Forwarded-Proto` | Traefik sets all of these automatically on every proxied request. |
| `X-Forwarded-Scheme` | Added explicitly in `dynamic/middlewares.yml` (`customRequestHeaders`). |
| **WebSockets** (remote debugger, Whiteboards) | Traefik detects the `Upgrade` header and tunnels ws/wss **natively** — no config needed. |
| **No response buffering** for live updates (`/api/eventSourceBus` SSE) | Traefik streams responses and does not buffer by default. We deliberately do **not** attach a `buffering` middleware. Entry‑point `readTimeout`/`idleTimeout` are set to `0` so long‑lived SSE streams are never cut. |
| Large **upload body size** | Traefik imposes **no** request‑body limit by default, so attachments pass through. (Optional cap shown in the notes.) |
| **HSTS** `max-age=31536000; includeSubDomains` | Set via the `headers` middleware (`stsSeconds`, `stsIncludeSubdomains`). |
| **X‑Frame‑Options: SAMEORIGIN** (not DENY) | Set via `customResponseHeaders` — DENY would break YouTrack's token‑refresh iframe. |
| **TLS 1.2+** | `tls.options.default.minVersion: VersionTLS12`. |
| **HTTP/2** | Enabled automatically by Traefik on the HTTPS entry point. |
| `proxy_http_version 1.1` (NGINX equivalent) | Traefik speaks HTTP/1.1 to the backend, which is what keeps WebSockets/SSE working. |
| Pass the original **Host** header | `loadbalancer.passhostheader=true`. |

In short: Traefik handles WebSockets, SSE streaming, HTTP/2 and forwarded
headers out of the box. The only things we configure explicitly are the extra
`X-Forwarded-Scheme` header, HSTS, `X-Frame-Options: SAMEORIGIN`, and the TLS
minimum version — all in one small `dynamic/middlewares.yml` file.

---

## 8. Option A — TLS via DNS‑01 challenge (Namecheap)

Use the files in **`dns-challenge/`**.

### 8.1 Enable Namecheap API access

1. Log into Namecheap → **Profile → Tools → Namecheap API Access**.
2. Toggle **API Access** on. Namecheap only grants this to accounts that meet
   **one** of: a **$50+ account balance**, **20+ domains**, or **$50+ spent** in
   the last two years. (This is a Namecheap policy, not a Traefik limitation.)
3. Note your **API Username** and copy the generated **API Key**.
4. **Whitelist your server's public IPv4** in the same screen. The Let's Encrypt
   client (lego, embedded in Traefik) calls the Namecheap API *from this
   server*, so its outbound IP must be on the whitelist. Find it with:

   ```bash
   curl -4 https://api.ipify.org ; echo
   ```

> **Important — Namecheap BasicDNS only.** The domain must be using Namecheap's
> own nameservers (BasicDNS). Namecheap's API sets host records in bulk, so a
> domain delegated to third‑party nameservers (Cloudflare, Route 53, …) will not
> work with this provider.

### 8.2 Configure and launch

```bash
cd dns-challenge

# Fill in your values
cp .env.example .env
nano .env                 # set NAMECHEAP_API_USER, NAMECHEAP_API_KEY (and YOUTRACK_VERSION)

# Set your public hostname on the router `rule` line
nano dynamic/youtrack.yml     # Host(`youtrack.example.com`) -> your hostname

# Set your real email in traefik.yml (the acme "email:" field)
nano traefik.yml

# Set the YouTrack base URL (see Step 4)
docker compose run --rm --no-deps youtrack \
  configure --base-url=https://youtrack.example.com

# Bring the stack up
docker compose up -d

# Watch Traefik obtain the certificate over DNS-01
docker compose logs -f traefik
```

You should see Traefik create a `_acme-challenge` TXT record via the Namecheap
API, wait for it to propagate, and then obtain the certificate.

### 8.3 How the DNS‑01 config works

In `traefik.yml`:

```yaml
certificatesResolvers:
  namecheap:
    acme:
      email: "admin@example.com"
      storage: /etc/traefik/acme/acme.json
      caServer: https://acme-staging-v02.api.letsencrypt.org/directory  # test first
      dnsChallenge:
        provider: namecheap
        resolvers:
          - "1.1.1.1:53"
          - "8.8.8.8:53"
        propagation:
          delayBeforeChecks: 0
```

The `NAMECHEAP_API_USER` / `NAMECHEAP_API_KEY` environment variables (from
`.env`, injected in `docker-compose.yml`) are what the `namecheap` provider
reads. Optional tuning vars — `NAMECHEAP_PROPAGATION_TIMEOUT` (default 3600s),
`NAMECHEAP_POLLING_INTERVAL` (15s), `NAMECHEAP_TTL` (120s) — are wired up too.

### 8.4 Wildcard certificates (DNS‑01 only)

DNS‑01 is the only challenge that can issue **wildcard** certs. To also cover
`*.example.com`, uncomment the `tls.domains:` block in
`dns-challenge/dynamic/youtrack.yml` and set your root domain there.

### 8.5 Going to production

Once staging issues a cert cleanly, edit `traefik.yml`: comment out the
`acme-staging` `caServer` line (switching to the production endpoint), then:

```bash
docker compose down
docker volume rm dns-challenge_traefik-acme   # discard the staging cert
docker compose up -d
```

---

## 9. Option B — TLS via HTTP‑01 challenge

Use the files in **`http-challenge/`**.

### 9.1 Requirements

* `YOUTRACK_DOMAIN` must resolve (A/AAAA) to **this** server.
* **Port 80 must be reachable from the public internet** — Let's Encrypt fetches
  a validation token from `http://youtrack.example.com/.well-known/acme-challenge/…`.
* No API keys or DNS provider needed — this is the simpler option.

### 9.2 Configure and launch

```bash
cd http-challenge

cp .env.example .env
nano .env                 # set YOUTRACK_VERSION

nano dynamic/youtrack.yml # set your public hostname on the router `rule` line

nano traefik.yml          # set your real acme email

# Set the YouTrack base URL (see Step 4)
docker compose run --rm --no-deps youtrack \
  configure --base-url=https://youtrack.example.com

docker compose up -d
docker compose logs -f traefik
```

### 9.3 How the HTTP‑01 config works

In `traefik.yml`:

```yaml
certificatesResolvers:
  letsencrypt:
    acme:
      email: "admin@example.com"
      storage: /etc/traefik/acme/acme.json
      caServer: https://acme-staging-v02.api.letsencrypt.org/directory  # test first
      httpChallenge:
        entryPoint: web     # the :80 entry point
```

The `web` (`:80`) entry point has a global http→https redirect, **but** Traefik
serves the ACME challenge on `:80` at a higher priority, so the redirect never
interferes with issuance or renewal.

### 9.4 Going to production

Same as DNS‑01: switch `caServer` to production, then recreate with a fresh
`acme.json`:

```bash
docker compose down
docker volume rm http-challenge_traefik-acme
docker compose up -d
```

---

## 10. Step 5 — Complete the YouTrack setup wizard

On first launch YouTrack prints a one‑time wizard token in its log:

```bash
docker compose logs youtrack | grep -i wizard_token
```

Because YouTrack now sits behind HTTPS, open the wizard on your real domain and
paste the token as the query parameter:

```
https://youtrack.example.com/?wizard_token=<TOKEN>
```

In the wizard: confirm the Base URL (it should already show your HTTPS URL),
set the administrator account, accept the licence, and finish. Do not close the
tab until it completes.

---

## 11. Verifying everything works

```bash
# 1. Certificate is real (or Let's Encrypt STAGING while testing) and valid
echo | openssl s_client -connect youtrack.example.com:443 -servername youtrack.example.com 2>/dev/null \
  | openssl x509 -noout -issuer -subject -dates

# 2. http is redirected to https (expect 301 -> https://...)
curl -sI http://youtrack.example.com | grep -i location

# 3. Security headers present (HSTS + X-Frame-Options: SAMEORIGIN)
curl -sI https://youtrack.example.com | grep -iE 'strict-transport|x-frame-options'

# 4. YouTrack answers through the proxy
curl -sSf https://youtrack.example.com/api/config >/dev/null && echo "YouTrack OK"
```

* Log into YouTrack, open a project board, and confirm **live updates** appear
  without a refresh (that exercises the SSE path).
* If you use the **Whiteboards** feature or the remote debugger, confirm they
  connect — that exercises the WebSocket path.

> While testing against **Let's Encrypt staging**, browsers will warn that the
> certificate is untrusted (it's signed by the staging CA). That's expected —
> switch to production (Section 8.5 / 9.4) to get a browser‑trusted cert.

---

## 12. Certificate renewal

Renewal is **fully automatic** — Traefik checks daily and renews each
certificate ~30 days before expiry using the same challenge you configured.
There is no cron job to add. Just make sure:

* the `traefik-acme` volume (holding `acme.json`) is preserved across restarts
  (it is a named volume in these stacks), and
* for DNS‑01, the Namecheap API key stays valid and the server IP stays
  whitelisted; for HTTP‑01, port 80 stays open.

To force an early renewal test, stop the stack, delete the acme volume, and
start again (this re‑issues immediately).

---

## 13. Troubleshooting

| Symptom | Likely cause & fix |
|---|---|
| `docker compose logs traefik` shows ACME rate‑limit errors | You hit Let's Encrypt production limits. Always test on **staging** first (default in these files). |
| DNS‑01: `namecheap: … IP is not whitelisted` / `API Key is invalid` | The server's outbound IPv4 (`curl -4 https://api.ipify.org`) is not on the Namecheap API whitelist, or API access isn't enabled / eligibility not met. |
| DNS‑01: `time limit exceeded` waiting for TXT | Propagation slow. It's usually fine within a few minutes; the default `NAMECHEAP_PROPAGATION_TIMEOUT=3600` allows for it. Ensure the domain uses **Namecheap BasicDNS**. |
| HTTP‑01: `connection refused` / `timeout during connect` | Port 80 isn't reachable from the internet. Open the firewall, and make sure the A/AAAA record points at this server. |
| Browser shows "not secure" during testing | You're still on the Let's Encrypt **staging** CA. Switch to production (Sections 8.5 / 9.4). |
| YouTrack links/redirects point to `http://…:8080` | Base URL not set. Re‑run the `configure --base-url=https://…` command (Step 4) and restart YouTrack. |
| Users get logged out unexpectedly | `X-Frame-Options` is `DENY` somewhere. It must be **SAMEORIGIN** (already set in `middlewares.yml`). |
| Live updates don't stream | A buffering proxy in front of Traefik, or a `buffering` middleware was added. Remove it — Traefik streams SSE by default. |
| 404 from Traefik | The router rule host doesn't match the domain you're visiting, or the `youtrack` container isn't on the `web` network / `traefik.enable=true` label missing. |

Useful commands:

```bash
docker compose ps                 # container status
docker compose logs -f traefik    # proxy + ACME logs
docker compose logs -f youtrack   # YouTrack logs
docker compose exec traefik cat /etc/traefik/acme/acme.json | head   # stored certs
```

---

## 14. DNS‑01 vs HTTP‑01 — which should I use?

| | **DNS‑01 (Namecheap)** | **HTTP‑01** |
|---|---|---|
| Port 80 must be public | No | **Yes** |
| Wildcard certs | **Yes** | No |
| Extra credentials | Namecheap API key + IP whitelist | None |
| Works behind CDN / closed firewall | **Yes** | No |
| Simplicity | Moderate | **Simplest** |

* Choose **HTTP‑01** if the server is directly on the internet with port 80 open
  and you only need a cert for the exact hostname.
* Choose **DNS‑01 (Namecheap)** if port 80 is closed/filtered, the server is
  behind a CDN, or you want a wildcard certificate.

---

### References

* YouTrack Docker installation — <https://www.jetbrains.com/help/youtrack/server/youtrack-docker-installation.html>
* YouTrack reverse‑proxy configuration — <https://www.jetbrains.com/help/youtrack/server/reverse-proxy-configuration.html>
* Traefik ACME certificate resolvers — <https://doc.traefik.io/traefik/reference/install-configuration/tls/certificate-resolvers/acme/>
* Traefik Namecheap (lego) provider — <https://go-acme.github.io/lego/dns/namecheap/>
