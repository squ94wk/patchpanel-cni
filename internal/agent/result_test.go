package agent_test

import (
	"encoding/json"
	"testing"

	"github.com/containernetworking/cni/pkg/types/create"

	"github.com/squ94wk/patchpanel-cni/internal/agent"
	"github.com/squ94wk/patchpanel-cni/internal/topo"
)

func TestBuildResult(t *testing.T) {
	const netns = "/var/run/netns/x"
	ports := []topo.Port{
		{Local: topo.Endpoint{Namespace: "ns", Topology: "lab", Device: "leaf1", Interface: "eth1"}},
		{Local: topo.Endpoint{Namespace: "ns", Topology: "lab", Device: "leaf1", Interface: "eth2"}},
	}

	for _, version := range []string{"1.0.0", "0.4.0", "0.3.1"} {
		t.Run(version, func(t *testing.T) {
			out, err := agent.BuildResult(version, netns, ports)
			if err != nil {
				t.Fatal(err)
			}

			res, err := create.Create(version, out)
			if err != nil {
				t.Fatalf("output does not parse at %s: %v", version, err)
			}
			if res.Version() != version {
				t.Errorf("got version %s", res.Version())
			}

			var parsed struct {
				Interfaces []struct {
					Name    string `json:"name"`
					Mac     string `json:"mac"`
					Sandbox string `json:"sandbox"`
				} `json:"interfaces"`
			}
			err = json.Unmarshal(out, &parsed)
			if err != nil {
				t.Fatal(err)
			}
			if len(parsed.Interfaces) != len(ports) {
				t.Fatalf("got %d interfaces, want %d", len(parsed.Interfaces), len(ports))
			}
			for n, i := range parsed.Interfaces {
				if i.Name != ports[n].Local.Interface {
					t.Errorf("interface %d: name %q, want %q", n, i.Name, ports[n].Local.Interface)
				}
				if i.Mac != topo.MAC(ports[n].Local).String() {
					t.Errorf("interface %q: mac %q, want %q", i.Name, i.Mac, topo.MAC(ports[n].Local))
				}
				if i.Sandbox != netns {
					t.Errorf("interface %q: sandbox %q, want %q", i.Name, i.Sandbox, netns)
				}
			}
		})
	}
}

func TestBuildResultUnsupportedVersion(t *testing.T) {
	_, err := agent.BuildResult("9.9.9", "/x", nil)
	if err == nil {
		t.Fatal("want error for unsupported version")
	}
}
