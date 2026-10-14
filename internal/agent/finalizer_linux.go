//go:build linux

package agent

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/squ94wk/dc-net-cni/api/v1alpha1"
	"github.com/squ94wk/dc-net-cni/internal/topo"
)

func (a *Agent) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&v1alpha1.DeviceAssociation{}).Complete(a)
}

// Reconcile cleans up the node state of a deleting DeviceAssociation of this node and removes the finalizer.
// An empty status.nodeName means no node state exists, so any agent may drop the finalizer.
func (a *Agent) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var cached v1alpha1.DeviceAssociation
	err := a.Client.Get(ctx, req.NamespacedName, &cached)
	if apierrors.IsNotFound(err) {
		return ctrl.Result{}, nil
	}
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get DeviceAssociation: %w", err)
	}
	if cached.DeletionTimestamp.IsZero() || !controllerutil.ContainsFinalizer(&cached, Finalizer) {
		return ctrl.Result{}, nil
	}
	if cached.Status.NodeName != "" && cached.Status.NodeName != a.Node {
		return ctrl.Result{}, nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// The cache may be stale: an ADD can have claimed the device in between, so decide on fresh state.
	var da v1alpha1.DeviceAssociation
	err = a.Reader.Get(ctx, req.NamespacedName, &da)
	if apierrors.IsNotFound(err) {
		return ctrl.Result{}, nil
	}
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get DeviceAssociation from the API server: %w", err)
	}
	if da.UID != cached.UID || da.DeletionTimestamp.IsZero() || !controllerutil.ContainsFinalizer(&da, Finalizer) {
		return ctrl.Result{}, nil
	}
	if da.Status.NodeName != "" && da.Status.NodeName != a.Node {
		return ctrl.Result{}, nil
	}

	if da.Status.NodeName != "" {
		err = cleanupDevice(a.ParkLinks, topo.Endpoint{Namespace: da.Namespace, Topology: da.Spec.Topology, Device: da.Spec.Device})
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to clean up device %q: %w", da.Spec.Device, err)
		}
	}

	patch := client.MergeFromWithOptions(da.DeepCopy(), client.MergeFromWithOptimisticLock{})
	controllerutil.RemoveFinalizer(&da, Finalizer)
	err = a.Client.Patch(ctx, &da, patch)
	if client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, fmt.Errorf("failed to remove finalizer: %w", err)
	}

	return ctrl.Result{}, nil
}
