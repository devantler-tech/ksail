package toolgen_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/toolgen"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSessionLogRef(t *testing.T) {
	t.Parallel()

	ref := toolgen.NewSessionLogRef()

	require.NotNil(t, ref)
}

func TestSessionLogRef_Log_NoOpWhenUnset(t *testing.T) {
	t.Parallel()

	ref := toolgen.NewSessionLogRef()

	// Should not panic when the function is not set.
	ref.Log(context.Background(), "hello", "info")
}

func TestSessionLogRef_SetAndLog(t *testing.T) {
	t.Parallel()

	ref := toolgen.NewSessionLogRef()

	var captured struct {
		message string
		level   string
	}

	ref.Set(func(_ context.Context, message, level string) {
		captured.message = message
		captured.level = level
	})

	ref.Log(context.Background(), "test message", "warning")

	assert.Equal(t, "test message", captured.message)
	assert.Equal(t, "warning", captured.level)
}

func TestSessionLogRef_SetOverwritesPrevious(t *testing.T) {
	t.Parallel()

	ref := toolgen.NewSessionLogRef()

	callCount1 := 0
	callCount2 := 0

	ref.Set(func(_ context.Context, _, _ string) { callCount1++ })
	ref.Log(context.Background(), "first", "info")

	ref.Set(func(_ context.Context, _, _ string) { callCount2++ })
	ref.Log(context.Background(), "second", "info")

	assert.Equal(t, 1, callCount1, "first function should have been called once")
	assert.Equal(t, 1, callCount2, "second function should have been called once")
}

//nolint:varnamelen // Short names keep the table-driven tests readable.
func TestSessionLogRef_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	ref := toolgen.NewSessionLogRef()

	var (
		callCount int
		mu        sync.Mutex
	)

	ref.Set(func(_ context.Context, _, _ string) {
		mu.Lock()
		callCount++
		mu.Unlock()
	})

	const goroutines = 50

	var wg sync.WaitGroup

	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()

			ref.Log(context.Background(), "concurrent", "info")
		}()
	}

	wg.Wait()

	mu.Lock()
	defer mu.Unlock()

	assert.Equal(t, goroutines, callCount)
}

func TestDefaultOptions(t *testing.T) {
	t.Parallel()

	opts := toolgen.DefaultOptions()

	assert.Contains(t, opts.ExcludeCommands, "ksail chat")
	assert.Contains(t, opts.ExcludeCommands, "ksail mcp")
	assert.Contains(t, opts.ExcludeCommands, "ksail completion")
	assert.Contains(t, opts.ExcludeCommands, "ksail help")
	assert.Contains(t, opts.ExcludeCommands, "ksail")
	assert.False(t, opts.IncludeHidden)
	assert.Equal(t, 5*time.Minute, opts.CommandTimeout)
	assert.Empty(t, opts.WorkingDirectory)
	assert.Nil(t, opts.OutputChan)
	assert.Nil(t, opts.Logger)
	assert.Nil(t, opts.SessionLog)
}

func TestOutputChunk_Fields(t *testing.T) {
	t.Parallel()

	chunk := toolgen.OutputChunk{
		ToolID: "my_tool",
		Source: "stdout",
		Chunk:  "hello world\n",
	}

	assert.Equal(t, "my_tool", chunk.ToolID)
	assert.Equal(t, "stdout", chunk.Source)
	assert.Equal(t, "hello world\n", chunk.Chunk)
}
