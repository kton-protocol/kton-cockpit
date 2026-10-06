package cli

// configcmd.go is `cockpit config`: this repository's configuration, read and changed one value at a
// time, without opening the JSON.
//
//	cockpit config                         the settings that shape what the verbs do
//	cockpit config git.push                one value, as JSON
//	cockpit config git.push off            set it (on/off, true/false, a number, JSON, or text)
//	cockpit config execution.network --unset
//
// A change is written, then the configuration is loaded the way every verb loads it. If that
// refuses — a binding that no longer matches the remote, a private key in a tier — the file is put
// back and the reason is said. Trust tiers, the identity, the image and the sources have their own
// commands (trust, keygen, pin, federation), which also check what they take in.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kton-protocol/kton-cockpit/internal/config"
)

func runConfig(ctx context.Context, args []string) error {
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
	}
	cfgPath := filepath.Join(cfg.RepoRoot, "cockpit.config.json")
	switch len(args) {
	case 0:
		configOverview(cfg, cfgPath)
		return nil
	case 1:
		b, _ := os.ReadFile(cfgPath)
		v, ok := getPath(b, strings.Split(args[0], "."))
		if !ok {
			fmt.Printf("not set — the default applies (cockpit config shows it)\n")
			return nil
		}
		var out bytes.Buffer
		json.Indent(&out, v, "", "  ")
		fmt.Println(out.String())
		return nil
	case 2:
	default:
		return fmt.Errorf("usage: cockpit config [key [value|--unset]]")
	}

	key, raw := args[0], args[1]
	path := strings.Split(key, ".")
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}
	edit := set(path, parseValue(raw))
	if raw == "--unset" {
		edit = func(b []byte) ([]byte, error) { return unsetPath(b, path) }
	}
	if err := editConfig(cfgPath, edit); err != nil {
		return err
	}
	if _, err := config.Load(ctx, "."); err != nil {
		os.WriteFile(cfgPath, before, 0o644)
		return fmt.Errorf("%v\n(cockpit.config.json is unchanged)", strings.TrimPrefix(err.Error(), "cockpit: "))
	}
	b, _ := os.ReadFile(cfgPath)
	if v, ok := getPath(b, path); ok {
		fmt.Printf("%s = %s\n", key, v)
	} else {
		fmt.Printf("%s unset\n", key)
	}
	fmt.Println("  commit cockpit.config.json to keep it")
	return nil
}

func parseValue(s string) any {
	switch strings.ToLower(s) {
	case "on", "yes":
		return true
	case "off", "no":
		return false
	}
	var v any
	if json.Unmarshal([]byte(s), &v) == nil {
		return v
	}
	return s
}

func getPath(doc []byte, path []string) (json.RawMessage, bool) {
	cur := json.RawMessage(doc)
	for _, k := range path {
		o, err := parseObject(cur)
		if err != nil {
			return nil, false
		}
		found := false
		for _, m := range o {
			if m.key == k {
				cur, found = m.val, true
			}
		}
		if !found {
			return nil, false
		}
	}
	return cur, true
}

func unsetPath(doc []byte, path []string) ([]byte, error) {
	o, err := parseObject(doc)
	if err != nil {
		return nil, err
	}
	for i := range o {
		if o[i].key != path[0] {
			continue
		}
		if len(path) == 1 {
			return append(o[:i:i], o[i+1:]...).marshal()
		}
		v, err := unsetPath(o[i].val, path[1:])
		if err != nil {
			return nil, err
		}
		o[i].val = v
		return o.marshal()
	}
	return doc, nil
}

func configOverview(cfg *config.Config, cfgPath string) {
	r := cfg.Raw
	on := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}
	row := func(key, val, hint string) {
		if hint != "" {
			hint = "→ " + hint
		}
		fmt.Println(strings.TrimRight(fmt.Sprintf("  %-27s %-44s %s", key, val, hint), " "))
	}
	fmt.Printf("%s\n\n", cfgPath)
	if r.Repo.IsLocal() {
		row("repo", "local mode, bound to "+r.Repo.Root, "")
	} else {
		row("repo", r.Repo.Owner+"/"+r.Repo.Name+" (= origin remote)", "")
		row("git.commit", on(r.Git.CommitEnabled()), "")
		row("git.push", on(r.PushEnabled()), "")
	}
	row("identity", r.Identity.SessionID, "cockpit keygen")
	tiers := make([]string, 0, len(r.Trust.Tiers))
	for t, ks := range r.Trust.Tiers {
		tiers = append(tiers, fmt.Sprintf("%s (%d)", t, len(ks)))
	}
	sort.Strings(tiers)
	row("trust.tiers", strings.Join(tiers, ", "), "cockpit trust")
	if r.Execution.Enabled() {
		img := r.Execution.Image
		if len(img) > 44 {
			img = img[:43] + "…"
		}
		row("execution.image", img, "cockpit pin")
		row("execution.network", on(r.Execution.Network), "")
	} else {
		row("execution.image", "none — commands are recorded, not run", "cockpit pin")
	}
	var srcs []string
	for _, s := range r.Federation.Sources {
		srcs = append(srcs, s.Name+" ("+s.Kind+")")
	}
	row("federation.sources", orDefault(strings.Join(srcs, ", "), "none"), "cockpit federation")
	row("claims.allowedTemplates", strings.Join(r.Claims.AllowedTemplates, ", "), "")
	row("reproduction.requiredLevel", orDefault(r.Reproduction.RequiredLevel, "L0"), "")
	row("union.publish", on(r.Union.Publish), "")
	row("anchor.enabled", on(r.Anchor.Enabled), "")
	fmt.Println("\nchange one:  cockpit config <key> <value>     e.g. cockpit config git.push off")
}
