package agent

import (
	"bytes"
	"fmt"

	types100 "github.com/containernetworking/cni/pkg/types/100"
)

// BuildResult returns the CNI result in cniVersion for the interfaces an Add created.
// Every interface must have the sandbox set, otherwise multus drops it.
func BuildResult(cniVersion string, interfaces []*types100.Interface) ([]byte, error) {
	result := types100.Result{CNIVersion: types100.ImplementedSpecVersion, Interfaces: interfaces}

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
