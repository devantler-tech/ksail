package hetznerbase

import (
	"io"

	"github.com/devantler-tech/ksail/v7/pkg/addressmask"
)

// progress returns the writer progress lines go to: LogWriter with server
// addresses hidden, unless the operator opted back in through
// [addressmask.ShowAddressesEnvVar]. A nil LogWriter discards the output.
func (b *Base) progress() io.Writer {
	if b.LogWriter == nil {
		return io.Discard
	}

	return addressmask.New().Writer(b.LogWriter)
}
