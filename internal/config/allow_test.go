package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// install --allow writes one fact into the operator's file and leaves the rest as it was: key
// order, indentation, values. A second allow of the same id changes nothing.
func TestAllowPackage_AddsTheIdAndKeepsTheFile(t *testing.T) {
	dir := t.TempDir()
	orig := "{\n  \"repo\": {\n    \"owner\": \"o\",\n    \"name\": \"n\"\n  },\n  \"claims\": {\n    \"allowedTemplates\": []\n  },\n  \"verbs\": {\n    \"say\": true\n  }\n}\n"
	os.WriteFile(filepath.Join(dir, "cockpit.config.json"), []byte(orig), 0o644)
	id := "sha256:" + strings.Repeat("a", 64)
	changed, err := AllowPackage(dir, id)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "cockpit.config.json"))
	want := strings.Replace(orig, "\"allowedTemplates\": []\n", "\"allowedTemplates\": [],\n    \"allowedPackages\": [\n      \""+id+"\"\n    ]\n", 1)
	if string(got) != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if changed, _ := AllowPackage(dir, id); changed {
		t.Error("allowing an allowed package again changed the file")
	}
}
