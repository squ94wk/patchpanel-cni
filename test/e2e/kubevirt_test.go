//go:build e2e

package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/squ94wk/patchpanel-cni/internal/topo"
)

const twoPortLeaves = `
  devices:
    - {name: leaf1}
    - {name: leaf2}
  links:
    - {a: {device: leaf1, interface: eth1}, b: {device: leaf2, interface: eth1}}
    - {a: {device: leaf1, interface: eth2}, b: {device: leaf2, interface: eth2}}
`

// kubevirtIfName is the pod interface name KubeVirt requests for a network: pod<11 hex of the sha256 of the network name>.
func kubevirtIfName(network string) string {
	sum := sha256.Sum256([]byte(network))

	return "pod" + hex.EncodeToString(sum[:])[:11]
}

// startVM creates a pod that attaches one port NAD per interface the way KubeVirt does: no cni-args, a requested interface name.
func startVM(t *testing.T, ns, name, device string, ifaces ...string) {
	t.Helper()

	var networks []string
	for _, iface := range ifaces {
		nad := portNAD(t, ns, device, iface)
		networks = append(networks, fmt.Sprintf(`{"name": "%s", "interface": "%s"}`, nad, kubevirtIfName(nad)))
	}

	startPodNetworks(t, ns, name, "["+strings.Join(networks, ",")+"]")
}

func TestKubeVirtPorts(t *testing.T) {
	ns := newNamespace(t, twoPortLeaves)

	startVM(t, ns, "vm1", "leaf1", "eth1", "eth2")
	startVM(t, ns, "vm2", "leaf2", "eth1", "eth2")
	waitReady(t, ns, "vm1")
	waitReady(t, ns, "vm2")

	ifaces := []string{"eth1", "eth2"}
	for i, iface := range ifaces {
		podIf := kubevirtIfName(topology + "-leaf1-" + iface)

		want := topo.MAC(topo.Endpoint{Namespace: ns, Topology: topology, Device: "leaf1", Interface: iface}).String()
		got := mustSh(t, ns, "vm1", "cat /sys/class/net/"+podIf+"/address")
		if got != want {
			t.Errorf("vm1: %s has MAC %s, want %s", podIf, got, want)
		}

		mustSh(t, ns, "vm1", fmt.Sprintf("ip addr add 192.168.%d.1/24 dev %s", i+1, podIf))
		mustSh(t, ns, "vm2", fmt.Sprintf("ip addr add 192.168.%d.2/24 dev %s", i+1, kubevirtIfName(topology+"-leaf2-"+iface)))
	}
	for i := range ifaces {
		waitPing(t, ns, "vm1", fmt.Sprintf("192.168.%d.2", i+1))
	}

	// KubeVirt's bridge binding renames the interface to <hash>-nic and puts a dummy under the requested name.
	renamed := make([]string, len(ifaces))
	for i, iface := range ifaces {
		podIf := kubevirtIfName(topology + "-leaf1-" + iface)
		renamed[i] = strings.TrimPrefix(podIf, "pod") + "-nic"

		mustSh(t, ns, "vm1", fmt.Sprintf(`set -e
ip link set %[1]s down
ip link set %[1]s name %[2]s
ip link set %[2]s up
ip link add %[1]s type dummy`, podIf, renamed[i]))
	}
	for i := range ifaces {
		waitPing(t, ns, "vm1", fmt.Sprintf("192.168.%d.2", i+1))
	}

	deletePod(t, ns, "vm2")
	for _, name := range renamed {
		waitCarrier(t, ns, "vm1", name, "0")
	}

	deletePod(t, ns, "vm1")
	eventually(t, 60*time.Second, "all DeviceAssociations and stubs to be gone", func() error {
		held := holders(t, ns)
		stubs := stubAliases(t, ns)
		if len(held) != 0 || len(stubs) != 0 {
			return fmt.Errorf("associations %v, stubs %v", held, stubs)
		}

		return nil
	})
}

// A port the device lacks fails its ADD without releasing the device.
func TestKubeVirtUnknownPort(t *testing.T) {
	ns := newNamespace(t, twoLeaves)

	startVM(t, ns, "vm1", "leaf1", "eth1", "eth9")
	waitEvent(t, ns, "vm1", "Failed to create pod sandbox")

	held := holders(t, ns)
	if len(held) != 1 || held["leaf1"] != "vm1" {
		t.Fatalf("device was released after the failed port: %v", held)
	}
}
