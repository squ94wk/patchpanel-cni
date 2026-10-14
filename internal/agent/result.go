package agent

import (
	"bytes"
	"fmt"

	types100 "github.com/containernetworking/cni/pkg/types/100"

	"github.com/squ94wk/dc-net-cni/internal/topo"
)

// BuildResult returns the CNI result in cniVersion. Every interface has the sandbox set, otherwise multus drops it.
func BuildResult(cniVersion, netns string, ports []topo.Port) ([]byte, error) {
	result := types100.Result{CNIVersion: types100.ImplementedSpecVersion}
	for _, p := range ports {
		result.Interfaces = append(result.Interfaces, &types100.Interface{
			Name:    p.Local.Interface,
			Mac:     topo.MAC(p.Local).String(),
			Sandbox: netns,
		})
	}

	converted, err := result.GetAsVersion(cniVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to convert result to CNI version %q: %w", cniVersion, err)
	}

	var out bytes.Buffer
	err = converted.PrintTo(&out)
	if err != nil {
		return nil, fmt.Errorf("failed to encode result: %w", err)
	}

	return out.Bytes(), nil
}
