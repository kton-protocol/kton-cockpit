package config

import "testing"

// The SPARQL image is pinned by digest, like the image a run executes in: the index daemon started
// from it is told from another by that digest alone.
func TestConfig_QueryImageIsPinnedByDigest(t *testing.T) {
	for _, img := range []string{"ghcr.io/oxigraph/oxigraph:0.5.11", "oci://ghcr.io/oxigraph/oxigraph:latest", "oci://ghcr.io/oxigraph/oxigraph"} {
		if validateQueryImage(img) == nil {
			t.Errorf("accepted %q", img)
		}
	}
	for _, img := range []string{"", "oci://ghcr.io/oxigraph/oxigraph@sha256:7532c1f9aa5f28c0dc6a4243198f7f6144017fab7429bf6c164a89fec9411767"} {
		if err := validateQueryImage(img); err != nil {
			t.Errorf("refused %q: %v", img, err)
		}
	}
}
