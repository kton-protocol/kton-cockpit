// kton-repro makes one recorded file version again, in containers, and says whether the bytes
// are the same.
//
//	kton-repro -project P -path F [-sha H]
//
// ktonwatch plans it (/api/reproplan): the call that made the file runs again; its inputs come
// from the CAS, or are made again by the calls that made them. An empty working directory gets
// only the files from the CAS. Every planned call runs in a container of its own, chosen by the
// tool: R and Quarto in the R image, NONMEM in the NONMEM image. No network, the invoking user,
// no capabilities. The stand-ins (kton-call) are in every container, ahead on PATH: a call
// whose key is known and whose outputs are kept is put in place instead of computed, except the
// call the reproduction is about. The result goes to <out>/<stamp>-<name>/kton-repro.json.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/kton-protocol/kton-cockpit/recorder/watch"
)

type runResult struct {
	Tool    string  `json:"tool"`
	Cwd     string  `json:"cwd"`
	Image   string  `json:"image"`
	ImageID string  `json:"image_id"`
	Seconds float64 `json:"seconds"`
	Exit    int     `json:"exit"`
	Reused  bool    `json:"reused,omitempty"` // put in place from the CAS by the stand-in
	Target  bool    `json:"target,omitempty"`
}

type report struct {
	Target     string            `json:"target"`
	Recorded   string            `json:"recorded"`
	Reproduced string            `json:"reproduced"`
	Same       bool              `json:"same"`
	Compare    *watch.Comparison `json:"compare,omitempty"` // when the bytes differ: provisional, numbers within a tolerance
	Dir        string            `json:"dir"`
	Runs       []runResult       `json:"runs"`
	Plan       watch.ReproPlan   `json:"plan"`
}

