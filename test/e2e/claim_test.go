//go:build e2e

package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/squ94wk/patchpanel-cni/internal/topo"
)

const heldByOthers = "all candidates are held by other pods"

const serverGroup = `
  devices:
    - {name: sw}
    - {name: srv1, group: servers}
    - {name: srv2, group: servers}
  links:
    - {a: {device: sw, interface: eth1}, b: {device: srv1, interface: eth1}}
    - {a: {device: sw, interface: eth2}, b: {device: srv2, interface: eth1}}
`

func waitEvent(t *testing.T, ns, pod, substr string) {
	t.Helper()

	eventually(t, 120*time.Second, "event of "+pod+" containing "+substr, func() error {
		events := podEvents(ns, pod)
		if !strings.Contains(events, substr) {
			return fmt.Errorf("events are: %s", events)
		}

		return nil
	})
}

func TestGroupClaim(t *testing.T) {
	ns := newNamespace(t, serverGroup)

	startPod(t, ns, "g1", `{"group":"servers"}`)
	waitReady(t, ns, "g1")
	startPod(t, ns, "g2", `{"group":"servers"}`)
	waitReady(t, ns, "g2")

	held := holders(t, ns)
	if len(held) != 2 || held["srv1"] != "g1" || held["srv2"] != "g2" {
		t.Fatalf("members are not claimed in name order: %v", held)
	}

	startPod(t, ns, "g3", `{"group":"servers"}`)
	waitEvent(t, ns, "g3", heldByOthers)
	phase := kubectl(t, "-n", ns, "get", "pod", "g3", "-o", "jsonpath={.status.phase}")
	if phase != "Pending" {
		t.Fatalf("g3 is %s while the group is full, want Pending", phase)
	}

	deletePod(t, ns, "g1")
	waitReady(t, ns, "g3")

	held = holders(t, ns)
	if len(held) != 2 || held["srv1"] != "g3" || held["srv2"] != "g2" {
		t.Fatalf("freed member was not handed to g3: %v", held)
	}
}

// A group request must not reuse a held device that is not a member of the group.
func TestGroupRequestWithHeldDeviceOutsideGroup(t *testing.T) {
	ns := newNamespace(t, serverGroup)

	startPodNetworks(t, ns, "p1", fmt.Sprintf(
		`[{"name": "%[1]s", "cni-args": {"device": "sw"}}, {"name": "%[1]s", "cni-args": {"group": "servers"}}]`, topology))
	waitEvent(t, ns, "p1", "Failed to create pod sandbox")

	held := holders(t, ns)
	if len(held) > 1 || (len(held) == 1 && held["sw"] != "p1") {
		t.Fatalf("group request claimed a device outside the group: %v", held)
	}
}

func TestExclusiveDevice(t *testing.T) {
	ns := newNamespace(t, twoLeaves)

	startPod(t, ns, "first", `{"device":"leaf1"}`)
	waitReady(t, ns, "first")

	startPod(t, ns, "second", `{"device":"leaf1"}`)
	waitEvent(t, ns, "second", heldByOthers)

	deletePod(t, ns, "first")
	waitReady(t, ns, "second")

	held := holders(t, ns)
	if len(held) != 1 || held["leaf1"] != "second" {
		t.Fatalf("device was not handed to the second pod: %v", held)
	}
}

// Deleting the association of a running pod cleans its node state. A new pod for the device may race the finalizer.
func TestAssociationDeletedWhilePodRuns(t *testing.T) {
	ns := newNamespace(t, twoLeaves)

	startPod(t, ns, "old", `{"device":"leaf1"}`)
	startPod(t, ns, "peer", `{"device":"leaf2"}`)
	waitReady(t, ns, "old")
	waitReady(t, ns, "peer")
	waitCarrier(t, ns, "peer", "eth1", "1")

	kubectl(t, "-n", ns, "delete", "deviceassociation", topo.AssociationName(ns, topology, "leaf1"), "--wait=false")
	startPod(t, ns, "new", `{"device":"leaf1"}`)
	waitReady(t, ns, "new")

	held := holders(t, ns)
	if len(held) != 2 || held["leaf1"] != "new" || held["leaf2"] != "peer" {
		t.Fatalf("device was not handed to the new pod: %v", held)
	}

	setIP(t, ns, "new", "192.168.0.1/24")
	setIP(t, ns, "peer", "192.168.0.2/24")
	waitCarrier(t, ns, "peer", "eth1", "1")
	waitPing(t, ns, "new", "192.168.0.2")
}
