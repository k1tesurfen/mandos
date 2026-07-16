use tauri::Emitter;
use std::collections::HashMap;
use std::process::{Command, Stdio};
use std::io::{BufRead, BufReader};

const MANDOS_BIN: &str = "/usr/local/bin/mandos";
const PATH_ENV: &str = "/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin";

// Set the macOS Dock icon at runtime. Needed because `tauri dev` launches a bare
// debug binary (no .app bundle), so the configured bundle icon isn't applied and
// the Dock shows a generic executable icon. The PNG is compiled into the binary.
#[cfg(target_os = "macos")]
fn set_dock_icon() {
    use objc2::{AnyThread, MainThreadMarker};
    use objc2_app_kit::{NSApplication, NSImage};
    use objc2_foundation::NSData;

    // Must run on the main thread (where the Tauri setup hook executes).
    let Some(mtm) = MainThreadMarker::new() else { return };

    const LOGO: &[u8] = include_bytes!("../icons/128x128@2x.png");
    let data = NSData::with_bytes(LOGO);
    let Some(image) = NSImage::initWithData(NSImage::alloc(), &data) else { return };

    let app = NSApplication::sharedApplication(mtm);
    // SAFETY: standard AppKit call on the main thread with a valid NSImage.
    unsafe { app.setApplicationIconImage(Some(&image)) };
}

// ---------------------------------------------------------------------------
// Read-only queries (synchronous — mandos answers fast)
// ---------------------------------------------------------------------------

#[tauri::command]
fn get_clients() -> Result<Vec<String>, String> {
    // The registry is the source of truth (a shared YAML on Google Drive). `mandos
    // client list` prints one name per line and degrades to empty (stderr warning)
    // when Drive is unmounted.
    let output = Command::new(MANDOS_BIN)
        .args(["client", "list"])
        .env("PATH", PATH_ENV)
        .output();

    match output {
        Ok(out) => {
            let stdout = String::from_utf8_lossy(&out.stdout);
            Ok(stdout
                .lines()
                .map(|s| s.trim().to_string())
                .filter(|s| !s.is_empty())
                .collect())
        }
        Err(e) => Err(format!("mandos konnte nicht ausgeführt werden: {}", e)),
    }
}

// All fields of one client as a JSON object (`mandos client get <name> --json`).
#[tauri::command]
fn get_client(name: String) -> Result<serde_json::Value, String> {
    let output = Command::new(MANDOS_BIN)
        .args(["client", "get", &name, "--json"])
        .env("PATH", PATH_ENV)
        .output()
        .map_err(|e| format!("mandos konnte nicht ausgeführt werden: {}", e))?;

    if !output.status.success() {
        return Err(String::from_utf8_lossy(&output.stderr).trim().to_string());
    }
    let stdout = String::from_utf8_lossy(&output.stdout);
    serde_json::from_str::<serde_json::Value>(&stdout)
        .map_err(|e| format!("Antwort von mandos ließ sich nicht lesen: {}", e))
}

// Resolved local/team/cloud config (`mandos config get …` + a stat for the team file).
#[derive(serde::Serialize)]
struct ConfigInfo {
    local: String,
    team_config: String,
    team_status: String, // "ok" | "missing" | "none"
    cloud_base: String,
}

fn mandos_get(key: &str) -> String {
    Command::new(MANDOS_BIN)
        .args(["config", "get", key])
        .env("PATH", PATH_ENV)
        .output()
        .ok()
        .filter(|o| o.status.success())
        .map(|o| String::from_utf8_lossy(&o.stdout).trim().to_string())
        .unwrap_or_default()
}

// A client's resolved Drive backup dir + whether it's currently reachable.
#[derive(serde::Serialize)]
struct CloudInfo {
    path: String,
    available: bool,
}

#[tauri::command]
fn cloud_info(name: String) -> Result<CloudInfo, String> {
    // `cloud path` resolves the backup dir from the client's meta/registry + the Drive
    // root; it errors if the client is unknown. `cloud available` exits 0 only when the
    // project folder actually exists (Drive mounted + folder present).
    let path_out = Command::new(MANDOS_BIN)
        .args(["cloud", "path", &name])
        .env("PATH", PATH_ENV)
        .output()
        .map_err(|e| format!("mandos konnte nicht ausgeführt werden: {}", e))?;
    if !path_out.status.success() {
        return Err(String::from_utf8_lossy(&path_out.stderr).trim().to_string());
    }
    let path = String::from_utf8_lossy(&path_out.stdout).trim().to_string();

    let available = Command::new(MANDOS_BIN)
        .args(["cloud", "available", &name])
        .env("PATH", PATH_ENV)
        .status()
        .map(|s| s.success())
        .unwrap_or(false);

    Ok(CloudInfo { path, available })
}

