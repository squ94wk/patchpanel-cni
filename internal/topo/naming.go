package topo

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
)

// AssociationName is deterministic so that a second Create for the same device fails with AlreadyExists.
func AssociationName(ns, topology, device string) string {
	sum := sha256.Sum256([]byte(ns + "/" + topology + "/" + device))
	return "da-" + hex.EncodeToString(sum[:])[:20]
}

// Alias is the full identity of a stub, stored in its ifalias.
func Alias(e Endpoint) string {
	return e.Namespace + "/" + e.Topology + "/" + e.Device + "/" + e.Interface
}

// ParseAlias takes namespace and topology from the left and the interface from the right.
// The device is everything in between and may contain "/".
func ParseAlias(s string) (Endpoint, error) {
	parts := strings.Split(s, "/")
	if len(parts) < 4 {
		return Endpoint{}, fmt.Errorf("failed to parse alias %q: want namespace/topology/device/interface", s)
	}

	return Endpoint{
		Namespace: parts[0],
		Topology:  parts[1],
		Device:    strings.Join(parts[2:len(parts)-1], "/"),
		Interface: parts[len(parts)-1],
	}, nil
}

// StubName is a short hash because the kernel limits interface names to 15 characters.
func StubName(e Endpoint) string {
	sum := sha256.Sum256([]byte(Alias(e)))
	return "dc" + hex.EncodeToString(sum[:])[:13]
}

// MAC is a unicast, locally administered address derived from the endpoint.
func MAC(e Endpoint) net.HardwareAddr {
	sum := sha256.Sum256([]byte(Alias(e)))
	mac := net.HardwareAddr(sum[:6])
	mac[0] = mac[0]&^1 | 2

	return mac
}
