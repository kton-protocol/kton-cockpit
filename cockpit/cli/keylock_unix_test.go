//go:build !windows

package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"kton.dev/plankton/core"
	"kton.dev/plankton/keys"
)

// ADR-021 at the command line: the built binary, run with no controlling terminal (a new session, so
// /dev/tty is not there to be asked even when the tests are run from one).

const cliPass = "correct horse battery staple 8841"

// noTerminal detaches cmd from any controlling terminal and gives it the environment env adds to the
// test's own, without KTON_KEY_PASSPHRASE unless env sets it.
func noTerminal(cmd *exec.Cmd, env ...string) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	var kept []string
	for _, e := range cmd.Env {
		if !strings.HasPrefix(e, keys.EnvPassphrase+"=") {
			kept = append(kept, e)
		}
	}
	cmd.Env = append(kept, env...)
}

func runNoTerminal(t *testing.T, r *testrepo.Repo, env []string, args ...string) (string, int) {
	t.Helper()
	cmd := r.CockpitCommand(t, args...)
	noTerminal(cmd, env...)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("cockpit %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out), code
}

func encryptKeyFile(t *testing.T, path, pass string) {
	t.Helper()
	priv, err := keys.Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := keys.MarshalEncrypted(priv, []byte(pass))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// envDumpingGit puts a git ahead of the real one on PATH that appends its environment to a file and
// runs the real git: every git the cockpit starts, the first one (reading the repository's root, before
// anything is signed) included, shows what a child of the cockpit inherits. It returns the file and
// the PATH to run the cockpit with.
func envDumpingGit(t *testing.T) (dump, path string) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	dump = filepath.Join(t.TempDir(), "child-env.txt")
	script := "#!/bin/sh\nenv >> '" + dump + "'\nexec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dump, dir + string(os.PathListSeparator) + os.Getenv("PATH")
}

func TestKeys_ChildrenNeverSeeThePassphraseAndItIsLoggedNowhere(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	encryptKeyFile(t, r.Key(testrepo.SessionID+"-claims"), cliPass)
	dump, path := envDumpingGit(t)

	out, code := runNoTerminal(t, r, []string{keys.EnvPassphrase + "=" + cliPass, "COCKPIT_TRACE=1", "PATH=" + path},
		"say", "working-on", "urn:example:one", "step=analysis")
	if code != 0 {
		t.Fatalf("say with the passphrase in the environment (exit %d):\n%s", code, out)
	}
	child, err := os.ReadFile(dump)
	if err != nil {
		t.Fatalf("no git ran through the shim, so this test proves nothing: %v", err)
	}
	if strings.Contains(string(child), keys.EnvPassphrase) || strings.Contains(string(child), cliPass) {
		t.Errorf("a child the cockpit started saw the passphrase:\n%s", child)
	}
	if !strings.Contains(string(child), "COCKPIT_REPO_DIR") {
		t.Error("the child's environment is not the cockpit's: the dump proves nothing")
	}
	if strings.Contains(out, cliPass) {
		t.Errorf("the passphrase is in the command's output or trace:\n%s", out)
	}
	if !strings.Contains(out, "trace say") {
		t.Errorf("the trace was not on, so it was not checked:\n%s", out)
	}
	filepath.WalkDir(r.Root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if b, _ := os.ReadFile(p); strings.Contains(string(b), cliPass) {
				t.Errorf("the passphrase is in %s", p)
			}
		}
		return nil
	})
}

func TestKeys_WithoutATerminalOrVariableAnEncryptedKeyIsLockedAndAWrongOneRefused(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	key := r.Key(testrepo.SessionID + "-claims")
	encryptKeyFile(t, key, cliPass)
	before := r.HeadSHA(t)

	out, code := runNoTerminal(t, r, nil, "say", "working-on", "urn:example:one", "step=analysis", "--json")
	if code != 2 || !strings.Contains(out, `"code":"identity.locked"`) || !strings.Contains(out, key) || !strings.Contains(out, keys.EnvPassphrase) {
		t.Errorf("no source: want exit 2 identity.locked naming the key and KTON_KEY_PASSPHRASE (exit %d):\n%s", code, out)
	}
	out, code = runNoTerminal(t, r, []string{keys.EnvPassphrase + "=wrong-one"}, "say", "working-on", "urn:example:one", "step=analysis", "--json")
	if code != 2 || !strings.Contains(out, `"code":"identity.locked"`) || !strings.Contains(out, "wrong passphrase") || strings.Contains(out, "wrong-one") {
		t.Errorf("a wrong passphrase (exit %d):\n%s", code, out)
	}
	if r.HeadSHA(t) != before {
		t.Error("a refused say committed")
	}
}

