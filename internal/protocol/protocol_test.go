package protocol_test

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/containernetworking/cni/pkg/types"

	"github.com/squ94wk/dc-net-cni/internal/protocol"
)

func serve(t *testing.T, handler func(protocol.Request) protocol.Response) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "dc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	socket := filepath.Join(dir, "s")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })

	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		_ = protocol.Handle(conn, handler)
	}()

	return socket
}

func TestCallHandleRoundTrip(t *testing.T) {
	req := protocol.Request{
		Command: "ADD", ContainerID: "abc", Netns: "/n", IfName: "eth0", Args: "K=v", Path: "/opt/cni/bin",
		Stdin: []byte(`{"topology":"lab"}`),
	}

	var got protocol.Request
	socket := serve(t, func(r protocol.Request) protocol.Response {
		got = r
		return protocol.Response{Result: []byte(`{"cniVersion":"1.0.0"}`)}
	})

	resp, err := protocol.Call(socket, req)
	if err != nil {
		t.Fatal(err)
	}

	if got.Command != req.Command || got.ContainerID != req.ContainerID || got.Netns != req.Netns ||
		got.IfName != req.IfName || got.Args != req.Args || got.Path != req.Path || !bytes.Equal(got.Stdin, req.Stdin) {
		t.Errorf("handler got %+v, want %+v", got, req)
	}
	if string(resp.Result) != `{"cniVersion":"1.0.0"}` || resp.Error != nil {
		t.Errorf("got response %+v", resp)
	}
}

func TestCallErrorPassthrough(t *testing.T) {
	socket := serve(t, func(protocol.Request) protocol.Response {
		return protocol.Response{Error: &types.Error{Code: 11, Msg: "no free device", Details: "group servers"}}
	})

	resp, err := protocol.Call(socket, protocol.Request{Command: "ADD"})
	if err != nil {
		t.Fatal(err)
	}

	if resp.Error == nil || resp.Error.Code != 11 || resp.Error.Msg != "no free device" || resp.Error.Details != "group servers" {
		t.Fatalf("got %+v", resp.Error)
	}
	if len(resp.Result) != 0 {
		t.Errorf("unexpected result %s", resp.Result)
	}
}

func TestCallNoListener(t *testing.T) {
	_, err := protocol.Call(filepath.Join(t.TempDir(), "x"), protocol.Request{})
	if err == nil {
		t.Fatal("want error when no agent is listening")
	}
}
