import { useState, useEffect, useRef } from "react";
import { invoke } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";
import { RefreshCw, CircleUser, UserPlus, Settings } from "lucide-react";
import "./App.css";

// One editable field of a client. `addFlag` is the `mandos client add` flag name; a
// field without it is edit-only (mandos client add has no flag for it — it's set later
// via `client set`). `required` fields (ssh/wp_root) can never be cleared.
interface FieldDef {
  key: string;
  label: string;
  placeholder: string;
  hint: string;
  required?: boolean;
  addFlag?: string;
}

// Note the German wording: the production site is called "Live", never "Produktion".
const FIELDS: FieldDef[] = [
  { key: "ssh", label: "SSH-Ziel", placeholder: "benutzer@server", required: true, addFlag: "ssh",
    hint: "Benutzer und Server für den SSH-Zugang, z. B. web123@ssh.hoster.de." },
  { key: "wp_root", label: "WordPress-Verzeichnis", placeholder: "/var/www/html", required: true, addFlag: "wp-root",
    hint: "Absoluter Pfad zur WordPress-Installation auf dem Server." },
  { key: "domain", label: "Live-Domain", placeholder: "beispiel.de", addFlag: "domain",
    hint: "Die Live-Domain der Website – wird für die Cloud-Zuordnung genutzt." },
  { key: "local_host", label: "Lokaler .test-Host", placeholder: "kunde.test", addFlag: "local-host",
    hint: "Überschreibt den lokalen Host der Kopie (Standard: aus der Domain abgeleitet)." },
  { key: "cloud_folder", label: "Cloud-Projektordner", placeholder: "beispiel.de", addFlag: "cloud-folder",
    hint: "Name des Projektordners in Google Drive (portabel – bevorzugt gegenüber dem festen Pfad)." },
  { key: "cloud_dir", label: "Cloud-Backup-Pfad", placeholder: "/…/LIVE_WEB/…/100_Backup", addFlag: "cloud-dir",
    hint: "Absoluter Backup-Pfad in Drive (maschinenspezifisch – nur wenn nötig)." },
  { key: "remote_tmp", label: "Temp-Verzeichnis (Server)", placeholder: "/tmp", addFlag: "remote-tmp",
    hint: "Überschreibt das temporäre Verzeichnis auf dem Server während des Backups." },
  { key: "login_path", label: "WP-Login-Pfad", placeholder: "/wp-admin/",
    hint: "Abweichender Login-Pfad, z. B. bei WPS Hide Login. (Nur über Bearbeiten setzbar.)" },
];

// A DNS-label-safe client name (mirrors mandos ValidName): lowercase letters, digits
// and hyphens, not starting/ending with a hyphen.
const NAME_RE = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/;

type View = "client" | "new" | "config";

interface ConfigInfo {
  local: string;
  team_config: string;
  team_status: string; // ok | missing | none
  cloud_base: string;
}

interface CloudInfo {
  path: string;
  available: boolean;
}

const emptyRecord = (): Record<string, string> =>
  Object.fromEntries(FIELDS.map((f) => [f.key, ""]));

