//go:build linux

package agent

import (
	"context"
	"errors"
	"net"
	"sync"

	"github.com/containernetworking/cni/pkg/types"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/squ94wk/patchpanel-cni/internal/protocol"
)

const (
	PodUIDLabel = "dcnet.squ94wk.github.io/pod-uid"
	Finalizer   = "dcnet.squ94wk.github.io/cleanup"
	MTU         = 9000
)

// Agent holds no state about devices: the stubs in the park netns are the only node-local truth.
// mu serializes ADD, DEL, the finalizer reconciler and the netlink watch.
type Agent struct {
	Client client.Client
	Reader client.Reader
	Node   string
	Park   netns.NsHandle
	// ParkLinks is a netlink handle in the park netns.
	ParkLinks *netlink.Handle

	mu sync.Mutex
}

// Serve answers one request per connection until l is closed.
func (a *Agent) Serve(ctx context.Context, l net.Listener) error {
	log := ctrl.Log.WithName("socket")

	for {
		conn, err := l.Accept()
		if errors.Is(err, net.ErrClosed) {
			return nil
		}
		if err != nil {
			log.Error(err, "failed to accept connection")
			continue
		}

		go func() {
			err := protocol.Handle(conn, func(req protocol.Request) protocol.Response {
				return a.handle(ctx, req)
			})
			if err != nil {
				log.Error(err, "failed to serve request")
			}
		}()
	}
}

func (a *Agent) handle(ctx context.Context, req protocol.Request) protocol.Response {
	log := ctrl.Log.WithName("cni").WithValues("command", req.Command, "containerID", req.ContainerID)

	inv, err := ParseInvocation(req)
	if err != nil {
		log.Error(err, "rejected invocation")
		return protocol.Response{Error: types.NewError(types.ErrInvalidNetworkConfig, "invalid invocation", err.Error())}
	}

	switch req.Command {
	case "ADD":
		result, err := a.Add(ctx, inv)
		if err != nil {
			log.Error(err, "ADD failed", "pod", inv.Namespace+"/"+inv.PodName)
			return protocol.Response{Error: types.NewError(types.ErrInternal, "ADD failed", err.Error())}
		}

		log.Info("ADD done", "pod", inv.Namespace+"/"+inv.PodName)
		return protocol.Response{Result: result}
	case "DEL":
		err := a.Del(ctx, inv)
		if err != nil {
			log.Error(err, "DEL failed", "pod", inv.Namespace+"/"+inv.PodName)
			return protocol.Response{Error: types.NewError(types.ErrInternal, "DEL failed", err.Error())}
		}

		log.Info("DEL done", "pod", inv.Namespace+"/"+inv.PodName)
		return protocol.Response{}
	default:
		return protocol.Response{Error: types.NewError(types.ErrInvalidEnvironmentVariables, "unsupported command", req.Command)}
	}
}
