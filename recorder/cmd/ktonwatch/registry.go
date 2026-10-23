package main

import (
	"net/http"
	"strings"

	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/show"
	"github.com/kton-protocol/kton-cockpit/recorder/watch"
)

// registries serves the signed records of the recorded projects' cockpit repositories next to
// the observations, from the same origin as kton-web (/web/) and the recorder's graph
// (/kton/graph.json), so the viewer can draw both in one graph (?union=…&graph=…):
//
//	/kton/reg/<id>/data/union.json, keys.json, names.json, attested.json, select
//
// It is cockpit show's data handler (internal/show), mounted, not copied: the cockpit reads the
// records through its index over the repository and its federation sources, and hands out the
// repository's trust tiers. Trust stays with the viewer, which verifies every record against them.
func registries(sv *watch.Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/kton/reg/")
		id, _, _ := strings.Cut(rest, "/")
		repo := sv.RegistryRepo(id)
		if id == "" || repo == "" {
			http.Error(w, "no recorded project in a cockpit repository with this id", http.StatusNotFound)
			return
		}
		cfg, err := config.Load(r.Context(), repo)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		h, err := show.New(cfg).Handler("")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.StripPrefix("/kton/reg/"+id, h).ServeHTTP(w, r)
	})
}
