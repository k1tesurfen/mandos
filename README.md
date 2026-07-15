# mandos

Internal client manager — the shared foundation our tools build on. Named for the
Vala who keeps the records and knows every fate: Mandos owns **client identity**, the
**SSH access** to their hostings, and the **Google Drive** paths where their data
lives, so tools like `wpsite` and `aule` don't each reimplement them.

> **Internal only.** The Go module path is the bare name `mandos` (not a URL), so it
> is never fetched by `go get`, never sent to `proxy.golang.org` / `sum.golang.org`,
> and can't be indexed by `pkg.go.dev`. It's a locally-built CLI; tools shell out to it.

## What it does

- **Client registry** — a shared `clients:` map (SSH target, WP root, cloud folder, …)
  on a mounted Google Drive, edited comment-preservingly (like `yq -i`). Two layers: a
  per-user **local** config points at the **team** file; team is authoritative.
- **SSH access** — multiplexed connections to client servers (one auth, reused) and
  key onboarding (probe → `ssh-copy-id` → manual `authorized_keys` fallback on macOS).
- **Google Drive** — resolve each client's backup dir, check availability (never
  creating the project folder), and copy in atomically.

## Install

```sh
make install            # builds and installs to /usr/local/bin/mandos
```

Requires Go, and at runtime: `ssh` (and `rsync`/`ssh-copy-id` where available).

## Setup

```sh
mandos config init \
  --team-config "/…/GoogleDrive/…/mandos/clients.yml" \
  --cloud-base  "/…/GoogleDrive/…/LIVE_WEB"
```

See `mandos.yml.example` (local) and `mandos.team.yml.example` (shared). Full schema in
[`docs/schema.md`](docs/schema.md).

## Usage

```sh
mandos client list [--json]
mandos client get acme                 # all fields
mandos client get acme ssh             # one field  -> ubuntu@acme-industrial.com
mandos client add acme --ssh ubuntu@host --wp-root /var/www/acme
mandos client setup-key acme           # (re)install your SSH key on the server
mandos client remove acme

mandos ssh acme -- wp core version     # run a command on the client's server
mandos cloud path acme                 # -> …/acme.com/100_Backup
mandos cloud available acme            # exit 0 if reachable, else 1
mandos config path
```

Designed to be scripted: plain output by default, `--json` where it helps, and clean
exit codes. Example (Bash):

```sh
target="$(mandos client get acme ssh)"
if mandos cloud available acme; then
  dest="$(mandos cloud path acme)/$(date +%Y%m%d_%H%M%S)"
  # …
fi
```

## Layout

```
config/   two-layer local+team config resolution
client/   client registry (list/get/set/add/remove), comment-preserving edits
remote/   multiplexed SSH + key onboarding
drive/    Google Drive path resolution, availability, atomic copy
cmd/mandos/  the CLI
```

## Env overrides

`MANDOS_CONFIG`, `MANDOS_TEAM_CONFIG`, `MANDOS_CLOUD_BASE`, `MANDOS_CLOUD_SUBDIR`.
