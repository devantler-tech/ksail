# Go Refactoring Guide: Package Structure and Dependencies

This guide covers improving package organization, reducing coupling, and managing dependencies effectively.

## Package Organization Principles

### 1. High Cohesion

**Principle:** Related functionality should be grouped together.

**Detection:**
- Packages with many unrelated functions
- Functions that don't share data or behavior
- Files in a package that don't import each other

**Strategy:**
1. Group by feature/domain, not by layer (avoid generic names like `utils`, `helpers`, `common`)
2. Keep related types and functions together
3. Split large packages into sub-packages when they grow beyond ~10-15 files

**Example:**

Before:
```
pkg/
  utils/
    string.go      # String utilities
    time.go        # Time utilities
    validation.go  # Validation helpers
    http.go        # HTTP helpers
```

After:
```
pkg/
  stringutil/
    format.go
    parse.go
  timeutil/
    format.go
    duration.go
  validation/
    email.go
    url.go
  httputil/
    request.go
    response.go
```

### 2. Low Coupling

**Principle:** Packages should depend on as few other packages as possible.

**Detection:**
```bash
# Analyze package dependencies
go mod graph | grep "github.com/devantler-tech/ksail"

# Find circular dependencies
go list -f '{{.ImportPath}} {{.Imports}}' ./... | grep -E "pkg/a.*pkg/b|pkg/b.*pkg/a"
```

**Strategy:**
1. Define interfaces in the consuming package, not the implementing package
2. Use dependency injection
3. Break circular dependencies by extracting shared types to a new package
4. Prefer smaller, focused packages

**Example:**

Before (circular dependency):
```go
// pkg/user/user.go
package user

import "github.com/devantler-tech/ksail/pkg/email"

type User struct {
    Email email.Address
}

// pkg/email/email.go
package email

import "github.com/devantler-tech/ksail/pkg/user"

func SendWelcome(u user.User) error {
    // ...
}
```

After (circular dependency broken):
```go
// pkg/types/email.go
package types

type EmailAddress string

// pkg/user/user.go
package user

import "github.com/devantler-tech/ksail/pkg/types"

type User struct {
    Email types.EmailAddress
}

// pkg/email/email.go
package email

import "github.com/devantler-tech/ksail/pkg/types"

func SendWelcome(email types.EmailAddress, name string) error {
    // ...
}
```

### 3. Interface Segregation

**Principle:** Interfaces should be small and focused. Define interfaces where they're used, not where they're implemented.

**Strategy:**
1. Keep interfaces to 1-3 methods
2. Define interfaces in the consuming package
3. Let implementing packages satisfy the interface implicitly
4. Avoid large "god" interfaces

**Example:**

Before:
```go
// pkg/storage/storage.go
package storage

type Storage interface {
    Get(key string) ([]byte, error)
    Set(key string, value []byte) error
    Delete(key string) error
    List() ([]string, error)
    Backup() error
    Restore(path string) error
}

// pkg/cache/cache.go
package cache

import "github.com/devantler-tech/ksail/pkg/storage"

type Cache struct {
    storage storage.Storage  // Needs all methods but only uses Get/Set
}
```

After:
```go
// pkg/cache/cache.go
package cache

// Define only what we need in the consuming package
type valueStore interface {
    Get(key string) ([]byte, error)
    Set(key string, value []byte) error
}

type Cache struct {
    storage valueStore  // Only depends on Get/Set
}

// pkg/storage/storage.go
package storage

// Implements Get/Set automatically satisfying cache.valueStore
type Storage struct {}

func (s *Storage) Get(key string) ([]byte, error) { /* ... */ }
func (s *Storage) Set(key string, value []byte) error { /* ... */ }
func (s *Storage) Delete(key string) error { /* ... */ }
// ... other methods
```

## Common Package Smells

### 1. God Packages

**Smell:** A package with too many files or responsibilities.

**Detection:**
```bash
# Count files per package
find . -name "*.go" -not -path "*/vendor/*" | sed 's|/[^/]*$||' | sort | uniq -c | sort -rn | head -20
```

**Refactoring:**
1. Identify logical groupings within the package
2. Create sub-packages for each group
3. Move related files to sub-packages
4. Update imports

### 2. Util/Helper Packages

**Smell:** Packages named `utils`, `helpers`, `common`.

**Refactoring:**
1. Rename based on what the package provides (e.g., `stringutil`, `validation`)
2. Split by domain if the package has multiple concerns
3. Move functions closer to where they're used if they're only used in one place

### 3. Internal Types Leaking

**Smell:** Implementation details exposed in public API.

**Refactoring:**
1. Use unexported types for internal implementation
2. Define public interfaces that hide implementation
3. Return interfaces, accept concrete types (when appropriate)

**Example:**

Before:
```go
// pkg/database/database.go
package database

// DB is exported but exposes internal details
type DB struct {
    conn      *sql.DB  // Should be hidden
    queryLog  []string // Should be hidden
}

func NewDB(connStr string) (*DB, error) {
    // Returns concrete type exposing internals
}
```

After:
```go
// pkg/database/database.go
package database

// Database is the public interface
type Database interface {
    Query(sql string) ([]Row, error)
    Close() error
}

// db is the private implementation
type db struct {
    conn      *sql.DB
    queryLog  []string
}

func NewDatabase(connStr string) (Database, error) {
    // Returns interface hiding implementation
}

func (d *db) Query(sql string) ([]Row, error) { /* ... */ }
func (d *db) Close() error { /* ... */ }
```

## Dependency Management

### Analyzing Dependencies

```bash
# List direct dependencies
go list -m all

# Find unused dependencies
go mod tidy

# Visualize dependency graph (requires graphviz)
go mod graph | dot -T svg -o deps.svg
```

### Reducing Dependencies

1. **Evaluate each dependency:**
   - Is it actively maintained?
   - Does it pull in many transitive dependencies?
   - Could we implement it ourselves simply?

2. **Prefer standard library:**
   - Use `net/http` instead of frameworks when possible
   - Use `encoding/json` for JSON
   - Use `testing` package for tests

3. **Interface segregation:**
   - Depend on interfaces, not concrete implementations
   - Define minimal interfaces for external dependencies

## Validation Steps

After package refactoring:

1. **Build:** Ensure code compiles
   ```bash
   go build ./...
   ```

2. **Test:** Verify existing tests pass
   ```bash
   go test ./...
   ```

3. **Check imports:** Ensure no circular dependencies
   ```bash
   go list -f '{{.ImportPath}} {{.Imports}}' ./... | grep circular || echo "No circular deps"
   ```

4. **Lint:** Check for new issues
   ```bash
   golangci-lint run --timeout 5m
   ```

5. **Verify usage:** Ensure public API hasn't changed unexpectedly
   ```bash
   # Check for breaking changes in go.mod
   go list -m -versions all
   ```

## Best Practices

1. **Start small:** Refactor one package at a time
2. **Maintain compatibility:** Avoid breaking public APIs unless necessary
3. **Use internal packages:** For code that shouldn't be imported externally
4. **Document package purpose:** Add package comments explaining what the package provides
5. **Keep flat structure:** Avoid deeply nested package hierarchies
6. **Group by feature:** Organize by domain/feature, not technical layer

## Common Mistakes

1. **Over-organizing:** Creating too many small packages prematurely
2. **Breaking APIs:** Changing public interfaces during refactoring
3. **Creating import cycles:** Not checking for circular dependencies
4. **Moving without understanding:** Refactoring without understanding package relationships
5. **Ignoring tests:** Not updating tests after moving code
