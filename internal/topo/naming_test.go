package topo_test

import (
	"regexp"
	"testing"

	"github.com/squ94wk/patchpanel-cni/internal/topo"
)

var dns1123 = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func TestAssociationName(t *testing.T) {
	name := topo.AssociationName("ns", "lab", "leaf1")

	if len(name) > 253 || !dns1123.MatchString(name) {
		t.Errorf("%q is not a valid Kubernetes name", name)
	}

	for _, other := range []string{
		topo.AssociationName("ns2", "lab", "leaf1"),
		topo.AssociationName("ns", "lab2", "leaf1"),
		topo.AssociationName("ns", "lab", "leaf2"),
	} {
		if other == name {
			t.Errorf("collision with %q", name)
		}
	}
}

func TestAliasRoundTrip(t *testing.T) {
	for _, e := range []topo.Endpoint{
		{Namespace: "ns", Topology: "lab", Device: "leaf1", Interface: "eth1"},
		{Namespace: "ns", Topology: "lab", Device: "rack1/leaf1", Interface: "eth1"},
	} {
		got, err := topo.ParseAlias(topo.Alias(e))
		if err != nil {
			t.Fatal(err)
		}
		if got != e {
			t.Errorf("got %v, want %v", got, e)
		}
	}
}

func TestParseAliasMalformed(t *testing.T) {
	_, err := topo.ParseAlias("ns/lab/leaf1")
	if err == nil {
		t.Fatal("want error for fewer than 4 parts")
	}
}

func TestStubName(t *testing.T) {
	a := topo.Endpoint{Namespace: "ns", Topology: "lab", Device: "leaf1", Interface: "eth1"}
	b := topo.Endpoint{Namespace: "ns", Topology: "lab", Device: "leaf1", Interface: "eth2"}

	if len(topo.StubName(a)) > 15 {
		t.Errorf("%q exceeds IFNAMSIZ", topo.StubName(a))
	}
	if topo.StubName(a) == topo.StubName(b) {
		t.Error("distinct endpoints share a name")
	}
}

func TestMAC(t *testing.T) {
	a := topo.Endpoint{Namespace: "ns", Topology: "lab", Device: "leaf1", Interface: "eth1"}
	b := topo.Endpoint{Namespace: "ns", Topology: "lab", Device: "leaf1", Interface: "eth2"}

	mac := topo.MAC(a)
	if len(mac) != 6 {
		t.Fatalf("got %d bytes", len(mac))
	}
	if mac[0]&2 == 0 {
		t.Error("not locally administered")
	}
	if mac[0]&1 != 0 {
		t.Error("multicast bit set")
	}
	if mac.String() == topo.MAC(b).String() {
		t.Error("distinct endpoints share a MAC")
	}
}
