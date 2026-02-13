# Go Refactoring Guide: Function Complexity and Code Smells

This guide covers common code smells in Go and how to refactor them incrementally while preserving behavior.

## Common Code Smells

### 1. Long Functions (God Functions)

**Smell:** Functions exceeding ~50 lines or handling multiple responsibilities.

**Detection:**
```bash
# Find long functions
grep -r "^func " --include="*.go" | while read line; do
  file=$(echo $line | cut -d: -f1)
  func=$(echo $line | cut -d: -f2-)
  wc -l "$file" | awk '{if ($1 > 50) print}'
done
```

**Refactoring Strategy:**
1. Identify logical blocks within the function
2. Extract helper functions for each block
3. Name helpers descriptively based on what they do
4. Keep helper functions close to where they're used
5. Use meaningful return values and errors

**Example:**

Before:
```go
func ProcessOrder(order Order) error {
    // Validate order (10 lines)
    if order.ID == "" {
        return errors.New("missing order ID")
    }
    // ... more validation
    
    // Calculate totals (15 lines)
    var total float64
    for _, item := range order.Items {
        total += item.Price * float64(item.Quantity)
    }
    // ... tax and shipping calculations
    
    // Save to database (20 lines)
    db := getDB()
    // ... database operations
    
    return nil
}
```

After:
```go
func ProcessOrder(order Order) error {
    if err := validateOrder(order); err != nil {
        return fmt.Errorf("validation failed: %w", err)
    }
    
    total := calculateOrderTotal(order)
    order.Total = total
    
    if err := saveOrder(order); err != nil {
        return fmt.Errorf("failed to save order: %w", err)
    }
    
    return nil
}

func validateOrder(order Order) error {
    if order.ID == "" {
        return errors.New("missing order ID")
    }
    // ... other validation
    return nil
}

func calculateOrderTotal(order Order) float64 {
    var total float64
    for _, item := range order.Items {
        total += item.Price * float64(item.Quantity)
    }
    // ... tax and shipping calculations
    return total
}

func saveOrder(order Order) error {
    db := getDB()
    // ... database operations
    return nil
}
```

### 2. Deep Nesting

**Smell:** Functions with more than 3-4 levels of nesting.

**Refactoring Strategy:**
1. Use early returns (guard clauses) to reduce nesting
2. Invert conditions where appropriate
3. Extract nested logic into separate functions
4. Prefer flat, left-aligned code

**Example:**

Before:
```go
func ProcessFile(path string) error {
    if fileExists(path) {
        if hasPermission(path) {
            data, err := readFile(path)
            if err == nil {
                if validateData(data) {
                    return saveData(data)
                } else {
                    return errors.New("invalid data")
                }
            } else {
                return err
            }
        } else {
            return errors.New("no permission")
        }
    } else {
        return errors.New("file not found")
    }
}
```

After:
```go
func ProcessFile(path string) error {
    if !fileExists(path) {
        return errors.New("file not found")
    }
    
    if !hasPermission(path) {
        return errors.New("no permission")
    }
    
    data, err := readFile(path)
    if err != nil {
        return fmt.Errorf("failed to read file: %w", err)
    }
    
    if !validateData(data) {
        return errors.New("invalid data")
    }
    
    return saveData(data)
}
```

### 3. Primitive Obsession

**Smell:** Using primitive types (string, int) instead of domain types.

**Refactoring Strategy:**
1. Create domain types for concepts
2. Add validation in constructors
3. Add methods to domain types
4. Use type safety to prevent invalid states

**Example:**

Before:
```go
func CreateUser(email string, age int) error {
    if !strings.Contains(email, "@") {
        return errors.New("invalid email")
    }
    if age < 0 || age > 150 {
        return errors.New("invalid age")
    }
    // ... save user
    return nil
}
```

After:
```go
type Email string

func NewEmail(s string) (Email, error) {
    if !strings.Contains(s, "@") {
        return "", errors.New("invalid email format")
    }
    return Email(s), nil
}

type Age int

func NewAge(a int) (Age, error) {
    if a < 0 || a > 150 {
        return 0, errors.New("invalid age")
    }
    return Age(a), nil
}

type User struct {
    Email Email
    Age   Age
}

func CreateUser(email Email, age Age) error {
    user := User{Email: email, Age: age}
    // ... save user
    return nil
}
```

## Validation Steps

After each refactoring:

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

4. **Format:** Apply formatting
   ```bash
   golangci-lint fmt
   ```

5. **Duplication:** Check for new duplication
   ```bash
   jscpd --config .jscpd.json
   ```

## Best Practices

1. **Make small changes:** One refactoring at a time
2. **Test after each change:** Don't accumulate untested changes
3. **Keep behavior unchanged:** Refactoring should not change external behavior
4. **Add tests for extracted functions:** If a helper is non-trivial
5. **Document why, not what:** Use comments sparingly, prefer self-documenting code
6. **Preserve error context:** Use `fmt.Errorf` with `%w` to wrap errors

## Common Mistakes

1. **Changing behavior while refactoring:** Keep refactoring separate from feature work
2. **Extracting functions that are only used once:** Only extract if it improves clarity
3. **Over-abstracting:** Don't create abstractions until you have 2-3 similar cases
4. **Ignoring test failures:** Fix immediately, don't continue refactoring
5. **Not running linters:** Always check for new lint issues before committing
