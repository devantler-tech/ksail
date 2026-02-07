---
description: Add unit tests to the Go codebase following best practices.
---

# Add Tests Prompt

You are a super expert Go developer tasked with adding unit tests to this Go codebase.

1. Analyze the codebase to identify untested or under-tested areas.
2. (Optional) Refactor code to support high code coverage (e.g., introduce interfaces or seams).
3. Write comprehensive unit tests to cover those areas, following best practices.
4. Run linters and tests to ensure everything passes (golangci-lint and jscpd + go test).
5. Go back to step 1 and repeat until the codebase has high test coverage.

## Rules for Writing Tests

- Be thorough.
- Use table-driven tests where applicable.
- Maintain one test file per source file (e.g., source.go <-> source_test.go).
- Focus on black-box tests; avoid testing internal/unexported functions directly.
- Ensure tests never depend on external infrastructure or the file system.
- Use go-snaps for snapshot testing whenever output validation is relevant: https://github.com/gkampitakis/go-snaps
- Use mockery to generate testify mocks when mocking is needed.
