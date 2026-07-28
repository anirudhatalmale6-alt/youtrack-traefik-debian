# Test harness (Pebble ACME) used to prove the guide

This is the exact setup used in [`../VERIFICATION.md`](../VERIFICATION.md) to
issue real certificates for both challenge types without a public domain, using
[Pebble](https://github.com/letsencrypt/pebble) (Let's Encrypt's own ACME test
server) as the CA.

Files:

* `traefik.yml` — Traefik v3 static config with **both** `certificatesResolvers`
  (`le-http` = HTTP‑01, `le-dns` = DNS‑01). Identical in shape to the delivered
  `dns-challenge/` and `http-challenge/` stacks; the only change is
  `caServer: https://localhost:14000/dir` (Pebble) instead of Let's Encrypt.
* `dynamic/config.yml` — routers for `http.yt.test` and `dns.yt.test`, the
  YouTrack service, and the same `youtrack-headers` middleware (HSTS,
  X‑Frame‑Options SAMEORIGIN, forwarded headers).
* `pebble-config.json` — Pebble configured to validate HTTP‑01 on port 80.
* `exec/dns-exec.go` — a ~40‑line static Go binary used as lego's `exec` DNS
  provider in the harness: it writes/removes the `_acme-challenge` TXT record in
  `pebble-challtestsrv`. In the real `dns-challenge/` stack this role is played
  by Traefik's built‑in **Namecheap** provider talking to the Namecheap API.

Everything was run rootless with Podman, all services sharing one pod's loopback,
so the whole ACME flow happens over `127.0.0.1` with no external dependencies.
