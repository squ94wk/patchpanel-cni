//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/squ94wk/dc-net-cni/internal/topo"
)

const twoLeaves = `
  devices:
    - {name: leaf1}
    - {name: leaf2}
  links:
    - {a: {device: leaf1, interface: eth1}, b: {device: leaf2, interface: eth1}}
`

func setIP(t *testing.T, ns, pod, cidr string) {
	t.Helper()

	mustSh(t, ns, pod, "ip addr add "+cidr+" dev eth1")
}

func TestLinkedPods(t *testing.T) {
	ns := newNamespace(t, twoLeaves)

	startPod(t, ns, "leaf1", `{"device":"leaf1"}`)
	startPod(t, ns, "leaf2", `{"device":"leaf2"}`)
	waitReady(t, ns, "leaf1")
	waitReady(t, ns, "leaf2")

	for _, device := range []string{"leaf1", "leaf2"} {
		want := topo.MAC(topo.Endpoint{Namespace: ns, Topology: topology, Device: device, Interface: "eth1"}).String()

		got := mustSh(t, ns, device, "cat /sys/class/net/eth1/address")
		if got != want {
			t.Errorf("%s: eth1 has MAC %s, want %s", device, got, want)
		}

		got = mustSh(t, ns, device, "cat /sys/class/net/eth1/carrier")
		if got != "1" {
			t.Errorf("%s: eth1 carrier is %s, want 1", device, got)
		}
	}

	status := kubectl(t, "-n", ns, "get", "pod", "leaf1", "-o", `jsonpath={.metadata.annotations.k8s\.v1\.cni\.cncf\.io/network-status}`)
	if !strings.Contains(status, `"interface": "eth1"`) {
		t.Errorf("network-status of leaf1 lacks eth1: %s", status)
	}

	setIP(t, ns, "leaf1", "192.168.0.1/24")
	setIP(t, ns, "leaf2", "192.168.0.2/24")
	waitPing(t, ns, "leaf1", "192.168.0.2")
	waitPing(t, ns, "leaf2", "192.168.0.1")
}

func TestPeerRecreated(t *testing.T) {
	ns := newNamespace(t, twoLeaves)

	startPod(t, ns, "leaf1", `{"device":"leaf1"}`)
	startPod(t, ns, "leaf2", `{"device":"leaf2"}`)
	waitReady(t, ns, "leaf1")
	waitReady(t, ns, "leaf2")
	setIP(t, ns, "leaf1", "192.168.0.1/24")
	setIP(t, ns, "leaf2", "192.168.0.2/24")
	waitPing(t, ns, "leaf1", "192.168.0.2")

	for round := 1; round <= 2; round++ {
		deletePod(t, ns, "leaf2")
		waitCarrier(t, ns, "leaf1", "eth1", "0")

		startPod(t, ns, "leaf2", `{"device":"leaf2"}`)
		waitReady(t, ns, "leaf2")
		setIP(t, ns, "leaf2", "192.168.0.2/24")
		waitCarrier(t, ns, "leaf1", "eth1", "1")
		waitPing(t, ns, "leaf1", "192.168.0.2")
		waitPing(t, ns, "leaf2", "192.168.0.1")
	}
}

func TestLLDPFrameCrossesLink(t *testing.T) {
	ns := newNamespace(t, twoLeaves)

	startPod(t, ns, "leaf1", `{"device":"leaf1"}`)
	startPod(t, ns, "leaf2", `{"device":"leaf2"}`)
	waitReady(t, ns, "leaf1")
	waitReady(t, ns, "leaf2")
	waitCarrier(t, ns, "leaf1", "eth1", "1")

	captured := make(chan string)
	go func() {
		out, err := kubectlTry("", "-n", ns, "exec", "leaf2", "--", "timeout", "30",
			"tcpdump", "-nn", "-e", "-c", "1", "-i", "eth1", "ether", "proto", "0x88cc")
		if err != nil {
			out = "capture failed: " + err.Error()
		}
		captured <- out
	}()

	// LLDP frame: nearest-bridge multicast destination, chassis ID, port ID, TTL and end TLVs, padded to the minimum frame size.
	const send = `python3 -c "
import socket
mac = open('/sys/class/net/eth1/address').read().strip().replace(':', '')
frame = bytes.fromhex('0180c200000e' + mac + '88cc' + '020704' + mac + '040307' + '6531' + '06020078' + '0000')
s = socket.socket(socket.AF_PACKET, socket.SOCK_RAW)
s.bind(('eth1', 0))
s.send(frame.ljust(60, b'\0'))
"`

	for {
		select {
		case out := <-captured:
			if !strings.Contains(out, "01:80:c2:00:00:0e") || !strings.Contains(out, "0x88cc") {
				t.Fatalf("leaf2 did not capture the LLDP frame: %s", out)
			}

			return
		case <-time.After(time.Second):
			mustSh(t, ns, "leaf1", send)
		}
	}
}

func TestSelfLink(t *testing.T) {
	ns := newNamespace(t, `
  devices:
    - {name: dut}
  links:
    - {a: {device: dut, interface: eth1}, b: {device: dut, interface: eth2}}
`)

	startPod(t, ns, "dut", `{"device":"dut"}`)
	waitReady(t, ns, "dut")
	for _, iface := range []string{"eth1", "eth2"} {
		waitCarrier(t, ns, "dut", iface, "1")
	}

	// Both ends are in the same pod: eth2 moves into its own netns so that traffic has to cross the link.
	mustSh(t, ns, "dut", `set -e
ip netns add far
ip link set eth2 netns far
ip -n far addr add 192.168.0.2/24 dev eth2
ip -n far link set eth2 up
ip addr add 192.168.0.1/24 dev eth1`)

	eventually(t, 60*time.Second, "ping between both ends of the self-link", func() error {
		_, err := sh(ns, "dut", "ping -c 1 -W 1 -I eth1 192.168.0.2")
		return err
	})
}
