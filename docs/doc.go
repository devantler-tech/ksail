// Package docs provides go:generate directives for generating reference
// documentation (CLI flags and configuration reference) from Go source code.
//
// Run: go generate ./docs/...
//
// NOTE: The generated CLI flags docs are consumed by pkg/svc/chat/gen_docs.go,
// so run this generator BEFORE go generate ./pkg/svc/chat/... when regenerating
// all documentation.
package docs

//go:generate go run gen_docs.go gen_docs_prose.go
