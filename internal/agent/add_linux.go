//go:build linux

package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"

	types100 "github.com/containernetworking/cni/pkg/types/100"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/squ94wk/patchpanel-cni/api/v1alpha1"
	"github.com/squ94wk/patchpanel-cni/internal/topo"
)

// Add claims a device of the topology for the pod, creates its ports (or only the selected port) and wires them to the existing peers.
// A failed Add removes the ports it created and a repeated Add recreates the ports it creates.
func (a *Agent) Add(ctx context.Context, inv Invocation) (result []byte, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	var topology v1alpha1.Topology
	err = a.Reader.Get(ctx, client.ObjectKey{Namespace: inv.Namespace, Name: inv.Topology}, &topology)
	if err != nil {
		return nil, fmt.Errorf("failed to get Topology %s/%s: %w", inv.Namespace, inv.Topology, err)
	}

	// A pod holds at most one device per topology: a held device is the only candidate.
	var held v1alpha1.DeviceAssociationList
	err = a.Reader.List(ctx, &held, client.InNamespace(inv.Namespace), client.MatchingLabels{PodUIDLabel: inv.PodUID})
	if err != nil {
		return nil, fmt.Errorf("failed to list DeviceAssociations of the pod: %w", err)
	}

	candidates := []string{inv.Device}
	if inv.Device == "" {
		candidates = topo.GroupMembers(topology.Spec, inv.Group)
	}
	for _, h := range held.Items {
		if h.Spec.Topology != inv.Topology {
			continue
		}
		if inv.Device != "" && inv.Device != h.Spec.Device {
			return nil, fmt.Errorf("failed to claim device %q: pod already holds device %q of Topology %s", inv.Device, h.Spec.Device, inv.Topology)
		}
		if inv.Device == "" && !slices.Contains(candidates, h.Spec.Device) {
			return nil, fmt.Errorf("failed to claim a device of group %q: pod already holds device %q of Topology %s, which is not a member", inv.Group, h.Spec.Device, inv.Topology)
		}
		candidates = []string{h.Spec.Device}
	}

	// Claim: the association name is deterministic, so Create is an atomic claim.
	var da v1alpha1.DeviceAssociation
	var created bool
	var claimed bool
	for _, device := range candidates {
		da = v1alpha1.DeviceAssociation{
			ObjectMeta: metav1.ObjectMeta{
				Name:       topo.AssociationName(inv.Namespace, inv.Topology, device),
				Namespace:  inv.Namespace,
				Labels:     map[string]string{PodUIDLabel: inv.PodUID},
				Finalizers: []string{Finalizer},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "v1",
					Kind:       "Pod",
					Name:       inv.PodName,
					UID:        types.UID(inv.PodUID),
				}},
			},
			Spec: v1alpha1.DeviceAssociationSpec{Topology: inv.Topology, Device: device, PodUID: inv.PodUID},
		}

		err = a.Client.Create(ctx, &da)
		if err == nil {
			created = true
			claimed = true
			break
		}
		if !apierrors.IsAlreadyExists(err) {
			return nil, fmt.Errorf("failed to create DeviceAssociation for device %q: %w", device, err)
		}

		err = a.Reader.Get(ctx, client.ObjectKeyFromObject(&da), &da)
		if err != nil {
			return nil, fmt.Errorf("failed to get existing DeviceAssociation for device %q: %w", device, err)
		}
		if da.Spec.PodUID != inv.PodUID {
			continue
		}
		if !da.DeletionTimestamp.IsZero() {
			return nil, fmt.Errorf("failed to claim device %q: its DeviceAssociation is being deleted", device)
		}

		claimed = true
		break
	}
	if !claimed {
		if len(candidates) == 0 {
			return nil, fmt.Errorf("failed to claim a device: group %q has no members in Topology %s", inv.Group, inv.Topology)
		}
		return nil, errors.New("failed to claim a free device: all candidates are held by other pods")
	}

	// Rollback on failure removes only the stubs this Add created: the sandbox may hold ports of earlier Adds.
	// Stubs are removed by name, because a stub that failed before its alias was set cannot be found by alias.
	var ports []topo.Port
	var createdStubs []string
	defer func() {
		if err == nil {
			return
		}

		var rollbackErrs []error
		for _, stub := range createdStubs {
			delErr := deleteStub(a.ParkLinks, stub)
			if delErr != nil {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("failed to roll back stub %s: %w", stub, delErr))
			}
		}
		delErr := reconcileDangling(a.ParkLinks)
		if delErr != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("failed to unwire dangling stubs during rollback: %w", delErr))
		}

		if created {
			delErr := a.Client.Delete(ctx, &da)
			if delErr != nil {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("failed to roll back DeviceAssociation %s: %w", da.Name, delErr))
			}
		}

		err = errors.Join(append([]error{err}, rollbackErrs...)...)
	}()

	ports, err = topo.Ports(inv.Namespace, inv.Topology, topology.Spec, da.Spec.Device)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve ports: %w", err)
	}

	// Status first: an empty nodeName means no node state exists, so any agent may drop the finalizer.
	da.Status.NodeName = a.Node
	da.Status.SandboxID = inv.ContainerID
	err = a.Client.Status().Update(ctx, &da)
	if err != nil {
		return nil, fmt.Errorf("failed to record node and sandbox in DeviceAssociation status: %w", err)
	}

	// Port mode: one Add creates only the selected port, the sandbox's other ports belong to other Adds.
	if inv.Interface != "" {
		i := slices.IndexFunc(ports, func(p topo.Port) bool { return p.Local.Interface == inv.Interface })
		if i < 0 {
			return nil, fmt.Errorf("failed to find interface %q on device %q in Topology %s/%s", inv.Interface, da.Spec.Device, inv.Namespace, inv.Topology)
		}
		ports = []topo.Port{ports[i]}
	}

	podNs, err := netns.GetFromPath(inv.Netns)
	if err != nil {
		return nil, fmt.Errorf("failed to open pod netns %s: %w", inv.Netns, err)
	}
	defer func() { _ = podNs.Close() }()

	pod, err := netlink.NewHandleAt(podNs)
	if err != nil {
		return nil, fmt.Errorf("failed to open netlink in pod netns: %w", err)
	}
	defer pod.Close()

	var ifaces []*types100.Interface
	for _, p := range ports {
		// Device mode names the pod side after the topology interface, port mode after CNI_IFNAME.
		podIfName := p.Local.Interface
		if inv.Interface != "" {
			podIfName = inv.IfName
		}

		// Remove a stub left by an earlier Add of this port, e.g. one whose sandbox was lost without a DEL.
		stub := topo.StubName(p.Local)
		err = deleteStub(a.ParkLinks, stub)
		if err != nil {
			return nil, fmt.Errorf("failed to remove previous stub of interface %q: %w", p.Local.Interface, err)
		}

		createdStubs = append(createdStubs, stub)
		err = createPort(a.ParkLinks, podNs, pod, p, podIfName)
		if err != nil {
			return nil, err
		}

		ifaces = append(ifaces, &types100.Interface{Name: podIfName, Mac: topo.MAC(p.Local).String(), Sandbox: inv.Netns})
	}

	for _, p := range ports {
		peer, lookupErr := a.ParkLinks.LinkByName(topo.StubName(p.Peer))
		var notFound netlink.LinkNotFoundError
		if errors.As(lookupErr, &notFound) {
			continue
		}
		if lookupErr != nil {
			return nil, fmt.Errorf("failed to look up peer stub of interface %q: %w", p.Local.Interface, lookupErr)
		}
		if peer.Attrs().Alias != topo.Alias(p.Peer) {
			continue
		}

		local, lookupErr := a.ParkLinks.LinkByName(topo.StubName(p.Local))
		if lookupErr != nil {
			return nil, fmt.Errorf("failed to look up stub of interface %q: %w", p.Local.Interface, lookupErr)
		}

		err = wire(a.ParkLinks, local, peer)
		if err != nil {
			return nil, fmt.Errorf("failed to wire interface %q: %w", p.Local.Interface, err)
		}
	}

	// A peer stub whose link moved to another device after a topology edit no longer points back: down it.
	err = reconcileDangling(a.ParkLinks)
	if err != nil {
		return nil, fmt.Errorf("failed to unwire dangling stubs: %w", err)
	}

	return BuildResult(inv.CNIVersion, ifaces)
}
