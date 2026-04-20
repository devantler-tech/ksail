package timer_test

import (
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/timer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockTimerStart(t *testing.T) {
	t.Parallel()

	t.Run("Start can be called", func(t *testing.T) {
		t.Parallel()

		m := timer.NewMockTimer(t)
		m.EXPECT().Start().Return()
		m.Start()
	})

	t.Run("Start RunAndReturn", func(t *testing.T) {
		t.Parallel()

		called := false
		m := timer.NewMockTimer(t)
		m.EXPECT().Start().RunAndReturn(func() {
			called = true
		})
		m.Start()

		assert.True(t, called)
	})
}

func TestMockTimerStop(t *testing.T) {
	t.Parallel()

	t.Run("Stop can be called", func(t *testing.T) {
		t.Parallel()

		m := timer.NewMockTimer(t)
		m.EXPECT().Stop().Return()
		m.Stop()
	})

	t.Run("Stop RunAndReturn", func(t *testing.T) {
		t.Parallel()

		called := false
		m := timer.NewMockTimer(t)
		m.EXPECT().Stop().RunAndReturn(func() {
			called = true
		})
		m.Stop()

		assert.True(t, called)
	})
}

func TestMockTimerNewStage(t *testing.T) {
	t.Parallel()

	t.Run("NewStage can be called", func(t *testing.T) {
		t.Parallel()

		m := timer.NewMockTimer(t)
		m.EXPECT().NewStage().Return()
		m.NewStage()
	})

	t.Run("NewStage RunAndReturn", func(t *testing.T) {
		t.Parallel()

		called := false
		m := timer.NewMockTimer(t)
		m.EXPECT().NewStage().RunAndReturn(func() {
			called = true
		})
		m.NewStage()

		assert.True(t, called)
	})
}

func TestMockTimerGetTiming(t *testing.T) {
	t.Parallel()

	t.Run("GetTiming returns configured values", func(t *testing.T) {
		t.Parallel()

		m := timer.NewMockTimer(t)
		expectedTotal := 5 * time.Second
		expectedStage := 2 * time.Second
		m.EXPECT().GetTiming().Return(expectedTotal, expectedStage)

		total, stage := m.GetTiming()

		assert.Equal(t, expectedTotal, total)
		assert.Equal(t, expectedStage, stage)
	})

	t.Run("GetTiming RunAndReturn", func(t *testing.T) {
		t.Parallel()

		m := timer.NewMockTimer(t)
		m.EXPECT().GetTiming().RunAndReturn(func() (time.Duration, time.Duration) {
			return 10 * time.Second, 3 * time.Second
		})

		total, stage := m.GetTiming()

		assert.Equal(t, 10*time.Second, total)
		assert.Equal(t, 3*time.Second, stage)
	})

	t.Run("GetTiming with zero values", func(t *testing.T) {
		t.Parallel()

		m := timer.NewMockTimer(t)
		m.EXPECT().GetTiming().Return(time.Duration(0), time.Duration(0))

		total, stage := m.GetTiming()

		assert.Equal(t, time.Duration(0), total)
		assert.Equal(t, time.Duration(0), stage)
	})
}

//nolint:varnamelen // Short names keep the table-driven tests readable.
func TestMockTimerFullWorkflow(t *testing.T) {
	t.Parallel()

	m := timer.NewMockTimer(t)

	// Set up expectations for a full timer workflow
	m.EXPECT().Start().Return()
	m.EXPECT().GetTiming().Return(100*time.Millisecond, 100*time.Millisecond).Once()
	m.EXPECT().NewStage().Return()
	m.EXPECT().GetTiming().Return(150*time.Millisecond, 50*time.Millisecond).Once()
	m.EXPECT().Stop().Return()

	// Execute the workflow
	m.Start()

	total, stage := m.GetTiming()
	assert.Equal(t, 100*time.Millisecond, total)
	assert.Equal(t, 100*time.Millisecond, stage)

	m.NewStage()

	total, stage = m.GetTiming()
	assert.Equal(t, 150*time.Millisecond, total)
	assert.Equal(t, 50*time.Millisecond, stage)

	m.Stop()
}

func TestMockTimerImplementsInterface(t *testing.T) {
	t.Parallel()

	m := timer.NewMockTimer(t)

	// Verify MockTimer satisfies the Timer interface at compile time
	var _ timer.Timer = m
	require.NotNil(t, m)
}

func TestImplStop(t *testing.T) {
	t.Parallel()

	t.Run("Stop is a no-op on started timer", func(t *testing.T) {
		t.Parallel()

		tmr := timer.New()
		tmr.Start()
		time.Sleep(20 * time.Millisecond)

		tmr.Stop()

		// Timer state should still be accessible after Stop
		total, stage := tmr.GetTiming()

		assert.Greater(t, total, time.Duration(0), "total should be > 0 after Stop")
		assert.Greater(t, stage, time.Duration(0), "stage should be > 0 after Stop")
	})

	t.Run("Stop on unstarted timer does not panic", func(t *testing.T) {
		t.Parallel()

		tmr := timer.New()

		require.NotPanics(t, func() {
			tmr.Stop()
		})

		total, stage := tmr.GetTiming()
		assert.Equal(t, time.Duration(0), total)
		assert.Equal(t, time.Duration(0), stage)
	})

	t.Run("Stop can be called multiple times safely", func(t *testing.T) {
		t.Parallel()

		tmr := timer.New()
		tmr.Start()

		require.NotPanics(t, func() {
			tmr.Stop()
			tmr.Stop()
			tmr.Stop()
		})
	})

	t.Run("NewStage works after Stop", func(t *testing.T) {
		t.Parallel()

		tmr := timer.New()
		tmr.Start()
		time.Sleep(30 * time.Millisecond)

		tmr.Stop()
		tmr.NewStage()

		total, stage := tmr.GetTiming()

		// Total should still be tracking from original Start
		assert.Greater(t, total, time.Duration(0))
		// Stage should be near zero after NewStage
		assert.Less(t, stage, total)
	})
}