func main() {
	home, _ := os.UserHomeDir()
	cache := filepath.Join(home, ".cache", "kton-cockpit")
	project := flag.String("project", "", "the project directory")
	path := flag.String("path", "", "the file to make again (absolute, or relative to the project)")
	sha := flag.String("sha", "", "the version (default: the latest one a call wrote)")
	watchURL := flag.String("watch", "http://127.0.0.1:8378", "ktonwatch")
	casDir := flag.String("cas", filepath.Join(cache, "cas"), "the CAS")
	out := flag.String("out", filepath.Join(cache, "repro"), "where reproductions go")
	imgR := flag.String("image-r", "sha256:dee2a8cfb95adf7eb63213da617a1ac538e0e328477ece0f07920bd9e513294a", "image for Rscript, R, quarto (scinteco/jam-r)")
	imgNM := flag.String("image-nonmem", "scinteco/improve-docker-tool:nonmem751", "image for nmfe")
	rtol := flag.Float64("rtol", 1e-3, "relative tolerance for numbers when the bytes differ (provisional, until a spectrum brings its own)")
	lic := flag.String("nonmem-license", filepath.Join(home, ".local", "opt", "nm751", "license", "nonmem.lic"), "NONMEM licence, mounted read-only")
	flag.Parse()
	if *project == "" || *path == "" {
		flag.Usage()
		os.Exit(2)
	}
	if !filepath.IsAbs(*path) {
		*path = filepath.Join(*project, *path)
	}

	var plan watch.ReproPlan
	q := url.Values{"project": {*project}, "path": {*path}, "sha": {*sha}}
	if err := getJSON(*watchURL+"/api/reproplan?"+q.Encode(), &plan); err != nil {
		fail("no plan: %v", err)
	}
	if len(plan.Lost) > 0 {
		fmt.Fprintf(os.Stderr, "kton-repro: %s cannot be made, missing: %s\n", plan.Target.Rel, strings.Join(plan.Lost, ", "))
		os.Exit(3)
	}
	var calls []watch.Call
	if err := getJSON(*watchURL+"/api/calls?"+url.Values{"project": {*project}}.Encode(), &calls); err != nil {
		fail("no calls: %v", err)
	}
	stamp := time.Now().Format("20060102-150405")
	dir := filepath.Join(*out, stamp+"-"+filepath.Base(*path))
	work, kton := filepath.Join(dir, "work"), filepath.Join(dir, "kton")
	must(os.MkdirAll(work, 0o755))
	must(os.MkdirAll(filepath.Join(kton, "stubs"), 0o755))

	// the stand-in and its names, as the containers see them
	callBin, err := exec.LookPath("kton-call")
	if err != nil {
		callBin = filepath.Join(home, ".local", "bin", "kton-call")
	}
	for _, t := range []string{"nmfe74", "nmfe75", "nmfe76", "execute", "vpc", "bootstrap", "scm", "sse", "bbi"} {
		must(os.Symlink("/kton/bin/kton-call", filepath.Join(kton, "stubs", t)))
	}
	b, _ := json.Marshal(calls) // the stand-in uses Argv, Reads[].Rel and the hashes
	must(os.WriteFile(filepath.Join(kton, "calls.json"), b, 0o644))

	cas := &watch.CAS{Dir: *casDir}
	for _, f := range plan.Put {
		dst := filepath.Join(work, f.Rel)
		must(os.MkdirAll(filepath.Dir(dst), 0o755))
		must(copyFile(cas.Path(f.SHA256), dst))
	}
	fmt.Printf("kton-repro: %s (sha256 %s) in %s\n  %d files from the store, %d calls\n", plan.Target.Rel, plan.Target.SHA256[:12], dir, len(plan.Put), len(plan.Run))

	rep := report{Target: plan.Target.Rel, Recorded: plan.Target.SHA256, Dir: dir, Plan: plan}
	targetKey := ""
	for _, r := range plan.Run {
		if r.Target {
			targetKey = r.Key
		}
	}
	uid := fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
	for _, r := range plan.Run {
		for _, d := range r.Dirs {
			must(os.MkdirAll(filepath.Join(work, d), 0o755))
		}
		img := *imgR
		args := []string{"run", "--rm", "--network", "none", "--user", uid, "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
			"-e", "HOME=/tmp", "-e", "KTON_CAS=/kton/cas", "-e", "KTON_CALLS=/kton/calls.json", "-e", "KTON_NO_REUSE=" + targetKey,
			"-e", "KTON_WATCH=http://unreachable.invalid",
			"-v", work + ":/work", "-w", filepath.Join("/work", r.Cwd),
			"-v", *casDir + ":/kton/cas:ro", "-v", callBin + ":/kton/bin/kton-call:ro",
			"-v", filepath.Join(kton, "stubs") + ":/kton/stubs:ro", "-v", filepath.Join(kton, "calls.json") + ":/kton/calls.json:ro"}
		switch {
		case strings.HasPrefix(r.Argv[0], "nmfe"):
			img = *imgNM
			if _, err := os.Stat(*lic); err == nil {
				args = append(args, "-v", *lic+":/opt/nm/license/nonmem.lic:ro")
			}
		case r.Argv[0] == "Rscript" || r.Argv[0] == "R" || r.Argv[0] == "quarto":
		default:
			// no image for this tool here: the stand-in may still put it in place from the CAS
		}
		args = append(args, "--entrypoint", "sh", img, "-c", `PATH=/kton/stubs:$PATH; export PATH; exec "$@"`, "sh")
		args = append(args, r.Argv...)
		fmt.Printf("  → %-40s in %-8s (%s)\n", r.Tool, "/"+r.Cwd, shortImg(imageName(img)))
		t0 := time.Now()
		cmd := exec.Command("docker", args...)
		var stderr bytes.Buffer
		cmd.Stdout = io.Discard
		cmd.Stderr = io.MultiWriter(&stderr, prefixWriter{"      "})
		err := cmd.Run()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			code = -1
		}
		res := runResult{Tool: r.Tool, Cwd: r.Cwd, Image: img, ImageID: imageID(img), Seconds: time.Since(t0).Seconds(), Exit: code,
			Reused: strings.Contains(stderr.String(), "from the store"), Target: r.Target}
		rep.Runs = append(rep.Runs, res)
		if code != 0 {
			fmt.Printf("    exited with %d\n", code)
			break
		}
	}
	rep.Reproduced = sha256File(filepath.Join(work, plan.Target.Rel))
	rep.Same = rep.Reproduced == strings.TrimPrefix(plan.Target.SHA256, "sha256:")
	if !rep.Same && rep.Reproduced != "" {
		a, errA := os.ReadFile(cas.Path(plan.Target.SHA256))
		b, errB := os.ReadFile(filepath.Join(work, plan.Target.Rel))
		if errA == nil && errB == nil {
			c := watch.CompareFiles(a, b, *rtol)
			rep.Compare = &c
		}
	}
	jb, _ := json.MarshalIndent(rep, "", " ")
	must(os.WriteFile(filepath.Join(dir, "kton-repro.json"), jb, 0o644))
	switch {
	case rep.Reproduced == "":
		fmt.Printf("= %s: not produced\n", plan.Target.Rel)
		os.Exit(1)
	case rep.Same:
		fmt.Printf("= %s: reproduziert, dieselben Bytes ✓\n", plan.Target.Rel)
	case rep.Compare != nil && rep.Compare.Verdict == "within":
		fmt.Printf("= %s: other bytes, but within the tolerance %g (provisional comparison): largest difference %.2g, %s\n",
			plan.Target.Rel, *rtol, rep.Compare.MaxRel, rep.Compare.Where)
	default:
		fmt.Printf("= %s: different, %s instead of %s\n", plan.Target.Rel, rep.Reproduced[:12], plan.Target.SHA256[:12])
		if c := rep.Compare; c != nil {
			if c.Where != "" {
				fmt.Printf("  %s; largest difference %.2g, %s\n", c.Note, c.MaxRel, c.Where)
			} else {
				fmt.Printf("  %s\n", c.Note)
			}
		}
		os.Exit(1)
	}
}

type prefixWriter struct{ p string }

func (w prefixWriter) Write(b []byte) (int, error) {
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if strings.TrimSpace(l) != "" {
			fmt.Println(w.p + l)
		}
	}
	return len(b), nil
}

func shortImg(i string) string {
	if k := strings.Index(i, "@sha256:"); k > 0 {
		return i[:k] + "@" + i[k+8:k+20]
	}
	return i
}

func imageID(img string) string {
	out, err := exec.Command("docker", "image", "inspect", "--format", "{{.Id}}", img).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func getJSON(u string, v any) error {
	c := http.Client{Timeout: 120 * time.Second}
	resp, err := c.Get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func sha256File(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil))
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func must(err error) {
	if err != nil {
		fail("%v", err)
	}
}

func fail(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "kton-repro: "+f+"\n", a...)
	os.Exit(1)
}

// imageName: a readable name for an image given by id
func imageName(img string) string {
	if !strings.HasPrefix(img, "sha256:") {
		return img
	}
	out, err := exec.Command("docker", "image", "inspect", "--format", "{{range .RepoTags}}{{.}} {{end}}{{range .RepoDigests}}{{.}} {{end}}", img).Output()
	if f := strings.Fields(string(out)); err == nil && len(f) > 0 {
		return strings.SplitN(f[0], "@", 2)[0] + "@" + img[7:19]
	}
	return img[:19]
}
