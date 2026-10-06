package awssso_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/internal/testutil"
	"github.com/devantler-tech/ksail/v7/pkg/svc/awssso"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestRenewDirectLegacyAndNamedSessions(t *testing.T) {
	t.Parallel()

	for _, profile := range []string{"legacy", "modern"} {
		t.Run(profile, func(t *testing.T) {
			t.Parallel()
			fixture := testutil.NewSyntheticSSO(t, profile)
			target, err := awssso.Resolve(t.Context(), fixture.Provider)
			require.NoError(t, err)
			expired, err := target.Expired(t.Context())
			require.NoError(t, err)
			require.True(t, expired)

			var manager awssso.Manager
			require.NoError(t, manager.Renew(t.Context(), target))
			expired, err = target.Expired(t.Context())
			require.NoError(t, err)
			assert.False(t, expired)

			logins, err := os.ReadFile(filepath.Join(fixture.Root, "logins"))
			require.NoError(t, err)
			assert.Contains(t, string(logins), "--profile="+profile)
			assert.Equal(t, 1, strings.Count(string(logins), "\n"))
		})
	}
}

func TestSelectedArgumentProfileWinsOverExecEnvironment(t *testing.T) {
	t.Parallel()
	fixture := testutil.NewSyntheticSSO(t, "legacy")
	fixture.Provider.Args = append(fixture.Provider.Args, "--profile=modern")
	target, err := awssso.Resolve(t.Context(), fixture.Provider)
	require.NoError(t, err)

	var manager awssso.Manager
	require.NoError(t, manager.Renew(t.Context(), target))

	logins, err := os.ReadFile(filepath.Join(fixture.Root, "logins"))
	require.NoError(t, err)
	assert.Contains(t, string(logins), "--profile=modern")
}

func TestRenewCoalescesProfilesSharingOneSession(t *testing.T) {
	t.Parallel()
	fixture := testutil.NewSyntheticSSO(t, "modern")
	first, err := awssso.Resolve(t.Context(), fixture.Provider)
	require.NoError(t, err)

	fixture.Provider.Env = append(
		fixture.Provider.Env,
		clientcmdapi.ExecEnvVar{Name: "AWS_PROFILE", Value: "sibling"},
	)
	second, err := awssso.Resolve(t.Context(), fixture.Provider)
	require.NoError(t, err)

	var (
		manager awssso.Manager
		workers sync.WaitGroup
	)

	results := make(chan error, 12)

	for index := range 12 {
		workers.Go(func() {
			target := first
			if index%2 == 1 {
				target = second
			}

			results <- manager.Renew(t.Context(), target)
		})
	}

	workers.Wait()
	close(results)

	for err := range results {
		require.NoError(t, err)
	}

	logins, err := os.ReadFile(filepath.Join(fixture.Root, "logins"))
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(logins), "\n"))
}

func TestUnsupportedAndUnrelatedErrorsNeverSignIn(t *testing.T) {
	t.Parallel()
	t.Run("not SSO", func(t *testing.T) {
		t.Parallel()
		fixture := testutil.NewSyntheticSSO(t, "other")
		_, err := awssso.Resolve(t.Context(), fixture.Provider)
		require.ErrorIs(t, err, awssso.ErrUnsupported)
		_, err = os.Stat(filepath.Join(fixture.Root, "logins"))
		assert.ErrorIs(t, err, os.ErrNotExist)
	})
	t.Run("unrelated command", func(t *testing.T) {
		t.Parallel()
		fixture := testutil.NewSyntheticSSO(t, "legacy")
		fixture.Provider.Args = []string{"sso", "login", "eks", "get-token"}
		_, err := awssso.Resolve(t.Context(), fixture.Provider)
		assert.ErrorIs(t, err, awssso.ErrUnsupported)
	})
	t.Run("access denied", func(t *testing.T) {
		t.Parallel()
		fixture := testutil.NewSyntheticSSO(t, "legacy")
		fixture.Provider.Env = append(fixture.Provider.Env,
			clientcmdapi.ExecEnvVar{Name: "FAKE_PROBE_FAILURE", Value: "yes"})
		target, err := awssso.Resolve(t.Context(), fixture.Provider)
		require.NoError(t, err)

		var manager awssso.Manager

		err = manager.Renew(t.Context(), target)
		require.ErrorIs(t, err, awssso.ErrCredentialCheck)
		assert.NotContains(t, err.Error(), "SENSITIVE")
		_, err = os.Stat(filepath.Join(fixture.Root, "logins"))
		assert.ErrorIs(t, err, os.ErrNotExist)
	})
}

func TestFailedAndCancelledLoginIsExplicitAndPrivate(t *testing.T) {
	t.Parallel()

	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "failed", true: "cancelled"}[cancelled], func(t *testing.T) {
			t.Parallel()
			fixture := testutil.NewSyntheticSSO(t, "legacy")

			key := "FAKE_LOGIN_FAIL"
			if cancelled {
				key = "FAKE_LOGIN_BLOCK"
			}

			fixture.Provider.Env = append(
				fixture.Provider.Env,
				clientcmdapi.ExecEnvVar{Name: key, Value: "yes"},
			)
			target, err := awssso.Resolve(t.Context(), fixture.Provider)
			require.NoError(t, err)
			ctx := t.Context()

			if cancelled {
				var cancel context.CancelFunc

				ctx, cancel = context.WithTimeout(ctx, 150*time.Millisecond)
				defer cancel()
			}

			var manager awssso.Manager

			err = manager.Renew(ctx, target)
			if cancelled {
				require.ErrorIs(t, err, context.DeadlineExceeded)
			} else {
				require.ErrorIs(t, err, awssso.ErrLoginFailed)
			}

			require.Error(t, err)
			assert.NotContains(t, err.Error(), "SENSITIVE")
		})
	}
}

