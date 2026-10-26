//go:build e2e

package e2e

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAgentRestart(t *testing.T) {
	ns := newNamespace(t, twoLeaves)

	startPod(t, ns, "leaf1", `{"device":"leaf1"}`)
	startPod(t, ns, "leaf2", `{"device":"leaf2"}`)
	waitReady(t, ns, "leaf1")
	waitReady(t, ns, "leaf2")
	setIP(t, ns, "leaf1", "192.168.0.1/24")
	setIP(t, ns, "leaf2", "192.168.0.2/24")
	waitPing(t, ns, "leaf1", "192.168.0.2")

	parkBefore, err := command("", "docker", "exec", node, "stat", "-L", "-c", "%i", "/run/netns/patchpanel-park")
	if err != nil {
		t.Fatal(err)
	}
	stubsBefore := stubAliases(t, ns)
	if len(stubsBefore) != 2 {
		t.Fatalf("want 2 stubs before the restart, got %v", stubsBefore)
	}

	oldAgent := kubectl(t, "-n", "patchpanel-system", "get", "pod", "-l", "app=patchpanel-agent", "-o", "jsonpath={.items[0].metadata.name}")
	kubectl(t, "-n", "patchpanel-system", "delete", "pod", oldAgent, "--timeout=120s")

	// The data plane is the kernel: it forwards without an agent.
	waitPing(t, ns, "leaf1", "192.168.0.2")

	eventually(t, 120*time.Second, "new agent to be ready", func() error {
		out, err := kubectlTry("", "-n", "patchpanel-system", "get", "pod", "-l", "app=patchpanel-agent", "-o",
			"jsonpath={.items[0].metadata.name} {.items[0].status.containerStatuses[0].ready}")
		if err != nil {
			return err
		}
		if out == "" || strings.HasPrefix(out, oldAgent) || !strings.HasSuffix(out, " true") {
			return fmt.Errorf("agent is %q", out)
		}

		return nil
	})

	parkAfter, err := command("", "docker", "exec", node, "stat", "-L", "-c", "%i", "/run/netns/patchpanel-park")
	if err != nil {
		t.Fatal(err)
	}
	if parkAfter != parkBefore {
		t.Errorf("park netns changed from inode %s to %s", parkBefore, parkAfter)
	}
	if stubsAfter := stubAliases(t, ns); !reflect.DeepEqual(stubsAfter, stubsBefore) {
		t.Errorf("stubs changed from %v to %v", stubsBefore, stubsAfter)
	}

	waitCarrier(t, ns, "leaf1", "eth1", "1")
	waitPing(t, ns, "leaf1", "192.168.0.2")
	waitPing(t, ns, "leaf2", "192.168.0.1")
}

func TestCleanupAfterPodDeletion(t *testing.T) {
	ns := newNamespace(t, twoLeaves)

	startPod(t, ns, "leaf1", `{"device":"leaf1"}`)
	startPod(t, ns, "leaf2", `{"device":"leaf2"}`)
	waitReady(t, ns, "leaf1")
	waitReady(t, ns, "leaf2")

	deletePod(t, ns, "leaf1")
	eventually(t, 60*time.Second, "DeviceAssociation and stub of leaf1 to be gone", func() error {
		held := holders(t, ns)
		stubs := stubAliases(t, ns)
		if len(held) != 1 || held["leaf2"] != "leaf2" || len(stubs) != 1 || stubs[0] != ns+"/"+topology+"/leaf2/eth1" {
			return fmt.Errorf("associations %v, stubs %v", held, stubs)
		}

		return nil
	})

	deletePod(t, ns, "leaf2")
	eventually(t, 60*time.Second, "all DeviceAssociations and stubs to be gone", func() error {
		held := holders(t, ns)
		stubs := stubAliases(t, ns)
		if len(held) != 0 || len(stubs) != 0 {
			return fmt.Errorf("associations %v, stubs %v", held, stubs)
		}

		return nil
	})
}
