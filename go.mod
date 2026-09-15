module github.com/deathbychoco/claude-science-cockpit

go 1.25.0

require (
	github.com/google/jsonschema-go v0.4.3
	github.com/modelcontextprotocol/go-sdk v1.6.1
	kton.dev/plankton v0.0.0-00010101000000-000000000000
)

require (
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
)

replace kton.dev/plankton => ../kton/reference

replace kton.dev/nekton => ../kton/nekton/reference

replace kton.dev/kton => ../kton/kton/reference
