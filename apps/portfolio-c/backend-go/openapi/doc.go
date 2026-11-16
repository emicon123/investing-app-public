// Package openapi holds the oapi-codegen-generated ServerInterface + types for
// portfolio-c, generated via `go generate ./...` from this app's own
// ../../docs/openapi.yaml bundle (originally a snapshot of the Java backend's springdoc
// endpoint — see that file's header for source and date). Regenerate, don't hand-edit,
// after any spec change (an Architect task, not a Backend-Go one — see AGENTS.md's
// sequencing gate).
//
// The generated code covers the FULL spec (every domain's endpoints); cmd/server/main.go's
// apiServer implements ServerInterface by embedding the domain handlers (whose methods are named
// after the operationIds), with openapi.Unimplemented (plain 501) as the fallback for any operation
// no handler serves.
package openapi

//go:generate go tool oapi-codegen -config oapi-codegen-config.yaml ../../docs/openapi.yaml
