package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/squ94wk/patchpanel-cni/internal/protocol"
)

type Invocation struct {
	Command     string
	ContainerID string
	Netns       string
	CNIVersion  string
	Topology    string
	Device      string
	Group       string
	Namespace   string
	PodName     string
	PodUID      string
}

type netConf struct {
	CNIVersion string `json:"cniVersion"`
	Topology   string `json:"topology"`
	Args       struct {
		CNI struct {
			Device string `json:"device"`
			Group  string `json:"group"`
		} `json:"cni"`
	} `json:"args"`
}

// ParseInvocation reads the topology and the device or group from the stdin config and the pod identity from CNI_ARGS.
// DEL only needs the topology and the pod UID and succeeds without them: there is nothing to clean up.
func ParseInvocation(r protocol.Request) (Invocation, error) {
	var conf netConf
	err := json.Unmarshal(r.Stdin, &conf)
	if err != nil {
		return Invocation{}, fmt.Errorf("failed to parse network config: %w", err)
	}

	inv := Invocation{
		Command:     r.Command,
		ContainerID: r.ContainerID,
		Netns:       r.Netns,
		CNIVersion:  conf.CNIVersion,
		Topology:    conf.Topology,
		Device:      conf.Args.CNI.Device,
		Group:       conf.Args.CNI.Group,
	}

	for kv := range strings.SplitSeq(r.Args, ";") {
		k, v, _ := strings.Cut(kv, "=")
		switch k {
		case "K8S_POD_NAMESPACE":
			inv.Namespace = v
		case "K8S_POD_NAME":
			inv.PodName = v
		case "K8S_POD_UID":
			inv.PodUID = v
		}
	}

	if inv.Command != "ADD" {
		return inv, nil
	}

	if inv.Topology == "" {
		return Invocation{}, errors.New("failed to find \"topology\" in network config")
	}

	if inv.PodUID == "" || inv.Namespace == "" || inv.PodName == "" {
		return Invocation{}, errors.New("failed to find K8S_POD_NAMESPACE, K8S_POD_NAME and K8S_POD_UID in CNI_ARGS")
	}

	if (inv.Device == "") == (inv.Group == "") {
		return Invocation{}, errors.New("failed to select a device: set exactly one of \"device\" and \"group\" in cni-args")
	}

	return inv, nil
}