#[tauri::command]
fn get_config() -> Result<ConfigInfo, String> {
    let local = mandos_get("local");
    let team_config = mandos_get("team-config");
    let cloud_base = mandos_get("cloud-base");
    let team_status = if team_config.is_empty() {
        "none".to_string()
    } else if std::path::Path::new(&team_config).exists() {
        "ok".to_string()
    } else {
        "missing".to_string()
    };
    Ok(ConfigInfo { local, team_config, team_status, cloud_base })
}

// ---------------------------------------------------------------------------
// Registry edits (synchronous — a set/unset loop with a text summary)
// ---------------------------------------------------------------------------

// Apply field changes to a client. Empty value → unset the (optional) key; a
// non-empty value → set it. `ssh`/`wp_root` are required and never unset here
// (the frontend rejects clearing them). Returns a human-readable summary for the
// console; a set failure (e.g. Drive unmounted) aborts and is returned as an error.
#[tauri::command]
fn save_client(name: String, changes: HashMap<String, String>) -> Result<String, String> {
    let mut lines: Vec<String> = Vec::new();
    // Deterministic order so the console reads the same each time.
    let mut keys: Vec<&String> = changes.keys().collect();
    keys.sort();
    for key in keys {
        let val = &changes[key];
        if val.is_empty() {
            if key == "ssh" || key == "wp_root" {
                return Err(format!("Feld „{}“ darf nicht leer sein.", key));
            }
            // Unset is best-effort: clearing an already-absent key is not an error.
            let out = Command::new(MANDOS_BIN)
                .args(["client", "unset", &name, key])
                .env("PATH", PATH_ENV)
                .output()
                .map_err(|e| format!("mandos unset {}: {}", key, e))?;
            if out.status.success() {
                lines.push(format!("  gelöscht: {}", key));
            } else {
                lines.push(format!("  (übersprungen: {})", key));
            }
        } else {
            let out = Command::new(MANDOS_BIN)
                .args(["client", "set", &name, key, val])
                .env("PATH", PATH_ENV)
                .output()
                .map_err(|e| format!("mandos set {}: {}", key, e))?;
            if !out.status.success() {
                return Err(String::from_utf8_lossy(&out.stderr).trim().to_string());
            }
            lines.push(format!("  {} = {}", key, val));
        }
    }
    if lines.is_empty() {
        return Ok("Keine Änderungen.".to_string());
    }
    Ok(format!("Gespeichert für „{}“:\n{}", name, lines.join("\n")))
}

// ---------------------------------------------------------------------------
// Streaming runner (for the commands that talk to a server / Drive)
// ---------------------------------------------------------------------------

// Run `mandos <args…>`, streaming stdout+stderr to the frontend as `mandos-log`
// events and emitting `mandos-finished` at the end. Used for add / remove /
// setup-key / ssh-test / cloud-check / config-init — anything with meaningful live
// output or a non-trivial runtime.
#[tauri::command]
fn run_mandos(app: tauri::AppHandle, args: Vec<String>) -> Result<(), String> {
    tauri::async_runtime::spawn(async move {
        let command_str = format!("mandos {}", args.join(" "));
        let _ = app.emit("mandos-log", format!("$ {}\n", command_str));

        let mut child = match Command::new(MANDOS_BIN)
            .args(&args)
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .env("PATH", PATH_ENV)
            .spawn()
        {
            Ok(c) => c,
            Err(e) => {
                let _ = app.emit("mandos-log", format!("Fehler: mandos konnte nicht gestartet werden: {}\n", e));
                let _ = app.emit("mandos-finished", ());
                return;
            }
        };

        let stdout = child.stdout.take();
        let stderr = child.stderr.take();

        let a1 = app.clone();
        let out_h = std::thread::spawn(move || {
            if let Some(out) = stdout {
                for line in BufReader::new(out).lines().map_while(Result::ok) {
                    let _ = a1.emit("mandos-log", format!("{}\n", line));
                }
            }
        });
        let a2 = app.clone();
        let err_h = std::thread::spawn(move || {
            if let Some(err) = stderr {
                for line in BufReader::new(err).lines().map_while(Result::ok) {
                    let _ = a2.emit("mandos-log", format!("{}\n", line));
                }
            }
        });

        let _ = out_h.join();
        let _ = err_h.join();

        match child.wait() {
            Ok(s) if s.success() => {
                let _ = app.emit("mandos-log", "Befehl erfolgreich abgeschlossen.\n".to_string());
            }
            Ok(s) => {
                let _ = app.emit("mandos-log", format!("Befehl mit Status beendet: {}\n", s));
            }
            Err(e) => {
                let _ = app.emit("mandos-log", format!("Fehler beim Warten auf den Prozess: {}\n", e));
            }
        }

        let _ = app.emit("mandos-finished", ());
    });

    Ok(())
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_opener::init())
        .setup(|_app| {
            #[cfg(target_os = "macos")]
            set_dock_icon();
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            get_clients,
            get_client,
            get_config,
            cloud_info,
            save_client,
            run_mandos
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
