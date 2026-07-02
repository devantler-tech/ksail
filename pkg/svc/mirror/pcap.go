package mirror

import (
	"errors"
	"fmt"
	"io"

	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcapgo"
)

// CaptureSummary describes a pcap stream produced by a capture session.
type CaptureSummary struct {
	// Packets is the number of packet records in the stream.
	Packets int
	// Bytes is the total captured payload size across all packets.
	Bytes int
	// LinkType is the stream's link-layer header type (LINUX_SLL2 for the
	// tap's "-i any" capture).
	LinkType layers.LinkType
}

// SummarizeCapture reads a pcap stream (as written by the tap's tcpdump) with
// the pure-Go pcapgo reader — no libpcap, no cgo — validating the file header
// and every packet record, and returns what was captured. A truncated or
// corrupt stream is an error.
func SummarizeCapture(reader io.Reader) (*CaptureSummary, error) {
	pcapReader, err := pcapgo.NewReader(reader)
	if err != nil {
		return nil, fmt.Errorf("read pcap header: %w", err)
	}

	summary := &CaptureSummary{
		Packets:  0,
		Bytes:    0,
		LinkType: pcapReader.LinkType(),
	}

	for {
		data, _, err := pcapReader.ReadPacketData()
		if errors.Is(err, io.EOF) {
			return summary, nil
		}

		if err != nil {
			return nil, fmt.Errorf("read pcap packet: %w", err)
		}

		summary.Packets++
		summary.Bytes += len(data)
	}
}
