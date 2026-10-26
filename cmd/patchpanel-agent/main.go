//go:build linux

package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/squ94wk/patchpanel-cni/api/v1alpha1"
	"github.com/squ94wk/patchpanel-cni/internal/agent"
	"github.com/squ94wk/patchpanel-cni/internal/protocol"
)

const parkName = "patchpanel-park"

func main() {
	ctrl.SetLogger(zap.New())
	log := ctrl.Log.WithName("setup")

	err := run()
	if err != nil {
		log.Error(err, "agent failed")
		os.Exit(1)
	}
}

func run() error {
	node := os.Getenv("NODE_NAME")
	if node == "" {
		return errors.New("failed to find node name: NODE_NAME is not set")
	}

	// The park netns is pinned on the host (/run/netns is mounted Bidirectional), so it outlives the agent.
	// Creating it moves the calling thread into it, so that thread is never unlocked and exits with its goroutine.
	var park netns.NsHandle
	var parkErr error
	parkDone := make(chan struct{})
	go func() {
		defer close(parkDone)
		runtime.LockOSThread()

		park, parkErr = netns.GetFromName(parkName)
		if parkErr == nil {
			var st unix.Statfs_t
			if unix.Fstatfs(int(park), &st) == nil && st.Type == unix.NSFS_MAGIC {
				return
			}

			// A leftover regular file pins nothing.
			_ = park.Close()
			parkErr = os.Remove(filepath.Join("/run/netns", parkName))
			if parkErr != nil {
				return
			}
		}

		park, parkErr = netns.NewNamed(parkName)
		if parkErr != nil {
			return
		}

		for _, key := range []string{"all", "default"} {
			parkErr = os.WriteFile("/proc/sys/net/ipv6/conf/"+key+"/disable_ipv6", []byte("1"), 0o644)
			if parkErr != nil {
				return
			}
		}
	}()
	<-parkDone
	if parkErr != nil {
		return fmt.Errorf("failed to open park netns: %w", parkErr)
	}

	parkLinks, err := netlink.NewHandleAt(park)
	if err != nil {
		return fmt.Errorf("failed to open netlink in park netns: %w", err)
	}
	defer parkLinks.Close()

	err = v1alpha1.AddToScheme(scheme.Scheme)
	if err != nil {
		return fmt.Errorf("failed to register API types: %w", err)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:  scheme.Scheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
	})
	if err != nil {
		return fmt.Errorf("failed to create manager: %w", err)
	}

	a := agent.Agent{
		Client:    mgr.GetClient(),
		Reader:    mgr.GetAPIReader(),
		Node:      node,
		Park:      park,
		ParkLinks: parkLinks,
	}

	err = a.SetupWithManager(mgr)
	if err != nil {
		return fmt.Errorf("failed to set up DeviceAssociation controller: %w", err)
	}

	err = os.MkdirAll(filepath.Dir(protocol.SocketPath), 0o755)
	if err != nil {
		return fmt.Errorf("failed to create socket directory: %w", err)
	}
	err = os.Remove(protocol.SocketPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove stale socket: %w", err)
	}
	listener, err := net.Listen("unix", protocol.SocketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", protocol.SocketPath, err)
	}
	defer func() { _ = listener.Close() }()

	ctx := ctrl.SetupSignalHandler()

	go a.Watch(ctx)
	go func() {
		err := a.Serve(ctx, listener)
		if err != nil {
			ctrl.Log.Error(err, "socket server stopped")
		}
	}()

	err = mgr.Start(ctx)
	if err != nil {
		return fmt.Errorf("failed to run manager: %w", err)
	}

	return nil
}
