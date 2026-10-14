package topo_test

import (
	"slices"
	"testing"

	"github.com/squ94wk/patchpanel-cni/api/v1alpha1"
	"github.com/squ94wk/patchpanel-cni/internal/topo"
)

func link(ad, ai, bd, bi string) v1alpha1.Link {
	return v1alpha1.Link{
		A: v1alpha1.LinkEnd{Device: ad, Interface: ai},
		B: v1alpha1.LinkEnd{Device: bd, Interface: bi},
	}
}

func TestPortsSortedWithPeers(t *testing.T) {
	spec := v1alpha1.TopologySpec{
		Devices: []v1alpha1.Device{{Name: "leaf1"}, {Name: "s1"}, {Name: "s2"}},
		Links: []v1alpha1.Link{
			link("leaf1", "eth3", "s2", "eth1"),
			link("s1", "eth1", "leaf1", "eth2"),
		},
	}

	ports, err := topo.Ports("ns", "lab", spec, "leaf1")
	if err != nil {
		t.Fatal(err)
	}

	want := []topo.Port{
		{
			Local: topo.Endpoint{Namespace: "ns", Topology: "lab", Device: "leaf1", Interface: "eth2"},
			Peer:  topo.Endpoint{Namespace: "ns", Topology: "lab", Device: "s1", Interface: "eth1"},
		},
		{
			Local: topo.Endpoint{Namespace: "ns", Topology: "lab", Device: "leaf1", Interface: "eth3"},
			Peer:  topo.Endpoint{Namespace: "ns", Topology: "lab", Device: "s2", Interface: "eth1"},
		},
	}
	if !slices.Equal(ports, want) {
		t.Fatalf("got %v, want %v", ports, want)
	}
}

func TestPortsUnknownDevice(t *testing.T) {
	spec := v1alpha1.TopologySpec{Devices: []v1alpha1.Device{{Name: "leaf1"}}}

	_, err := topo.Ports("ns", "lab", spec, "nope")
	if err == nil {
		t.Fatal("want error for unknown device")
	}
}

func TestPortsDeviceWithoutLinks(t *testing.T) {
	spec := v1alpha1.TopologySpec{
		Devices: []v1alpha1.Device{{Name: "a"}, {Name: "b"}, {Name: "c"}},
		Links:   []v1alpha1.Link{link("a", "eth1", "b", "eth1")},
	}

	ports, err := topo.Ports("ns", "lab", spec, "c")
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 0 {
		t.Fatalf("got %v, want no ports", ports)
	}
}

func TestPortsSelfLoop(t *testing.T) {
	spec := v1alpha1.TopologySpec{
		Devices: []v1alpha1.Device{{Name: "a"}},
		Links:   []v1alpha1.Link{link("a", "eth2", "a", "eth1")},
	}

	ports, err := topo.Ports("ns", "lab", spec, "a")
	if err != nil {
		t.Fatal(err)
	}

	if len(ports) != 2 {
		t.Fatalf("got %v, want both ends as ports", ports)
	}
	if ports[0].Local.Interface != "eth1" || ports[0].Peer.Interface != "eth2" {
		t.Errorf("first port: got %v", ports[0])
	}
	if ports[1].Local.Interface != "eth2" || ports[1].Peer.Interface != "eth1" {
		t.Errorf("second port: got %v", ports[1])
	}
}

func TestGroupMembersSorted(t *testing.T) {
	spec := v1alpha1.TopologySpec{Devices: []v1alpha1.Device{
		{Name: "srv3", Group: "servers"},
		{Name: "leaf1", Group: "leaves"},
		{Name: "srv1", Group: "servers"},
		{Name: "lone"},
	}}

	got := topo.GroupMembers(spec, "servers")
	if !slices.Equal(got, []string{"srv1", "srv3"}) {
		t.Fatalf("got %v", got)
	}
}

func TestGroupMembersUnknownGroup(t *testing.T) {
	spec := v1alpha1.TopologySpec{Devices: []v1alpha1.Device{{Name: "a", Group: "g"}, {Name: "b"}}}

	got := topo.GroupMembers(spec, "nope")
	if len(got) != 0 {
		t.Fatalf("got %v, want empty", got)
	}
}
