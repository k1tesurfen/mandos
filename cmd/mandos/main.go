// Command mandos is the internal CLI for client identity, SSH access and Google Drive
// paths — the shared foundation other tools (wpsite, aule, …) shell out to.
//
// Machine-readable by design: plain output for humans, --json where it helps, clean
// exit codes (e.g. `cloud available` exits non-zero when unavailable).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"mandos/client"
	"mandos/config"
	"mandos/drive"
	"mandos/remote"
)

const version = "0.1.0"

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		os.Exit(1)
	}
	switch args[0] {
	case "client":
		cmdClient(args[1:])
	case "ssh":
		cmdSSH(args[1:])
	case "cloud":
		cmdCloud(args[1:])
	case "config":
		cmdConfig(args[1:])
	case "version", "-v", "--version":
		fmt.Println("mandos", version)
	case "help", "-h", "--help":
		usage()
	default:
		die("unknown command %q (try: mandos help)", args[0])
	}
}

// ---------------------------------------------------------------------------
// client
// ---------------------------------------------------------------------------

func cmdClient(args []string) {
	if len(args) == 0 {
		die("usage: mandos client <list|get|set|add|remove|setup-key|has> …")
	}
	switch args[0] {
	case "list":
		clientList(args[1:])
	case "get":
		clientGet(args[1:])
	case "set":
		clientSet(args[1:])
	case "add":
		clientAdd(args[1:])
	case "remove", "rm":
		clientRemove(args[1:])
	case "setup-key":
		clientSetupKey(args[1:])
	case "has":
		clientHas(args[1:])
	default:
		die("unknown client subcommand %q", args[0])
	}
}

func clientList(args []string) {
	_, asJSON := popBool(args, "--json")
	names, err := client.List()
	if err != nil {
		die("%v", err)
	}
	if asJSON {
		printJSON(names)
		return
	}
	for _, n := range names {
		fmt.Println(n)
	}
}

func clientGet(args []string) {
	args, asJSON := popBool(args, "--json")
	if len(args) < 1 {
		die("usage: mandos client get <name> [<key>] [--json]")
	}
	name := args[0]
	if len(args) >= 2 { // single field
		v, err := client.GetField(name, args[1])
		if err != nil {
			die("%v", err)
		}
		fmt.Println(v)
		return
	}
	m, err := client.GetMap(name)
	if err != nil {
		die("%v", err)
	}
	if asJSON {
		printJSON(m)
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%s: %v\n", k, m[k])
	}
}

func clientSet(args []string) {
	if len(args) != 3 {
		die("usage: mandos client set <name> <key> <value>")
	}
	if err := client.Set(args[0], args[1], args[2]); err != nil {
		die("%v", err)
	}
}

func clientAdd(args []string) {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		die("usage: mandos client add <name> --ssh <user@host> --wp-root <path> [--remote-tmp p] [--local-host h] [--cloud-folder f] [--cloud-dir d] [--domain d] [--identity key] [--no-key]")
	}
	name := args[0]
	fs := flag.NewFlagSet("client add", flag.ExitOnError)
	sshTarget := fs.String("ssh", "", "SSH target (user@host)")
	wpRoot := fs.String("wp-root", "", "absolute path to the WordPress install")
	remoteTmp := fs.String("remote-tmp", "", "remote staging dir override")
	localHost := fs.String("local-host", "", "local .test host override")
	cloudFolder := fs.String("cloud-folder", "", "portable Drive project-folder name")
	cloudDir := fs.String("cloud-dir", "", "absolute Drive backup dir (machine-specific)")
	domain := fs.String("domain", "", "production domain")
	identity := fs.String("identity", "", "SSH private key to install")
	noKey := fs.Bool("no-key", false, "skip installing the SSH key")
	_ = fs.Parse(args[1:])

	if !client.ValidName(name) {
		die("invalid client name %q (lowercase letters, digits, hyphens; not starting/ending with '-')", name)
	}
	if *sshTarget == "" || *wpRoot == "" {
		die("--ssh and --wp-root are required")
	}
	if !filepath.IsAbs(*wpRoot) {
		die("--wp-root must be an absolute path, got %q", *wpRoot)
	}
	if exists, err := client.Has(name); err != nil {
		die("%v", err)
	} else if exists {
		die("client %q already exists", name)
	}

	// Write the entry first (recoverable/editable even if key install later fails).
	must(client.Set(name, "ssh", *sshTarget))
	must(client.Set(name, "wp_root", *wpRoot))
	for key, val := range map[string]string{
		"remote_tmp":   *remoteTmp,
		"local_host":   *localHost,
		"cloud_folder": *cloudFolder,
		"cloud_dir":    *cloudDir,
		"domain":       *domain,
	} {
		if val != "" {
			must(client.Set(name, key, val))
		}
	}
	fmt.Printf("Added client %q.\n", name)

	if *noKey {
		return
	}
	fmt.Printf("Installing SSH key on %s …\n", *sshTarget)
	if err := remote.SetupKey(*sshTarget, *identity); err != nil {
		// Non-fatal: the entry is kept; fix access and re-run `mandos client setup-key`.
		warn("SSH key install failed: %v", err)
		warn("the client entry was kept — fix access and run: mandos client setup-key %s", name)
	}
}

func clientRemove(args []string) {
	if len(args) != 1 {
		die("usage: mandos client remove <name>")
	}
	if err := client.Remove(args[0]); err != nil {
		die("%v", err)
	}
	fmt.Printf("Removed client %q.\n", args[0])
}

