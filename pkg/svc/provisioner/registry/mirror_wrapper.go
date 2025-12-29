package registry

import (
	"net"
	"strconv"
	"strings"

	registryutil "github.com/devantler-tech/ksail/v5/pkg/registry"
)

// BuildRegistryInfosFromSpecs builds registry Info structs from mirror specs directly.
// This is used when provisioners need Info structs for SetupRegistries/CleanupRegistries.
// The upstreams map provides optional overrides for upstream URLs per host.
func BuildRegistryInfosFromSpecs(
	mirrorSpecs []registryutil.MirrorSpec,
	upstreams map[string]string,
	baseUsedPorts map[int]struct{},
) []Info {
	registryInfos := make([]Info, 0, len(mirrorSpecs))

	usedPorts, nextPort := registryutil.InitPortAllocation(baseUsedPorts)

	for _, spec := range mirrorSpecs {
		host := strings.TrimSpace(spec.Host)
		if host == "" {
			continue
		}

		// Build endpoint for this host
		port := registryutil.AllocatePort(&nextPort, usedPorts)
		endpoint := "http://" + net.JoinHostPort(host, strconv.Itoa(port))

		// Get upstream URL
		upstream := spec.Remote
		if upstream == "" {
			upstream = registryutil.GenerateUpstreamURL(host)
		}

		if upstreams != nil && upstreams[host] != "" {
			upstream = upstreams[host]
		}

		info := BuildRegistryInfo(host, []string{endpoint}, port, "", upstream)
		registryInfos = append(registryInfos, info)
	}

	return registryInfos
}
