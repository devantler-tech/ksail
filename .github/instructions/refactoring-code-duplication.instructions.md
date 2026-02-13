# Go Refactoring Guide: Code Duplication

This guide covers detecting and eliminating code duplication in Go projects.

## Detection

### Using jscpd

Run duplication detection:

```bash
jscpd --config .jscpd.json
```

This will generate:

- Console output with duplication statistics
- HTML report in `report/` directory
- Markdown report for CI/CD

### Manual Detection

Look for:

- Copy-pasted functions with minor variations
- Similar error handling patterns
- Repeated validation logic
- Similar test setup code

## Refactoring Strategies

### 1. Extract Common Function

**Pattern:** Multiple functions with identical or nearly identical code.

**Strategy:**

1. Identify the common logic
2. Extract to a shared function
3. Parameterize the differences
4. Update all call sites

**Example:**

Before:

```go
func ValidateUser(user User) error {
    if user.Name == "" {
        return errors.New("name is required")
    }
    if user.Email == "" {
        return errors.New("email is required")
    }
    return nil
}

func ValidateProduct(product Product) error {
    if product.Name == "" {
        return errors.New("name is required")
    }
    if product.SKU == "" {
        return errors.New("SKU is required")
    }
    return nil
}
```

After:

```go
func ValidateRequired(fieldName, value string) error {
    if value == "" {
        return fmt.Errorf("%s is required", fieldName)
    }
    return nil
}

func ValidateUser(user User) error {
    if err := ValidateRequired("name", user.Name); err != nil {
        return err
    }
    if err := ValidateRequired("email", user.Email); err != nil {
        return err
    }
    return nil
}

func ValidateProduct(product Product) error {
    if err := ValidateRequired("name", product.Name); err != nil {
        return err
    }
    if err := ValidateRequired("SKU", product.SKU); err != nil {
        return err
    }
    return nil
}
```

### 2. Extract to Shared Package

**Pattern:** Similar code across multiple packages.

**Strategy:**

1. Create a shared package with a descriptive name (e.g., `pkg/validation`, `pkg/stringutil`)
2. Move common functions there
3. Export functions with clear names
4. Update all imports

**Example:**

Before (in multiple packages):

```go
// pkg/user/validator.go
func isValidEmail(email string) bool {
    return strings.Contains(email, "@")
}

// pkg/customer/validator.go
func isValidEmail(email string) bool {
    return strings.Contains(email, "@")
}
```

After:

```go
// pkg/validation/email.go
package validation

import "strings"

// IsValidEmail checks if an email address has basic validity
func IsValidEmail(email string) bool {
    return strings.Contains(email, "@")
}

// pkg/user/validator.go
import "github.com/devantler-tech/ksail/v5/pkg/validation"

func ValidateUser(user User) error {
    if !validation.IsValidEmail(user.Email) {
        return errors.New("invalid email")
    }
    return nil
}
```

### 3. Use Table-Driven Patterns

**Pattern:** Repetitive test cases or conditional logic.

**Strategy:**

1. Define a struct with test data
2. Create a slice of test cases
3. Loop through cases
4. Use subtests for clarity

**Example:**

Before:

```go
func TestValidation(t *testing.T) {
    err := Validate("")
    if err == nil {
        t.Error("expected error for empty string")
    }
    
    err = Validate("abc")
    if err != nil {
        t.Error("unexpected error for valid string")
    }
    
    err = Validate("   ")
    if err == nil {
        t.Error("expected error for whitespace string")
    }
}
```

After:

```go
func TestValidation(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        wantErr bool
    }{
        {"empty string", "", true},
        {"valid string", "abc", false},
        {"whitespace only", "   ", true},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := Validate(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

### 4. Use Interfaces for Common Behavior

**Pattern:** Multiple types with similar method implementations.

**Strategy:**

1. Define an interface for common behavior
2. Implement the interface on each type
3. Write shared functions that accept the interface
4. Reduce type-specific duplication

**Example:**

Before:

```go
func SaveUser(user User) error {
    if err := validateUser(user); err != nil {
        return err
    }
    db := getDB()
    return db.Save("users", user)
}

func SaveProduct(product Product) error {
    if err := validateProduct(product); err != nil {
        return err
    }
    db := getDB()
    return db.Save("products", product)
}
```

After:

```go
type Validatable interface {
    Validate() error
}

type Saveable interface {
    Validatable
    TableName() string
}

func (u User) Validate() error {
    // validation logic
    return nil
}

func (u User) TableName() string {
    return "users"
}

func (p Product) Validate() error {
    // validation logic
    return nil
}

func (p Product) TableName() string {
    return "products"
}

func Save(entity Saveable) error {
    if err := entity.Validate(); err != nil {
        return err
    }
    db := getDB()
    return db.Save(entity.TableName(), entity)
}
```

## Validation Steps

After eliminating duplication:

1. **Build:** Ensure code compiles

   ```bash
   go build ./...
   ```

2. **Test:** Verify existing tests pass

   ```bash
   go test ./...
   ```

3. **Lint:** Check for new issues

   ```bash
   golangci-lint run --timeout 5m
   ```

4. **Duplication:** Verify duplication is reduced

   ```bash
   jscpd --config .jscpd.json
   ```

   Check that the duplication report shows improvement.

## Best Practices

1. **Don't over-abstract:** Some duplication is acceptable if abstraction hurts clarity
2. **Extract after 2-3 occurrences:** One occurrence doesn't justify extraction
3. **Keep extracted code close:** Put helpers near where they're used
4. **Prefer composition over inheritance:** Go favors composition
5. **Use packages wisely:** Don't create utility packages too eagerly
6. **Parameterize differences:** Make extracted functions flexible but not overly generic

## When NOT to Remove Duplication

1. **Coincidental duplication:** Code that looks similar but represents different concepts
2. **Test code:** Some duplication in tests is acceptable for clarity
3. **Configuration:** Similar configuration blocks that represent different entities
4. **Temporary duplication:** During active development, wait for patterns to emerge

## Common Mistakes

1. **Premature abstraction:** Creating abstractions before understanding the pattern
2. **Over-parameterization:** Making functions too generic and hard to understand
3. **Breaking cohesion:** Moving related code to generic utility packages
4. **Ignoring context:** Not all similar code represents the same concept
5. **Creating god packages:** Dumping everything into a single utility package