func TestSharedSignInSurvivesItsFirstCallerLeaving(t *testing.T) {
	t.Parallel()
	fixture := testutil.NewSyntheticSSO(t, "legacy")
	fixture.Provider.Env = append(
		fixture.Provider.Env,
		clientcmdapi.ExecEnvVar{Name: "FAKE_LOGIN_GATE", Value: "yes"},
	)
	target, err := awssso.Resolve(t.Context(), fixture.Provider)
	require.NoError(t, err)

	var manager awssso.Manager

	firstCtx, leave := context.WithCancel(t.Context())
	defer leave()

	first, second := make(chan error, 1), make(chan error, 1)

	go func() { first <- manager.Renew(firstCtx, target) }()

	logins := filepath.Join(fixture.Root, "logins")

	require.Eventually(t, func() bool {
		_, statErr := os.Stat(logins)

		return statErr == nil
	}, time.Minute, 10*time.Millisecond, "the first caller starts the provider sign-in")

	go func() { second <- manager.Renew(t.Context(), target) }()

	require.Eventually(t, func() bool { return manager.Waiters(target) == 2 },
		time.Minute, 10*time.Millisecond, "the second caller joins the sign-in in progress")
	leave()
	require.ErrorIs(t, <-first, context.Canceled)

	// The provider only finishes now, after the caller that started the sign-in has left.
	require.NoError(t, os.WriteFile(filepath.Join(fixture.Root, "release"), nil, 0o600))
	require.NoError(t, <-second, "a remaining caller still receives the completed sign-in")

	recorded, err := os.ReadFile(logins) //nolint:gosec // Test-created fixture path.
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(recorded), "\n"), "both callers shared one sign-in")
}

func TestSignInNobodyAwaitsIsStoppedAndNotReused(t *testing.T) {
	t.Parallel()
	fixture := testutil.NewSyntheticSSO(t, "legacy")
	fixture.Provider.Env = append(
		fixture.Provider.Env,
		clientcmdapi.ExecEnvVar{Name: "FAKE_LOGIN_BLOCK", Value: "yes"},
	)
	target, err := awssso.Resolve(t.Context(), fixture.Provider)
	require.NoError(t, err)

	var manager awssso.Manager

	pids := filepath.Join(fixture.Root, "pids")

	for attempt := 1; attempt <= 2; attempt++ {
		ctx, cancel := context.WithCancel(t.Context())
		result := make(chan error, 1)

		go func() { result <- manager.Renew(ctx, target) }()

		var started []string

		require.Eventually(t, func() bool {
			recorded, _ := os.ReadFile(pids) //nolint:gosec // Test-created fixture path.
			started = strings.Fields(string(recorded))

			return len(started) == attempt
		}, time.Minute, 10*time.Millisecond,
			"each request starts its own sign-in instead of joining an abandoned one")

		cancel()
		require.ErrorIs(t, <-result, context.Canceled)

		pid, err := strconv.Atoi(started[attempt-1])
		require.NoError(t, err)
		process, err := os.FindProcess(pid)
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			return errors.Is(process.Signal(syscall.Signal(0)), os.ErrProcessDone)
		}, 20*time.Second, 10*time.Millisecond, "the provider process nobody awaits is stopped")
	}
}

func TestDeviceCodeSignInShowsInstructionsOnlyOnTheTerminal(t *testing.T) {
	t.Parallel()
	t.Run("renewed", func(t *testing.T) {
		t.Parallel()
		fixture := testutil.NewSyntheticSSO(t, "legacy")
		target, err := awssso.Resolve(t.Context(), fixture.Provider)
		require.NoError(t, err)

		var (
			manager  awssso.Manager
			terminal bytes.Buffer
		)

		require.NoError(t, manager.RenewWithDeviceCode(t.Context(), target, &terminal))
		assert.Contains(t, terminal.String(), "SYNTHETIC-CODE")

		logins, err := os.ReadFile(filepath.Join(fixture.Root, "logins"))
		require.NoError(t, err)
		assert.Contains(t, string(logins), "--use-device-code")
	})
	t.Run("failed", func(t *testing.T) {
		t.Parallel()
		fixture := testutil.NewSyntheticSSO(t, "legacy")
		fixture.Provider.Env = append(
			fixture.Provider.Env,
			clientcmdapi.ExecEnvVar{Name: "FAKE_LOGIN_FAIL", Value: "yes"},
		)
		target, err := awssso.Resolve(t.Context(), fixture.Provider)
		require.NoError(t, err)

		var (
			manager  awssso.Manager
			terminal bytes.Buffer
		)

		err = manager.RenewWithDeviceCode(t.Context(), target, &terminal)
		require.ErrorIs(t, err, awssso.ErrLoginFailed)
		assert.NotContains(t, err.Error(), "SENSITIVE", "provider output stays on the terminal")
	})
	t.Run("browser flow", func(t *testing.T) {
		t.Parallel()
		fixture := testutil.NewSyntheticSSO(t, "legacy")
		target, err := awssso.Resolve(t.Context(), fixture.Provider)
		require.NoError(t, err)

		var manager awssso.Manager

		require.NoError(t, manager.Renew(t.Context(), target))

		logins, err := os.ReadFile(filepath.Join(fixture.Root, "logins"))
		require.NoError(t, err)
		assert.NotContains(t, string(logins), "--use-device-code")
	})
}
