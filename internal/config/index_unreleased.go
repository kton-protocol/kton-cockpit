//go:build unreleased

package config

// The oxigraph index projects records with the kernel's RDF packages, which only this build links.
func init() { IndexKinds = append(IndexKinds, "oxigraph") }
