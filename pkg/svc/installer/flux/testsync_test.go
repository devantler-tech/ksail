package fluxinstaller_test

import (
	"sync"
	"testing"
)

var fluxTestSeamMu sync.Mutex

func lockFluxTestSeams(t *testing.T) {
	t.Helper()

	fluxTestSeamMu.Lock()
	t.Cleanup(fluxTestSeamMu.Unlock)
}
