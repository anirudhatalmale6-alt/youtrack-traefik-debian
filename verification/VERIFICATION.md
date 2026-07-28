# Verification — proven end-to-end on a test server

This guide is not theoretical. I stood the whole stack up on my own test server
and drove the full certificate‑issuance flow for **both** challenge types until
real certificates were issued and YouTrack loaded over HTTPS through Traefik.

## How the certificate flow was proven without a public domain

To exercise the real ACME protocol without owning a public domain, I ran the
issuance against **[Pebble](https://github.com/letsencrypt/pebble)** — the small
ACME test server that **Let's Encrypt themselves publish** for exactly this
purpose — together with `pebble-challtestsrv`. Pebble speaks the identical ACME
protocol to Let's Encrypt, so Traefik goes through the genuine challenge flow
(account registration → order → challenge → validation → issuance). The **only**
difference from production is the signing CA is Pebble's local test CA instead of
Let's Encrypt. To go live you change a single line — the `caServer` — which the
main guide already documents.

The exact harness used is in [`harness/`](harness/) and is reproducible.

Components actually run:

| Component | Version |
|---|---|
| Traefik | 3.3.7 |
| YouTrack | 2026.2.17765 (official `jetbrains/youtrack` image) |
| Pebble (test ACME CA) | latest |

---

## 1. HTTP‑01 — certificate issued

Traefik solved the HTTP‑01 challenge on its `web` (:80) entry point. Pebble's
validation authority fetched the token and issued the certificate:

```
# Pebble log
Attempting to validate w/ HTTP: http://http.yt.test:80/.well-known/acme-challenge/LEbpVdnKFwJn3tn0qdBXoGtA66pu4XcFpO3x2az2yyg
Order ... is fully authorized. Processing finalization
Issued certificate serial 320415c6b17ebeef for order ...
```

Certificate Traefik stored (`acme/http.json`):

```
domain:  http.yt.test
issuer=  CN = Pebble Intermediate CA 3d7357
notBefore=Jul 28 15:48:44 2026 GMT
notAfter =Oct 26 15:48:43 2026 GMT   (90-day LE-style validity)
X509v3 Subject Alternative Name: critical
    DNS:http.yt.test
```

## 2. DNS‑01 — certificate issued

Traefik solved the DNS‑01 challenge: it created the `_acme-challenge` TXT record,
Pebble validated it via DNS, the cert was issued, and the TXT record was cleaned
up afterwards. (In the harness a tiny hook writes the TXT to the test DNS server;
in the delivered `dns-challenge/` stack this is the **Namecheap** provider doing
the identical job against Namecheap's API.)

```
# challtestsrv — TXT record provisioned then removed by the DNS-01 hook
Added   TXT response for Host "_acme-challenge.dns.yt.test." - Value "yx9TTa4TeBJv3W0UcE8PydpyI-pjzOZa7Hw1t-_w9JM"
Removed TXT response for Host "_acme-challenge.dns.yt.test."

# Pebble — validated the DNS-type challenge and issued the cert
Pulled a task from the Tasks queue: Identifier{Type:"dns", Value:"dns.yt.test"} ...
Order ... is fully authorized. Processing finalization
Issued certificate serial 12985b6e24e18058 for order ...
```

Certificate Traefik stored (`acme/dns.json`):

```
domain:  dns.yt.test
issuer=  CN = Pebble Intermediate CA 3d7357
notBefore=Jul 28 15:48:46 2026 GMT
notAfter =Oct 26 15:48:45 2026 GMT
X509v3 Subject Alternative Name: critical
    DNS:dns.yt.test
```

---

## 3. Traefik actually serves those certs over TLS

`openssl s_client` against the running Traefik, for each hostname (SNI):

```
SNI http.yt.test -> issuer=CN = Pebble Intermediate CA 3d7357   (valid 90 days)
SNI dns.yt.test  -> issuer=CN = Pebble Intermediate CA 3d7357   (valid 90 days)
```

## 4. Reverse‑proxy behaviour verified

All of the JetBrains reverse‑proxy requirements were checked against the live stack:

```
# http -> https redirect
$ curl -I http://http.yt.test/
HTTP/1.1 301 Moved Permanently
Location: https://http.yt.test/

# YouTrack served THROUGH Traefik over HTTPS (both cert domains)
$ curl -I https://http.yt.test/          $ curl -I https://dns.yt.test/
HTTP/2 200                                HTTP/2 200
strict-transport-security: max-age=31536000; includeSubDomains; preload
x-frame-options: SAMEORIGIN
```

* **HTTP/2 200** — Traefik terminates TLS and proxies to YouTrack on `:8080`.
* **HSTS** header present, one‑year max‑age.
* **X‑Frame‑Options: SAMEORIGIN** present (not DENY — as YouTrack requires).
* **http → https** 301 redirect working, and it did **not** interfere with the
  ACME HTTP‑01 challenge.

## 5. YouTrack UI over HTTPS

The YouTrack 2026.2 setup wizard, loaded in a browser over HTTPS through Traefik
(page title: *"Configuration Wizard: JetBrains YouTrack 2026.2"*):

![YouTrack over HTTPS through Traefik](youtrack_https.png)

---

## Reproducing this

See [`harness/`](harness/) for the Traefik config, the dynamic routers/middlewares,
the Pebble config, and the tiny DNS‑01 hook used to drive the test. The harness
runs the same Traefik `certificatesResolvers` blocks as the delivered stacks —
only the `caServer` points at the local Pebble instance instead of Let's Encrypt.
