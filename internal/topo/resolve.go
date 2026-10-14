package topo

import (
	"fmt"
	"slices"
	"strings"

	"github.com/squ94wk/dc-net-cni/api/v1alpha1"
)

type Endpoint struct {
	Namespace, Topology, Device, Interface string
}

// Port is one interface of a device and the interface it is cabled to.
type Port struct {
	Local, Peer Endpoint
}

// Ports returns the ports of device sorted by local interface name.
func Ports(ns, name string, spec v1alpha1.TopologySpec, device string) ([]Port, error) {
	if !slices.ContainsFunc(spec.Devices, func(d v1alpha1.Device) bool { return d.Name == device }) {
		return nil, fmt.Errorf("failed to find device %q in Topology %s/%s", device, ns, name)
	}

	end := func(e v1alpha1.LinkEnd) Endpoint {
		return Endpoint{Namespace: ns, Topology: name, Device: e.Device, Interface: e.Interface}
	}

	var ports []Port
	for _, l := range spec.Links {
		if l.A.Device == device {
			ports = append(ports, Port{Local: end(l.A), Peer: end(l.B)})
		}
		if l.B.Device == device {
			ports = append(ports, Port{Local: end(l.B), Peer: end(l.A)})
		}
	}

	slices.SortFunc(ports, func(a, b Port) int { return strings.Compare(a.Local.Interface, b.Local.Interface) })

	return ports, nil
}

// GroupMembers returns the names of the devices in group, sorted.
func GroupMembers(spec v1alpha1.TopologySpec, group string) []string {
	var members []string
	for _, d := range spec.Devices {
		if d.Group == group {
			members = append(members, d.Name)
		}
	}

	slices.Sort(members)

	return members
}
