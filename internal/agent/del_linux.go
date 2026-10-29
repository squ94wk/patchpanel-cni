//go:build linux

package agent

import (
	"context"
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/squ94wk/patchpanel-cni/api/v1alpha1"
	"github.com/squ94wk/patchpanel-cni/internal/topo"
)

// Del removes the ports of the pod's device, or only the selected port, if the sandbox is the one that created them.
// Cleanup failures are logged and not returned: the DeviceAssociation finalizer cleans up when the pod is garbage collected.
func (a *Agent) Del(ctx context.Context, inv Invocation) error {
	if inv.Topology == "" || inv.PodUID == "" || inv.Namespace == "" {
		return nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	var list v1alpha1.DeviceAssociationList
	err := a.Reader.List(ctx, &list, client.InNamespace(inv.Namespace), client.MatchingLabels{PodUIDLabel: inv.PodUID})
	if err != nil {
		return fmt.Errorf("failed to list DeviceAssociations of the pod: %w", err)
	}

	for _, da := range list.Items {
		if da.Spec.Topology != inv.Topology || da.Status.SandboxID != inv.ContainerID {
			continue
		}

		if inv.Interface != "" {
			// The selected port is identified by the config, not by CNI_IFNAME: the pod side may have been renamed in the netns.
			if inv.Device != da.Spec.Device {
				continue
			}

			endpoint := topo.Endpoint{Namespace: inv.Namespace, Topology: inv.Topology, Device: da.Spec.Device, Interface: inv.Interface}
			err := deleteStub(a.ParkLinks, topo.StubName(endpoint))
			if err != nil {
				ctrl.Log.Error(err, "failed to delete stub", "device", da.Spec.Device, "interface", inv.Interface)
			}

			err = reconcileDangling(a.ParkLinks)
			if err != nil {
				ctrl.Log.Error(err, "failed to unwire dangling stubs", "device", da.Spec.Device)
			}

			continue
		}

		err := cleanupDevice(a.ParkLinks, topo.Endpoint{Namespace: inv.Namespace, Topology: inv.Topology, Device: da.Spec.Device})
		if err != nil {
			ctrl.Log.Error(err, "failed to clean up device", "device", da.Spec.Device)
		}
	}

	return nil
}
