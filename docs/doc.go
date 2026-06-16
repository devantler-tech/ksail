// Package docs provides go:generate directives for generating reference
// documentation (CLI flags pages and index, configuration reference, and the
// MCP tool catalog partial) from Go source code, plus the unit-testable
// rendering helpers (render.go, fielddocs.go) used by the gen_docs.go
// generator.
//
// Run: go generate ./docs/...
//
// NOTE: The generated CLI flags docs are consumed by pkg/svc/chat/gen_docs.go,
// so run this generator BEFORE go generate ./pkg/svc/chat/... when regenerating
// all documentation.
package docs

//go:generate go run gen_docs.go gen_docs_prose.go
