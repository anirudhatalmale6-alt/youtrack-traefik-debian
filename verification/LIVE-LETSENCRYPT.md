# Live proof — real domain, real Namecheap API, real Let's Encrypt

The earlier [`VERIFICATION.md`](VERIFICATION.md) proved the whole flow end-to-end
using Let's Encrypt's own ACME **test** server (Pebble), because at that point
there was no public domain to work with.

The client then registered a real domain — **`ocogo.space`** — on Namecheap,
funded the account so the Namecheap API was enabled, and whitelisted the test
server's IP. That let me run the **exact `dns-challenge/` path from the guide**
against the **real Namecheap API** and the **real Let's Encrypt**, and obtain a
genuine, publicly‑trusted certificate for `youtrack.ocogo.space`.

The only change from the delivered `dns-challenge/traefik.yml` is the `caServer`
line (staging vs production); the `certificatesResolvers.namecheap` block and the
`youtrack-headers` middleware are byte-for-byte the same as what you deploy.

Components run: Traefik **v3.3** · YouTrack **2026.2.17765** (official image) ·
Namecheap production DNS API · Let's Encrypt (staging, then production).

---

## 1. Traefik drove the Namecheap API — TXT record created, then cleaned up

During the DNS‑01 challenge, Traefik's built‑in Namecheap provider wrote the
`_acme-challenge` TXT record through the real Namecheap API. Confirmed live via
`namecheap.domains.dns.getHosts` **while the challenge was in flight**:

```
Name="_acme-challenge.youtrack"  Type="TXT"     <-- created by Traefik via Namecheap API
```

Publicly resolvable at that moment (queried against Cloudflare 1.1.1.1):

```
$ dig +short TXT _acme-challenge.youtrack.ocogo.space @1.1.1.1
"Psj0fnurHPj9mHk1euv37Ll-eNGaByqIh2Qu05t7PQc"
```

After issuance, Traefik removed it again — `getHosts` no longer lists any
`_acme-challenge` record. That is the full create → validate → clean‑up cycle,
against the live Namecheap API.

## 2. Let's Encrypt STAGING — certificate issued

```
issuer  = C=US, O=Let's Encrypt, CN=(STAGING) Ersatz Emmer YR2
subject = CN=youtrack.ocogo.space
notBefore=Jul 28 17:01:55 2026 GMT   notAfter=Oct 26 17:01:54 2026 GMT
X509v3 Subject Alternative Name: DNS:youtrack.ocogo.space
```

## 3. Let's Encrypt PRODUCTION — genuinely trusted certificate issued

```
issuer  = C=US, O=Let's Encrypt, CN=YR1
subject = CN=youtrack.ocogo.space
serial  = 050C831380787FB0A279DDBC27ED25376AE9
notBefore=Jul 28 17:04:17 2026 GMT   notAfter=Oct 26 17:04:16 2026 GMT
X509v3 Subject Alternative Name: DNS:youtrack.ocogo.space
```

This chains to a publicly-trusted root — verified against the OS trust store:

```
$ openssl verify -CApath /etc/ssl/certs prod.pem
prod.pem: OK
```

(The certificate is also visible in the public Certificate Transparency logs, as
every real Let's Encrypt certificate is.)

## 4. Traefik serves that trusted cert, YouTrack behind it over HTTP/2

`curl` using the normal system trust store (no `-k`, no override), routed to the
running Traefik, with YouTrack live behind it:

```
$ curl -I https://youtrack.ocogo.space/
TLS verify: 0            <-- 0 = certificate fully trusted
HTTP/2 200              <-- YouTrack served through Traefik
strict-transport-security: max-age=31536000; includeSubDomains; preload
x-frame-options: SAMEORIGIN
```

* **TLS verify: 0** — the production Let's Encrypt cert validated against the
  public trust store, no exceptions.
* **HTTP/2 200** — Traefik terminated TLS and proxied to YouTrack on `:8080`.
* **HSTS** and **X‑Frame‑Options: SAMEORIGIN** applied by the same middleware
  shipped in the delivered stack.

## 5. YouTrack 2026.2 over trusted HTTPS on the real domain

Loaded in Chromium **without** any certificate override (`--ignore-certificate-errors`
was *not* used). The browser accepted the Let's Encrypt certificate on its own and
rendered the page — which is only possible with a valid, trusted certificate:

![YouTrack 2026.2 over trusted HTTPS on youtrack.ocogo.space](youtrack_real_https.png)

---

In short: the DNS‑01 + Namecheap path in this guide has now been run against the
real Namecheap API and the real Let's Encrypt, producing a genuine trusted
certificate on a live domain — not a lab stand‑in.
