package chat

// gen_docs.go reads the source docs from docs/src/content/docs/, processes
// them (strips frontmatter, imports, JSX), and writes docs_generated.go
// containing the result as a Go string constant. This avoids duplicating
// the entire docs directory inside this package.
//go:generate go run gen_docs.go
