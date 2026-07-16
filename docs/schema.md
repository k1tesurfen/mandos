# Config schema

Mandos uses two YAML files.

## Local config (per-user, per-machine)

`~/.config/mandos/mandos.yml` (override with `MANDOS_CONFIG`). Machine-specific only.

```yaml
team_config: /path/on/Drive/mandos/clients.yml   # shared client registry (below)
cloud_base:  /path/on/Drive/LIVE_WEB             # Google Drive projects root
```

- **`team_config`** — path to the shared clients file on your mounted Drive. Per-user,
  because every Drive mount path differs. Omit to run solo (clients then live in this
  file under a `clients:` key).
- **`cloud_base`** — parent of each site's project folder. Backups resolve to
  `<cloud_base>/<project-folder>/100_Backup`. Omit if you don't use cloud.

## Team config (shared on Google Drive — the source of truth)

Holds only the `clients:` map. Never put machine-specific paths here.

```yaml
clients:
  <name>:
    ssh: user@host            # REQUIRED — SSH target for the production server
    wp_root: /abs/path        # REQUIRED — absolute path to the WordPress install
    remote_tmp: ~/.tmp        # optional — remote staging dir override
    local_host: name.test     # optional — local hostname override
    login_path: /geheim-login # optional — custom wp-admin/login path (default /wp-admin/)
    domain: example.com       # optional — production domain (derives the Drive folder)
    cloud_folder: example.de  # optional — PORTABLE Drive project-folder NAME under cloud_base
    cloud_dir: /abs/…/100_Backup  # optional — absolute Drive dir (machine-specific; discouraged)
    # Tools may add their own keys here (e.g. deactivate_plugins, review_pages).
    # Mandos preserves unknown keys and comments on edit.
```

`<name>` must be a DNS label: lowercase letters, digits and hyphens, not starting or
ending with a hyphen.

### Client-file resolution

| team_config set? | team file present? | reads use | writes use |
|---|---|---|---|
| no  | —       | local file      | local file |
| yes | yes     | team file       | team file  |
| yes | no      | error (degrade) | error (refuse) |

Team is authoritative — clients are never redefined locally, so there is no merge or
precedence logic. A configured-but-missing team file (Drive unmounted) makes reads
error and **writes refuse**, so a client is never silently written where the team
can't see it.

### Cloud path resolution

`mandos cloud path <name>` resolves in this order:

1. `cloud_dir` — used verbatim (absolute).
2. `<cloud_base>/<cloud_folder>/100_Backup`.
3. `<cloud_base>/<domain>/100_Backup`.

Errors if none of `cloud_dir`/`cloud_folder`/`domain` is set (or `cloud_base` is
needed but unset). The `100_Backup` subfolder name is overridable via
`MANDOS_CLOUD_SUBDIR`. Mandos writes only inside that subfolder and **never creates the
project (domain) folder** — `cloud available` returns false until it exists.
