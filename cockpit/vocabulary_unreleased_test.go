//go:build unreleased

package cockpit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitmick/ktonpkg/scope"
)

// The cockpit's templates define describes and rationale; ktonpkg reads packages by the same IRIs
// and the same "value" field, held as constants because it sits below the cockpit and cannot
// import it. This keeps the two from drifting apart.
func TestTemplates_KtonpkgReadsWhatDescribesAndRationaleWrite(t *testing.T) {
	for name, pred := range map[string]string{"describes": scope.Description, "rationale": scope.Rationale} {
		b, err := os.ReadFile(filepath.Join("..", "uat", "participant-skeleton", "templates", name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var tmpl struct {
			Predicate string                     `json:"predicate"`
			Fields    map[string]json.RawMessage `json:"fields"`
		}
		if err := json.Unmarshal(b, &tmpl); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if tmpl.Predicate != pred {
			t.Errorf("%s: the template says %s, ktonpkg reads %s", name, tmpl.Predicate, pred)
		}
		if _, ok := tmpl.Fields["value"]; !ok || len(tmpl.Fields) != 1 {
			t.Errorf("%s: ktonpkg reads one field, value; the template has %v", name, tmpl.Fields)
		}
	}
}
