//go:build linux

package agent

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	ctrl "sigs.k8s.io/controller-runtime"
)

// Watch downs the stubs whose peer disappeared, until ctx is done.
// It resubscribes after subscription errors and reconciles after each (re)subscription to not miss events.
func (a *Agent) Watch(ctx context.Context) {
	log := ctrl.Log.WithName("watch")

	for ctx.Err() == nil {
		updates := make(chan netlink.LinkUpdate)
		done := make(chan struct{})
		failed := make(chan error, 1)

		err := netlink.LinkSubscribeWithOptions(updates, done, netlink.LinkSubscribeOptions{
			Namespace: &a.Park,
			ErrorCallback: func(err error) {
				select {
				case failed <- err:
				default:
				}
			},
		})
		if err != nil {
			log.Error(err, "failed to subscribe to link updates")
			sleep(ctx, time.Second)
			continue
		}

		a.reconcile(log)

	events:
		for {
			select {
			case <-ctx.Done():
				break events
			case err := <-failed:
				log.Error(err, "link subscription failed")
				sleep(ctx, time.Second)
				break events
			case u, ok := <-updates:
				if !ok {
					break events
				}
				if u.Header.Type == unix.RTM_DELLINK {
					a.reconcile(log)
				}
			}
		}

		close(done)

		// The sender goroutine blocks on the unbuffered channel until it observes done: drain until it closes the channel.
		go func() {
			for range updates {
			}
		}()
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// reconcile takes the lock itself.
func (a *Agent) reconcile(log logr.Logger) {
	a.mu.Lock()
	defer a.mu.Unlock()

	err := reconcileDangling(a.ParkLinks)
	if err != nil {
		log.Error(err, "failed to unwire dangling stubs")
	}
}