function App() {
  const [clients, setClients] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [view, setView] = useState<View>("config");
  const [selected, setSelected] = useState<string | null>(null);

  const [logs, setLogs] = useState<string>("");
  const [isRunning, setIsRunning] = useState<boolean>(false);

  // Selected client's fields (editable) + the last-loaded values (for the dirty diff).
  const [fields, setFields] = useState<Record<string, string>>(emptyRecord());
  const [orig, setOrig] = useState<Record<string, string>>(emptyRecord());

  // "Neuer Kunde" form
  const [addName, setAddName] = useState<string>("");
  const [addFields, setAddFields] = useState<Record<string, string>>(emptyRecord());
  const [addInstallKey, setAddInstallKey] = useState<boolean>(true);

  // Config form
  const [config, setConfig] = useState<ConfigInfo | null>(null);
  const [cfgTeam, setCfgTeam] = useState<string>("");
  const [cfgCloud, setCfgCloud] = useState<string>("");

  // Destructive confirm (remove) — type the client name
  const [confirmRemove, setConfirmRemove] = useState<string | null>(null);
  const [confirmInput, setConfirmInput] = useState<string>("");

  const terminalRef = useRef<HTMLPreElement>(null);

  const log = (s: string) => setLogs((prev) => prev + s);

  // ---- data loading -------------------------------------------------------

  const refreshClients = async () => {
    try {
      const cl = await invoke<string[]>("get_clients");
      setClients(cl);
      setError(null);
      // Drop a selection that has vanished (e.g. the client was removed).
      setSelected((prev) => (prev && cl.includes(prev) ? prev : null));
    } catch (err: any) {
      setError(err.toString());
    }
  };

  const loadClientFields = async (name: string) => {
    try {
      const data = await invoke<Record<string, any>>("get_client", { name });
      const rec = emptyRecord();
      for (const f of FIELDS) {
        if (data[f.key] != null) rec[f.key] = String(data[f.key]);
      }
      setFields(rec);
      setOrig(rec);
    } catch (err: any) {
      log(`\n[Fehler] Kunde konnte nicht geladen werden: ${err}\n`);
    }
  };

  const loadConfig = async () => {
    try {
      const c = await invoke<ConfigInfo>("get_config");
      setConfig(c);
      setCfgTeam(c.team_config);
      setCfgCloud(c.cloud_base);
    } catch (err: any) {
      log(`\n[Fehler] Konfiguration konnte nicht geladen werden: ${err}\n`);
    }
  };

  useEffect(() => {
    refreshClients();
    loadConfig();
  }, []);

  // Load the selected client's fields whenever the selection changes. Skipped while a
  // command runs — when it finishes (isRunning → false) this re-fires and loads the
  // fresh values, so a just-added client isn't queried before `client add` completes.
  useEffect(() => {
    if (view === "client" && selected && !isRunning) loadClientFields(selected);
  }, [selected, view, isRunning]);

  // Backend event stream (logs + finished). The `cancelled` guard prevents the
  // classic React.StrictMode double-listener (which doubles every logged line).
  useEffect(() => {
    let cancelled = false;
    const unlisteners: Array<() => void> = [];
    async function setup() {
      const un1 = await listen<string>("mandos-log", (e) => setLogs((p) => p + e.payload));
      const un2 = await listen<void>("mandos-finished", () => {
        // Flipping isRunning false re-fires the field-load effect for the selected client.
        setIsRunning(false);
        refreshClients();
        loadConfig();
      });
      if (cancelled) { un1(); un2(); return; }
      unlisteners.push(un1, un2);
    }
    setup();
    return () => { cancelled = true; unlisteners.forEach((u) => u()); };
  }, []);

  useEffect(() => {
    if (terminalRef.current) terminalRef.current.scrollTop = terminalRef.current.scrollHeight;
  }, [logs]);

  // ---- command helpers ----------------------------------------------------

  // Streaming command (add/remove/setup-key/ssh/config-init): the runner emits
  // mandos-finished, which clears isRunning + refreshes.
  const runStream = async (args: string[]) => {
    if (isRunning) return;
    setIsRunning(true);
    try {
      await invoke("run_mandos", { args });
    } catch (err: any) {
      log(`\n[GUI-Fehler] Befehl konnte nicht gestartet werden: ${err}\n`);
      setIsRunning(false);
    }
  };

  // ---- client detail actions ----------------------------------------------

  const changed: Record<string, string> = {};
  for (const f of FIELDS) {
    const cur = (fields[f.key] || "").trim();
    const was = (orig[f.key] || "").trim();
    if (cur !== was) changed[f.key] = cur;
  }
  const dirty = Object.keys(changed).length > 0;
  const requiredOk = (fields.ssh || "").trim() !== "" && (fields.wp_root || "").trim() !== "";

  const saveClient = async () => {
    if (!selected || !dirty || !requiredOk || isRunning) return;
    setIsRunning(true);
    try {
      const summary = await invoke<string>("save_client", { name: selected, changes: changed });
      log(`\n${summary}\n`);
      await loadClientFields(selected);
      await refreshClients();
    } catch (err: any) {
      log(`\n[Fehler] Speichern fehlgeschlagen: ${err}\n`);
    } finally {
      setIsRunning(false);
    }
  };

  const checkCloud = async () => {
    if (!selected || isRunning) return;
    setIsRunning(true);
    log(`$ mandos cloud path/available ${selected}\n`);
    try {
      const info = await invoke<CloudInfo>("cloud_info", { name: selected });
      log(`Cloud-Pfad: ${info.path}\nErreichbar: ${info.available ? "ja" : "nein (Drive nicht eingehängt oder Projektordner fehlt)"}\n`);
    } catch (err: any) {
      log(`[Fehler] ${err}\n`);
    } finally {
      setIsRunning(false);
    }
  };

  const doRemove = async () => {
    if (!confirmRemove) return;
    if (confirmInput !== confirmRemove) {
      alert(`Bestätigung fehlgeschlagen. Bitte „${confirmRemove}“ genau so eingeben.`);
      return;
    }
    const name = confirmRemove;
    setConfirmRemove(null);
    setConfirmInput("");
    if (selected === name) setSelected(null);
    await runStream(["client", "remove", name]);
  };

  // ---- new client ----------------------------------------------------------

  const addNameValid = NAME_RE.test(addName);
  const addNameTaken = clients.includes(addName);
  const addRequiredOk = (addFields.ssh || "").trim() !== "" && (addFields.wp_root || "").trim() !== "";
  const addReady = addNameValid && !addNameTaken && addRequiredOk;

  const submitAdd = async () => {
    if (!addReady || isRunning) return;
    const args = ["client", "add", addName];
    for (const f of FIELDS) {
      if (!f.addFlag) continue;
      const v = (addFields[f.key] || "").trim();
      if (v !== "") args.push(`--${f.addFlag}`, v);
    }
    if (!addInstallKey) args.push("--no-key");
    // Reset the form; the new client will appear after refresh, then select it.
    const newName = addName;
    setAddName("");
    setAddFields(emptyRecord());
    setAddInstallKey(true);
    await runStream(args);
    setSelected(newName);
    setView("client");
  };

  // ---- config --------------------------------------------------------------

  const saveConfig = async () => {
    const args = ["config", "init"];
    if (cfgTeam.trim() !== "") args.push("--team-config", cfgTeam.trim());
    if (cfgCloud.trim() !== "") args.push("--cloud-base", cfgCloud.trim());
    if (args.length === 2) {
      log("\n[Hinweis] Bitte mindestens Team-Konfiguration oder Cloud-Basis angeben.\n");
      return;
    }
    await runStream(args);
  };

  // ---- selection handlers --------------------------------------------------

  const selectClient = (name: string) => {
    if (isRunning) return;
    setSelected(name);
    setView("client");
  };
  const goNew = () => { if (!isRunning) { setView("new"); } };
  const goConfig = () => { if (!isRunning) { setView("config"); } };

  // ---- header text ---------------------------------------------------------

  const headerTitle =
    view === "client" ? `Kunde: ${selected}` : view === "new" ? "Neuer Kunde" : "Konfiguration";
  const headerSubtitle =
    view === "client"
      ? "SSH-Zugang und Cloud-Zuordnung dieses Kunden verwalten"
      : view === "new"
      ? "Einen SSH-Kunden anlegen und den SSH-Schlüssel installieren"
      : "Diesen Rechner mit der geteilten Kunden-Registry und Google Drive verbinden";

  // ---- field input helper --------------------------------------------------

  const FieldInput = (
    rec: Record<string, string>,
    setRec: (r: Record<string, string>) => void,
    f: FieldDef,
    editOnly: boolean,
  ) => (
    <label className="field-row" key={f.key}>
      <span className="field-label">
        {f.label}
        {f.required && <em className="req">*</em>}
        {editOnly && !f.addFlag && <span className="field-tag">nur Bearbeiten</span>}
      </span>
      <input
        className="field-input"
        type="text"
        value={rec[f.key] || ""}
        placeholder={f.placeholder}
        onChange={(e) => setRec({ ...rec, [f.key]: e.target.value })}
        spellCheck={false}
        autoCapitalize="off"
        autoCorrect="off"
      />
      <span className="field-hint">{f.hint}</span>
    </label>
  );

  return (
    <div className="app-container">
      {/* Sidebar */}
      <aside className="sidebar">
        <div className="sidebar-header">
          <div className="app-brand">
            <img className="brand-logo" src="/mandos-logo-solo.png" alt="mandos" />
            <h1 className="brand-title">mandos</h1>
          </div>
        </div>

        <nav className="sidebar-nav">
          <div className="nav-section">
            <div className="section-header">
              <span>KUNDEN</span>
              <button onClick={refreshClients} disabled={isRunning} className="refresh-btn" title="Kundenliste neu laden">
                <RefreshCw size={18} strokeWidth={2} />
              </button>
            </div>
            <ul className="nav-list">
              {clients.map((name) => (
                <li key={name}>
                  <button
                    className={`nav-item ${view === "client" && selected === name ? "active" : ""}`}
                    onClick={() => selectClient(name)}
                    disabled={isRunning}
                  >
                    <span className="client-icon"><CircleUser size={16} strokeWidth={2} /></span>
                    <span className="client-name">{name}</span>
                  </button>
                </li>
              ))}
              {clients.length === 0 && !error && (
                <li className="empty-state">Keine Kunden – ist Google Drive verbunden?</li>
              )}
              {error && <li className="error-state" title={error}>Registry konnte nicht geladen werden</li>}
            </ul>
          </div>

          <div className="nav-section">
            <div className="section-header">SYSTEM</div>
            <ul className="nav-list">
              <li>
                <button className={`nav-item ${view === "new" ? "active" : ""}`} onClick={goNew} disabled={isRunning}>
                  <span className="client-icon"><UserPlus size={16} strokeWidth={2} /></span>
                  <span>Neuer Kunde</span>
                </button>
              </li>
              <li>
                <button className={`nav-item ${view === "config" ? "active" : ""}`} onClick={goConfig} disabled={isRunning}>
                  <span className="client-icon"><Settings size={16} strokeWidth={2} /></span>
                  <span>Konfiguration</span>
                </button>
              </li>
            </ul>
          </div>
        </nav>
      </aside>

      {/* Main */}
      <main className="main-content">
        <header className="content-header">
          <div className="header-info">
            <h2 className="current-title">{headerTitle}</h2>
            <p className="current-subtitle">{headerSubtitle}</p>
          </div>
          <div className="status-indicator">
            {isRunning ? (
              <div className="status-badge running"><span className="spinner" />Befehl läuft</div>
            ) : (
              <div className="status-badge idle"><span className="dot" />Bereit</div>
            )}
          </div>
        </header>

        <section className="actions-section">
          {/* ---- Client detail ---- */}
          {view === "client" && selected && (
            <div className="detail">
              <div className="detail-card">
                <div className="detail-card-title">Stammdaten</div>
                <div className="field-grid">
                  {FIELDS.map((f) => FieldInput(fields, setFields, f, true))}
                </div>
                {!requiredOk && <span className="form-hint error">SSH-Ziel und WordPress-Verzeichnis dürfen nicht leer sein.</span>}
                <div className="detail-actions">
                  <button className="btn-primary" onClick={saveClient} disabled={!dirty || !requiredOk || isRunning}>
                    Speichern
                  </button>
                  {dirty && (
                    <button className="btn-cancel" onClick={() => setFields(orig)} disabled={isRunning}>
                      Zurücksetzen
                    </button>
                  )}
                </div>
              </div>

              <div className="actions-grid">
                <button className="action-card" onClick={() => runStream(["client", "setup-key", selected])} disabled={isRunning}>
                  <div className="action-card-header">
                    <span className="action-title">SSH-Schlüssel installieren</span>
                    <span className="action-cmd">mandos client setup-key</span>
                  </div>
                  <p className="action-desc">Installiert deinen SSH-Schlüssel auf dem Server (per ssh-copy-id). Falls ein Server-Passwort nötig ist, erscheint ein Passwort-Dialog.</p>
                </button>

                <button className="action-card" onClick={() => runStream(["ssh", selected, "--", "uname", "-a"])} disabled={isRunning}>
                  <div className="action-card-header">
                    <span className="action-title">SSH testen</span>
                    <span className="action-cmd">mandos ssh … -- uname -a</span>
                  </div>
                  <p className="action-desc">Prüft die SSH-Verbindung zum Server, indem ein harmloser Befehl ausgeführt wird.</p>
                </button>

                <button className="action-card" onClick={checkCloud} disabled={isRunning}>
                  <div className="action-card-header">
                    <span className="action-title">Cloud prüfen</span>
                    <span className="action-cmd">mandos cloud path/available</span>
                  </div>
                  <p className="action-desc">Zeigt den Backup-Ordner dieses Kunden in Google Drive und ob er gerade erreichbar ist.</p>
                </button>

                <button
                  className="action-card destructive"
                  onClick={() => { setConfirmRemove(selected); setConfirmInput(""); }}
                  disabled={isRunning}
                >
                  <div className="action-card-header">
                    <span className="action-title">Entfernen</span>
                    <span className="action-cmd">mandos client remove</span>
                  </div>
                  <p className="action-desc">Löscht den Kunden aus der geteilten Registry. Backups in der Cloud bleiben erhalten. Unwiderruflich.</p>
                </button>
              </div>
            </div>
          )}

          {/* ---- New client ---- */}
          {view === "new" && (
            <div className="detail">
              <div className="detail-card">
                <div className="detail-card-title">Kundendaten</div>
                <label className="field-row">
                  <span className="field-label">Name<em className="req">*</em></span>
                  <input
                    className="field-input"
                    type="text"
                    value={addName}
                    placeholder="z. B. acme"
                    onChange={(e) => setAddName(e.target.value)}
                    spellCheck={false}
                    autoCapitalize="off"
                    autoCorrect="off"
                    autoFocus
                  />
                  {addName.length > 0 && !addNameValid && (
                    <span className="field-hint error">Nur Kleinbuchstaben, Ziffern und Bindestriche (nicht am Anfang/Ende).</span>
                  )}
                  {addNameValid && addNameTaken && (
                    <span className="field-hint error">„{addName}“ ist bereits vergeben.</span>
                  )}
                  {!(addName.length > 0 && !addNameValid) && !(addNameValid && addNameTaken) && (
                    <span className="field-hint">Kurzer, eindeutiger Bezeichner – wird zum Registry-Schlüssel.</span>
                  )}
                </label>
                <div className="field-grid">
                  {FIELDS.filter((f) => f.addFlag).map((f) => FieldInput(addFields, setAddFields, f, false))}
                </div>
                <label className="checkbox-row">
                  <input type="checkbox" checked={addInstallKey} onChange={(e) => setAddInstallKey(e.target.checked)} />
                  <span>SSH-Schlüssel jetzt installieren (ssh-copy-id)</span>
                </label>
                <div className="detail-actions">
                  <button className="btn-primary" onClick={submitAdd} disabled={!addReady || isRunning}>
                    Kunde anlegen
                  </button>
                </div>
              </div>
            </div>
          )}

          {/* ---- Config ---- */}
          {view === "config" && (
            <div className="detail">
              <div className="detail-card">
                <div className="detail-card-title">Aktuelle Konfiguration</div>
                <dl className="config-list">
                  <div className="config-row">
                    <dt>Lokale Datei</dt>
                    <dd><code>{config?.local || "…"}</code></dd>
                  </div>
                  <div className="config-row">
                    <dt>Team-Konfiguration</dt>
                    <dd>
                      <code>{config?.team_config || "(keine)"}</code>
                      {config && config.team_status !== "none" && (
                        <span className={`cfg-badge ${config.team_status === "ok" ? "ok" : "bad"}`}>
                          {config.team_status === "ok" ? "erreichbar" : "fehlt (Drive nicht eingehängt?)"}
                        </span>
                      )}
                    </dd>
                  </div>
                  <div className="config-row">
                    <dt>Cloud-Basis</dt>
                    <dd><code>{config?.cloud_base || "(nicht gesetzt)"}</code></dd>
                  </div>
                </dl>
              </div>

              <div className="detail-card">
                <div className="detail-card-title">Einrichten</div>
                <div className="field-grid">
                  <label className="field-row">
                    <span className="field-label">Team-Konfiguration</span>
                    <input
                      className="field-input"
                      type="text"
                      value={cfgTeam}
                      placeholder="/…/Google Drive/…/mandos/mandos.team.yml"
                      onChange={(e) => setCfgTeam(e.target.value)}
                      spellCheck={false}
                    />
                    <span className="field-hint">Pfad zur geteilten Kunden-Registry (YAML) in Google Drive.</span>
                  </label>
                  <label className="field-row">
                    <span className="field-label">Cloud-Basis</span>
                    <input
                      className="field-input"
                      type="text"
                      value={cfgCloud}
                      placeholder="/…/Google Drive/…/LIVE_WEB"
                      onChange={(e) => setCfgCloud(e.target.value)}
                      spellCheck={false}
                    />
                    <span className="field-hint">Projekte-Wurzel in Drive – darunter liegen die Kunden-Projektordner.</span>
                  </label>
                </div>
                <div className="detail-actions">
                  <button className="btn-primary" onClick={saveConfig} disabled={isRunning}>
                    Speichern
                  </button>
                </div>
              </div>
            </div>
          )}
        </section>

        {/* Console */}
        <section className="terminal-section">
          <div className="terminal-header">
            <span className="terminal-title">KONSOLENAUSGABE</span>
            <div className="terminal-actions">
              <button onClick={() => setLogs("")} className="btn-clear-logs" title="Konsole leeren">Leeren</button>
            </div>
          </div>
          <div className="terminal-body">
            <pre ref={terminalRef} className="terminal-pre">
              {logs || "Die Konsole ist leer. Führe eine Aktion aus, um die Ausgabe zu sehen …"}
            </pre>
          </div>
        </section>
      </main>

      {/* Remove confirmation overlay */}
      {confirmRemove && (
        <div className="modal-backdrop" onClick={() => setConfirmRemove(null)}>
          <div className="confirm-overlay" onClick={(e) => e.stopPropagation()}>
            <div className="confirm-card">
              <h3>⚠️ Kunde entfernen</h3>
              <p>
                Du bist dabei, den Kunden <strong>{confirmRemove}</strong> unwiderruflich aus der geteilten Registry zu löschen.
              </p>
              <p className="confirm-desc">Backups in der Cloud bleiben erhalten. Der Eintrag (SSH-Zugang, Cloud-Zuordnung) geht verloren.</p>
              <div className="confirm-form">
                <label htmlFor="confirm-input">Zum Bestätigen bitte <strong>{confirmRemove}</strong> eingeben:</label>
                <input
                  id="confirm-input"
                  type="text"
                  value={confirmInput}
                  onChange={(e) => setConfirmInput(e.target.value)}
                  placeholder={confirmRemove}
                  autoFocus
                />
                <div className="confirm-buttons">
                  <button className="btn-cancel" onClick={() => setConfirmRemove(null)}>Abbrechen</button>
                  <button className="btn-danger" onClick={doRemove} disabled={confirmInput !== confirmRemove}>
                    Entfernen
                  </button>
                </div>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

export default App;
