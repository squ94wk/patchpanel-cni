//go:build linux

package agent

import (
	"errors"
	"fmt"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"

	"github.com/squ94wk/dc-net-cni/internal/topo"
)

// createPort creates the veth pair of a port: the stub stays in the park netns and
// the other end lands in the pod netns under its topology name. Both ends are created down,
// the pod side is brought up so that the pod sees carrier as soon as the port is wired.
func createPort(park *netlink.Handle, podNs netns.NsHandle, pod *netlink.Handle, p topo.Port) error {
	stub := topo.StubName(p.Local)

	var veth netlink.Veth
	veth.Name = stub
	veth.MTU = MTU
	veth.TxQLen = -1
	veth.PeerName = p.Local.Interface
	veth.PeerMTU = MTU
	veth.PeerTxQLen = -1
	veth.PeerHardwareAddr = topo.MAC(p.Local)
	veth.PeerNamespace = netlink.NsFd(int(podNs))

	err := park.LinkAdd(&veth)
	if err != nil {
		return fmt.Errorf("failed to create interface %q in pod: %w", p.Local.Interface, err)
	}

	stubLink, err := park.LinkByName(stub)
	if err != nil {
		return fmt.Errorf("failed to look up stub for interface %q: %w", p.Local.Interface, err)
	}

	err = park.LinkSetAlias(stubLink, topo.Alias(p.Local))
	if err != nil {
		return fmt.Errorf("failed to set alias on stub for interface %q: %w", p.Local.Interface, err)
	}

	podLink, err := pod.LinkByName(p.Local.Interface)
	if err != nil {
		return fmt.Errorf("failed to look up interface %q in pod: %w", p.Local.Interface, err)
	}

	err = pod.LinkSetUp(podLink)
	if err != nil {
		return fmt.Errorf("failed to set interface %q up in pod: %w", p.Local.Interface, err)
	}

	return nil
}

// wire patches two stubs together: every frame arriving on one is redirected out of the other.
// Redirecting instead of bridging keeps LLDP, LACP and STP frames intact.
func wire(park *netlink.Handle, a, b netlink.Link) error {
	for _, pair := range [][2]netlink.Link{{a, b}, {b, a}} {
		from, to := pair[0], pair[1]

		var qdisc netlink.Clsact
		qdisc.LinkIndex = from.Attrs().Index
		qdisc.Handle = netlink.MakeHandle(0xffff, 0)
		qdisc.Parent = netlink.HANDLE_CLSACT

		err := park.QdiscReplace(&qdisc)
		if err != nil {
			return fmt.Errorf("failed to add clsact qdisc to %s: %w", from.Attrs().Name, err)
		}

		// Wiring must not depend on the kernel's handling of an existing matchall filter: it can be left over
		// from an old peer on a down stub or from the other direction of a self-link.
		existing, err := park.FilterList(from, netlink.HANDLE_MIN_INGRESS)
		if err != nil {
			return fmt.Errorf("failed to list filters of %s: %w", from.Attrs().Name, err)
		}
		for _, f := range existing {
			err = park.FilterDel(f)
			if err != nil {
				return fmt.Errorf("failed to remove old redirect of %s: %w", from.Attrs().Name, err)
			}
		}

		var redirect netlink.MirredAction
		redirect.Action = netlink.TC_ACT_STOLEN
		redirect.MirredAction = netlink.TCA_EGRESS_REDIR
		redirect.Ifindex = to.Attrs().Index

		var filter netlink.MatchAll
		filter.LinkIndex = from.Attrs().Index
		filter.Parent = netlink.HANDLE_MIN_INGRESS
		filter.Priority = 1
		filter.Handle = 1
		filter.Protocol = unix.ETH_P_ALL
		filter.Actions = []netlink.Action{&redirect}

		err = park.FilterAdd(&filter)
		if err != nil {
			return fmt.Errorf("failed to redirect %s to %s: %w", from.Attrs().Name, to.Attrs().Name, err)
		}
	}

	for _, l := range []netlink.Link{a, b} {
		err := park.LinkSetUp(l)
		if err != nil {
			return fmt.Errorf("failed to set %s up: %w", l.Attrs().Name, err)
		}
	}

	return nil
}

// deleteStub deletes a stub by name, which deletes its pod side as well. A missing stub is fine.
func deleteStub(park *netlink.Handle, name string) error {
	link, err := park.LinkByName(name)
	var notFound netlink.LinkNotFoundError
	if errors.As(err, &notFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to look up stub: %w", err)
	}

	err = park.LinkDel(link)
	if err != nil {
		return fmt.Errorf("failed to delete stub: %w", err)
	}

	return nil
}

// cleanupDevice deletes all stubs of the device and downs the stubs that were cabled to them.
func cleanupDevice(park *netlink.Handle, device topo.Endpoint) error {
	links, err := park.LinkList()
	if err != nil {
		return fmt.Errorf("failed to list stubs: %w", err)
	}

	var errs []error
	for _, l := range links {
		e, err := topo.ParseAlias(l.Attrs().Alias)
		if err != nil {
			continue
		}
		if e.Namespace != device.Namespace || e.Topology != device.Topology || e.Device != device.Device {
			continue
		}

		err = park.LinkDel(l)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to delete stub %s: %w", l.Attrs().Name, err))
		}
	}

	errs = append(errs, reconcileDangling(park))

	return errors.Join(errs...)
}

// reconcileDangling downs and unwires every up stub whose redirect target is gone or does not redirect back,
// which is how a deleted peer shows up as unplugged cable.
func reconcileDangling(park *netlink.Handle) error {
	links, err := park.LinkList()
	if err != nil {
		return fmt.Errorf("failed to list stubs: %w", err)
	}

	var stubs []StubTarget
	byIndex := make(map[int]netlink.Link)
	filters := make(map[int]netlink.Filter)
	for _, l := range links {
		_, err := topo.ParseAlias(l.Attrs().Alias)
		if err != nil {
			continue
		}

		stub := StubTarget{Index: l.Attrs().Index, Up: l.Attrs().Flags&unix.IFF_UP != 0}

		fs, err := park.FilterList(l, netlink.HANDLE_MIN_INGRESS)
		if err != nil {
			return fmt.Errorf("failed to list filters of %s: %w", l.Attrs().Name, err)
		}
		for _, f := range fs {
			matchAll, ok := f.(*netlink.MatchAll)
			if !ok {
				continue
			}
			for _, act := range matchAll.Actions {
				mirred, ok := act.(*netlink.MirredAction)
				if !ok {
					continue
				}
				stub.Target = mirred.Ifindex
				filters[stub.Index] = f
			}
		}

		stubs = append(stubs, stub)
		byIndex[stub.Index] = l
	}

	var errs []error
	for _, idx := range BrokenStubs(stubs) {
		l := byIndex[idx]

		err := park.LinkSetDown(l)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to set %s down: %w", l.Attrs().Name, err))
		}

		f, ok := filters[idx]
		if !ok {
			continue
		}
		err = park.FilterDel(f)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to remove redirect of %s: %w", l.Attrs().Name, err))
		}
	}

	return errors.Join(errs...)
}
