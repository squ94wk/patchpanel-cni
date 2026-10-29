package agent_test

import (
	"testing"

	"github.com/squ94wk/patchpanel-cni/internal/agent"
	"github.com/squ94wk/patchpanel-cni/internal/protocol"
)

const podArgs = "IgnoreUnknown=1;K8S_POD_NAMESPACE=ns;K8S_POD_NAME=pod1;K8S_POD_INFRA_CONTAINER_ID=abc;K8S_POD_UID=uid1"

func addRequest(stdin, args string) protocol.Request {
	return protocol.Request{Command: "ADD", ContainerID: "abc", Netns: "/var/run/netns/x", Args: args, Stdin: []byte(stdin)}
}

func TestParseInvocationAddDevice(t *testing.T) {
	inv, err := agent.ParseInvocation(addRequest(
		`{"cniVersion":"1.0.0","topology":"lab","args":{"cni":{"device":"leaf1"}}}`, podArgs))
	if err != nil {
		t.Fatal(err)
	}

	want := agent.Invocation{
		Command: "ADD", ContainerID: "abc", Netns: "/var/run/netns/x", CNIVersion: "1.0.0",
		Topology: "lab", Device: "leaf1", Namespace: "ns", PodName: "pod1", PodUID: "uid1",
	}
	if inv != want {
		t.Fatalf("got %+v, want %+v", inv, want)
	}
}

func TestParseInvocationAddGroup(t *testing.T) {
	inv, err := agent.ParseInvocation(addRequest(
		`{"cniVersion":"1.0.0","topology":"lab","args":{"cni":{"group":"servers"}}}`, podArgs))
	if err != nil {
		t.Fatal(err)
	}
	if inv.Group != "servers" || inv.Device != "" {
		t.Fatalf("got %+v", inv)
	}
}

func TestParseInvocationAddDeviceAndGroup(t *testing.T) {
	_, err := agent.ParseInvocation(addRequest(
		`{"topology":"lab","args":{"cni":{"device":"leaf1","group":"servers"}}}`, podArgs))
	if err == nil {
		t.Fatal("want error when both device and group are set")
	}
}

func TestParseInvocationAddPort(t *testing.T) {
	req := addRequest(`{"cniVersion":"1.0.0","topology":"lab","args":{"cni":{"device":"leaf1","interface":"eth1"}}}`, podArgs)
	req.IfName = "pod0123456789a"

	inv, err := agent.ParseInvocation(req)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Device != "leaf1" || inv.Interface != "eth1" || inv.IfName != "pod0123456789a" {
		t.Fatalf("got %+v", inv)
	}
}

func TestParseInvocationAddInterfaceWithoutDevice(t *testing.T) {
	_, err := agent.ParseInvocation(addRequest(
		`{"topology":"lab","args":{"cni":{"group":"servers","interface":"eth1"}}}`, podArgs))
	if err == nil {
		t.Fatal("want error when interface is set with a group")
	}
}

func TestParseInvocationAddNeitherDeviceNorGroup(t *testing.T) {
	_, err := agent.ParseInvocation(addRequest(`{"topology":"lab"}`, podArgs))
	if err == nil {
		t.Fatal("want error when neither device nor group is set")
	}
}

func TestParseInvocationAddMissingTopology(t *testing.T) {
	_, err := agent.ParseInvocation(addRequest(`{"args":{"cni":{"device":"leaf1"}}}`, podArgs))
	if err == nil {
		t.Fatal("want error without topology")
	}
}

func TestParseInvocationAddMissingPodIdentity(t *testing.T) {
	for _, args := range []string{
		"K8S_POD_NAME=pod1;K8S_POD_UID=uid1",
		"K8S_POD_NAMESPACE=ns;K8S_POD_UID=uid1",
		"K8S_POD_NAMESPACE=ns;K8S_POD_NAME=pod1",
	} {
		_, err := agent.ParseInvocation(addRequest(`{"topology":"lab","args":{"cni":{"device":"leaf1"}}}`, args))
		if err == nil {
			t.Errorf("want error for CNI_ARGS %q", args)
		}
	}
}

func TestParseInvocationAddInvalidConfig(t *testing.T) {
	_, err := agent.ParseInvocation(addRequest(`not json`, podArgs))
	if err == nil {
		t.Fatal("want error for invalid stdin")
	}
}

func TestParseInvocationDelNeedsOnlyTopology(t *testing.T) {
	inv, err := agent.ParseInvocation(protocol.Request{
		Command: "DEL", ContainerID: "abc", Args: "K8S_POD_UID=uid1", Stdin: []byte(`{"topology":"lab"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Topology != "lab" || inv.PodUID != "uid1" {
		t.Fatalf("got %+v", inv)
	}
}

func TestParseInvocationDelCarriesInterface(t *testing.T) {
	inv, err := agent.ParseInvocation(protocol.Request{
		Command: "DEL", ContainerID: "abc", Args: "K8S_POD_UID=uid1",
		Stdin: []byte(`{"topology":"lab","args":{"cni":{"device":"leaf1","interface":"eth1"}}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Device != "leaf1" || inv.Interface != "eth1" {
		t.Fatalf("got %+v", inv)
	}
}

func TestParseInvocationDelMissingTopology(t *testing.T) {
	inv, err := agent.ParseInvocation(protocol.Request{Command: "DEL", Stdin: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Topology != "" {
		t.Fatalf("got %+v", inv)
	}
}
