# mandos-gui

A Tauri + React/TypeScript desktop front-end for the `mandos` CLI — the visual
counterpart to the `wpsite` GUI, in the same aule-inspired style (dark sidebar, light
neumorphic workspace, a full-width console at the bottom). **German-only UI.**

The CLI stays the source of truth: the GUI only shells out to `/usr/local/bin/mandos`
and streams its output. No logic lives in the GUI — install/build `mandos` first.

## What it does

- **KUNDEN** — lists clients (`mandos client list`). Selecting one shows its fields
  (SSH target, WP root, cloud folder, Live-Domain, …) in an editable form. **Speichern**
  applies only the changed fields via `mandos client set` / `unset`.
- Per-client actions: **SSH-Schlüssel installieren** (`client setup-key`), **SSH testen**
  (`ssh … -- uname -a`), **Cloud prüfen** (`cloud path` + `cloud available`), and
  **Entfernen** (`client remove`, with a type-the-name confirmation).
- **Neuer Kunde** — a form that runs `mandos client add …` (with an optional
  “SSH-Schlüssel jetzt installieren” checkbox → `--no-key` when off).
- **Konfiguration** — shows the resolved local/team/cloud paths (with a reachability
  badge) and runs `mandos config init` to point this machine at the shared registry +
  Drive root.

> **Password prompts work from the GUI.** When a key install (or first-time key
> generation) needs a server password/passphrase, mandos routes the prompt through a
> native macOS dialog via `SSH_ASKPASS` (see the mandos README) — no terminal needed. If
> the key already works, there's no prompt at all.

## Develop

```sh
npm install
npm run tauri dev
```

## Build the distributable

```sh
npm install
npm run tauri build      # .app / .dmg under src-tauri/target/release/bundle/
```

## Validation

```sh
npx tsc --noEmit                     # frontend types
npm run build                        # vite production build
(cd src-tauri && cargo check)        # Tauri/Rust backend
```

## Icon

`public/mandos-logo.png` (framed, app icon/favicon) and `public/mandos-logo-solo.png`
(transparent, in-app on the dark sidebar) are simple placeholder marks. To rebrand,
replace them and regenerate the icon set:

```sh
npx @tauri-apps/cli icon public/mandos-logo.png
```
