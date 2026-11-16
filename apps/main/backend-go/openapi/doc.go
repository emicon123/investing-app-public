// Package openapi holds the oapi-codegen-generated ServerInterface + types for investing-app,
// generated via `go generate ./...` from the real ../../../docs/openapi.yaml bundle. Regenerate,
// don't hand-edit, after any spec change (an Architect task, not a Backend-Go one — see
// AGENTS.md's sequencing gate).
//
// The generated code covers the FULL spec (every domain's endpoints); cmd/server/main.go's
// apiServer implements ServerInterface by embedding the domain handlers, whose methods are named
// after the operationIds.
package openapi

//go:generate go tool oapi-codegen -config oapi-codegen-config.yaml ../../../../docs/openapi.yaml
