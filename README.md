# mandos

Internal client manager — the shared foundation our tools build on. Named for the
Vala who keeps the records and knows every fate: Mandos owns **client identity**, the
**SSH access** to their hostings, and the **Google Drive** paths where their data
lives, so tools like `wpsite` and `aule` don't each reimplement them.

> **Internal only.** The Go module path is the bare name `mandos` (not a URL), so it
> is never fetched by `go get`, never sent to `proxy.golang.org` / `sum.golang.org`,
> and can't be indexed by `pkg.go.dev`. It's a locally-built CLI; tools shell out to it.

**Status: already adopted by `wpsite`.** wpsite reads the client list, SSH targets and
cloud paths from mandos, and delegates SSH-key onboarding to `mandos client setup-key`
— while keeping its own SSH *connection* multiplexing for backups/apply. A machine is
configured once with `mandos config init` (below); after that wpsite's own config only
needs `base_dir`.

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

### Password prompts (terminal or GUI)

`client add` / `client setup-key` may need a server password (for `ssh-copy-id`) or a
passphrase (when generating your first local key). mandos wires up OpenSSH's
`SSH_ASKPASS` so this works whether it's run from a terminal or a GUI:

- **From a terminal** — ssh prompts on the terminal as usual (unchanged).
- **Without a terminal** (e.g. launched by the mandos/wpsite GUI) — ssh can't reach
  `/dev/tty`, so mandos points `SSH_ASKPASS` at itself (`mandos askpass`, an internal
  subcommand) which pops a **native macOS password dialog**. The secret goes straight to
  ssh and is never logged. `SSH_ASKPASS_REQUIRE=force` is deliberately *not* set, so the
  terminal always wins when one is present.

If the key already authenticates, mandos probes that first (`BatchMode=yes`) and skips
the prompt entirely.

Designed to be scripted: plain output by default, `--json` where it helps, and clean
exit codes. Example (Bash):

```sh
target="$(mandos client get acme ssh)"
if mandos cloud available acme; then
  dest="$(mandos cloud path acme)/$(date +%Y%m%d_%H%M%S)"
  # …
fi
```

## GUI

A desktop front-end lives in [`mandos-gui/`](mandos-gui/) (Tauri + React/TS, same style
as the wpsite GUI) for browsing/editing clients, installing SSH keys and configuring the
machine — all by shelling out to this CLI. See its [README](mandos-gui/README.md).

## Layout

```
config/   two-layer local+team config resolution
client/   client registry (list/get/set/add/remove), comment-preserving edits
remote/   multiplexed SSH + key onboarding
drive/    Google Drive path resolution, availability, atomic copy
cmd/mandos/  the CLI
mandos-gui/  Tauri + React desktop front-end (shells out to the CLI)
```

## Env overrides

`MANDOS_CONFIG`, `MANDOS_TEAM_CONFIG`, `MANDOS_CLOUD_BASE`, `MANDOS_CLOUD_SUBDIR`.