func TestKeys_DoctorSaysPlainOrEncryptedWithoutAskingForAPassphrase(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	encryptKeyFile(t, r.Key(testrepo.SessionID+"-claims"), cliPass)

	stdout, code := runNoTerminal(t, r, nil, "doctor", "--json")
	if code != 0 {
		t.Fatalf("doctor on an encrypted key (exit %d):\n%s", code, stdout)
	}
	var rep struct {
		Fields struct {
			PlanktonKey, NektonKey struct{ Form string }
			Identity               struct {
				NektonKeyID string
				Keys        []struct{ Name, KeyID, Form string }
			}
		}
	}
	// The report's own layout: find the fields wherever doctor nests them.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		t.Fatalf("doctor --json: %v\n%s", err, stdout)
	}
	if err := json.Unmarshal([]byte(stdout), &rep.Fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["fields"]; ok {
		json.Unmarshal(raw["fields"], &rep.Fields)
	}
	f := rep.Fields
	if f.PlanktonKey.Form != "plain" || f.NektonKey.Form != "encrypted" {
		t.Errorf("want plankton plain and nekton encrypted, got %q and %q:\n%s", f.PlanktonKey.Form, f.NektonKey.Form, stdout)
	}
	if len(f.Identity.Keys) == 0 || f.Identity.Keys[0].Form != "encrypted" || f.Identity.Keys[0].KeyID == "" {
		t.Errorf("the identity's claim key should be encrypted with a keyid read from its public half: %+v", f.Identity.Keys)
	}
	text, _ := runNoTerminal(t, r, nil, "doctor")
	if !strings.Contains(text, "(encrypted)") || !strings.Contains(text, "(plain)") {
		t.Errorf("doctor's text should say each key's form:\n%s", text)
	}
}

func TestKeys_KeygenEncryptWritesAKeySSHKeygenReads(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen is not installed")
	}
	r := testrepo.New(t)
	r.Use(t)
	ring := t.TempDir()

	// No terminal and no variable: refused, and nothing is made.
	out, code := runNoTerminal(t, r, []string{"KTON_KEYS=" + ring}, "keygen", "--encrypt", "alice")
	if code == 0 || !strings.Contains(out, keys.EnvPassphrase) {
		t.Errorf("keygen --encrypt without a terminal or %s should refuse (exit %d):\n%s", keys.EnvPassphrase, code, out)
	}
	if _, err := os.Stat(filepath.Join(ring, "alice.key")); err == nil {
		t.Error("a refused keygen wrote a key")
	}

	out, code = runNoTerminal(t, r, []string{"KTON_KEYS=" + ring, keys.EnvPassphrase + "=" + cliPass}, "keygen", "--encrypt", "alice")
	if code != 0 {
		t.Fatalf("keygen --encrypt (exit %d):\n%s", code, out)
	}
	if strings.Contains(out, cliPass) {
		t.Errorf("keygen printed the passphrase:\n%s", out)
	}
	for _, name := range []string{"alice", "alice-claims"} {
		key := filepath.Join(ring, name+".key")
		b, err := os.ReadFile(key)
		if err != nil || !keys.IsEncrypted(b) {
			t.Fatalf("%s should be an encrypted key (%v)", key, err)
		}
		sshOut, err := exec.Command("ssh-keygen", "-y", "-P", cliPass, "-f", key).CombinedOutput()
		if err != nil || !strings.HasPrefix(string(sshOut), "ssh-ed25519 ") {
			t.Errorf("ssh-keygen -y could not read %s: %v\n%s", key, err, sshOut)
		}
		if _, err := exec.Command("ssh-keygen", "-y", "-P", "nope", "-f", key).CombinedOutput(); err == nil {
			t.Errorf("ssh-keygen read %s with a wrong passphrase", key)
		}
		// The .pub beside it is the hex public half of that very key.
		pub, perr := keys.PublicKey(b)
		pubFile, ferr := os.ReadFile(filepath.Join(ring, name+".pub"))
		if perr != nil || ferr != nil || strings.TrimSpace(string(pubFile)) == "" {
			t.Fatalf("the public half of %s: %v %v", name, perr, ferr)
		}
		if _, err := core.ParsePublicKeyHex(strings.TrimSpace(string(pubFile))); err != nil || core.KeyIDHex(pub) == "" {
			t.Errorf("%s.pub is not the key's public half: %v", name, err)
		}
	}
}

// The servers never read the passphrase, even the right one in their environment (ADR-021 section 4):
// `cockpit mcp` and `cockpit serve --stdio` refuse an encrypted key as identity.locked and sign nothing.
func TestKeys_McpAndServeRefuseAnEncryptedKeyWithTheRightPassphraseInTheEnvironment(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	encryptKeyFile(t, r.Key(testrepo.SessionID+"-claims"), cliPass)
	head := r.HeadSHA(t)
	env := []string{keys.EnvPassphrase + "=" + cliPass}
	fields := map[string]string{"step": "analysis", "by-session": "s"}

	t.Run("mcp", func(t *testing.T) {
		cmd := r.CockpitCommand(t, "mcp")
		noTerminal(cmd, env...)
		ctx := context.Background()
		cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer cs.Close()
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "cockpit_say",
			Arguments: map[string]any{"template": "working-on", "subject": "urn:example:x", "fields": fields}})
		if err != nil {
			t.Fatal(err)
		}
		var text strings.Builder
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				text.WriteString(tc.Text)
			}
		}
		if !res.IsError || !strings.Contains(text.String(), "encrypted") || strings.Contains(text.String(), cliPass) {
			t.Errorf("want a tool error for the encrypted key, got %v %q", res.IsError, text.String())
		}
	})

	t.Run("serve", func(t *testing.T) {
		cmd := r.CockpitCommand(t, "serve", "--stdio")
		noTerminal(cmd, env...)
		in, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		out, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { in.Close(); _ = cmd.Wait() })
		c := &serveClient{t: t, w: in, r: bufio.NewReader(out)}
		c.ok("initialize", map[string]any{"root": r.Root})
		rep := c.call("say", map[string]any{"template": "working-on", "subject": "urn:example:x", "fields": fields})
		if rep.refusalCode() != "identity.locked" {
			t.Errorf("say over serve: want identity.locked, got %+v", rep)
		}
	})
	if r.HeadSHA(t) != head {
		t.Error("a server committed with an encrypted key")
	}
}