func clientSetupKey(args []string) {
	fs := flag.NewFlagSet("client setup-key", flag.ExitOnError)
	identity := fs.String("identity", "", "SSH private key to install")
	_ = fs.Parse(args)
	rest := fs.Args()
	if len(rest) != 1 {
		die("usage: mandos client setup-key <name> [--identity <key>]")
	}
	target, err := client.GetField(rest[0], "ssh")
	if err != nil {
		die("%v", err)
	}
	if target == "" {
		die("client %q has no ssh target", rest[0])
	}
	if err := remote.SetupKey(target, *identity); err != nil {
		die("%v", err)
	}
	fmt.Printf("SSH key OK for %s.\n", target)
}

func clientHas(args []string) {
	if len(args) != 1 {
		die("usage: mandos client has <name>")
	}
	ok, err := client.Has(args[0])
	if err != nil {
		die("%v", err)
	}
	if !ok {
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// ssh
// ---------------------------------------------------------------------------

func cmdSSH(args []string) {
	if len(args) < 1 {
		die("usage: mandos ssh <name> [--] <command> [args…]")
	}
	name := args[0]
	rest := args[1:]
	if len(rest) > 0 && rest[0] == "--" {
		rest = rest[1:]
	}
	target, err := client.GetField(name, "ssh")
	if err != nil {
		die("%v", err)
	}
	if target == "" {
		die("client %q has no ssh target", name)
	}
	err = remote.Run(target, rest...)
	remote.CloseMux()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		die("%v", err)
	}
}

// ---------------------------------------------------------------------------
// cloud
// ---------------------------------------------------------------------------

func cmdCloud(args []string) {
	if len(args) < 2 {
		die("usage: mandos cloud <path|available> <name>")
	}
	c, err := client.Get(args[1])
	if err != nil {
		die("%v", err)
	}
	switch args[0] {
	case "path":
		dir, err := drive.ClientCloudDir(c)
		if err != nil {
			die("%v", err)
		}
		fmt.Println(dir)
	case "available":
		if drive.Available(c) {
			fmt.Println("available")
			return
		}
		fmt.Println("unavailable")
		os.Exit(1)
	default:
		die("unknown cloud subcommand %q", args[0])
	}
}

// ---------------------------------------------------------------------------
// config
// ---------------------------------------------------------------------------

func cmdConfig(args []string) {
	if len(args) == 0 {
		die("usage: mandos config <path|init> …")
	}
	switch args[0] {
	case "path":
		configPath()
	case "init":
		configInit(args[1:])
	default:
		die("unknown config subcommand %q", args[0])
	}
}

func configPath() {
	fmt.Printf("local:      %s\n", config.LocalPath())
	team, err := config.TeamPath()
	if err != nil {
		die("%v", err)
	}
	if team == "" {
		fmt.Println("team:       (none — clients live in the local file)")
	} else {
		status := "ok"
		if _, err := os.Stat(team); err != nil {
			status = "MISSING (Drive unmounted?)"
		}
		fmt.Printf("team:       %s  [%s]\n", team, status)
	}
	base, _ := config.CloudBase()
	if base == "" {
		fmt.Println("cloud_base: (unset)")
	} else {
		fmt.Printf("cloud_base: %s\n", base)
	}
}

func configInit(args []string) {
	fs := flag.NewFlagSet("config init", flag.ExitOnError)
	team := fs.String("team-config", "", "path to the shared clients YAML on Drive")
	cloudBase := fs.String("cloud-base", "", "Google Drive projects root")
	_ = fs.Parse(args)

	l, err := config.LoadLocal()
	if err != nil {
		die("%v", err)
	}
	if *team != "" {
		l.TeamConfig = *team
	}
	if *cloudBase != "" {
		l.CloudBase = *cloudBase
	}
	if err := config.WriteLocal(l); err != nil {
		die("%v", err)
	}
	fmt.Printf("Wrote %s\n", config.LocalPath())
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// popBool removes a boolean flag (e.g. --json) from args wherever it appears, so it
// can follow positionals — Go's flag package stops at the first positional.
func popBool(args []string, name string) ([]string, bool) {
	out := make([]string, 0, len(args))
	found := false
	for _, a := range args {
		if a == name {
			found = true
			continue
		}
		out = append(out, a)
	}
	return out, found
}

func printJSON(v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		die("%v", err)
	}
	fmt.Println(string(b))
}

func must(err error) {
	if err != nil {
		die("%v", err)
	}
}

func warn(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "mandos: warning: "+format+"\n", a...)
}

func die(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "mandos: "+format+"\n", a...)
	os.Exit(1)
}

func usage() {
	fmt.Print(`mandos — internal client identity, SSH access & Google Drive paths

Usage: mandos <command> …

  client list [--json]                       list client names
  client get <name> [<key>] [--json]         show a client (all fields, or one)
  client set <name> <key> <value>            set one field (comment-preserving)
  client add <name> --ssh <u@h> --wp-root <p> [--remote-tmp|--local-host|
             --cloud-folder|--cloud-dir|--domain <v>] [--identity <key>] [--no-key]
  client remove <name>                       delete a client entry
  client setup-key <name> [--identity <key>] (re)install your SSH key on the server
  client has <name>                          exit 0 if the client exists, else 1

  ssh <name> [--] <command> [args…]          run a command on the client's server (muxed)

  cloud path <name>                          print the client's Drive backup dir
  cloud available <name>                     exit 0 if reachable, else 1

  config path                                show resolved config + cloud paths
  config init [--team-config p] [--cloud-base p]   write the local config

  version | help

Env overrides: MANDOS_CONFIG, MANDOS_TEAM_CONFIG, MANDOS_CLOUD_BASE, MANDOS_CLOUD_SUBDIR
`)
}
