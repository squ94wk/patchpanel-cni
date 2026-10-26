package protocol

import (
	"encoding/json"
	"fmt"
	"net"

	"github.com/containernetworking/cni/pkg/types"
)

const SocketPath = "/run/patchpanel/patchpanel.sock"

// Request is a raw CNI invocation: the environment variables plus the stdin config.
type Request struct {
	Command     string `json:"command"`
	ContainerID string `json:"containerID"`
	Netns       string `json:"netns"`
	IfName      string `json:"ifName"`
	Args        string `json:"args"`
	Path        string `json:"path"`
	Stdin       []byte `json:"stdin"`
}

// Response carries either the CNI result or a CNI error.
type Response struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  *types.Error    `json:"error,omitempty"`
}

// Call sends one request over a new connection to the unix socket and reads one response.
func Call(socket string, req Request) (Response, error) {
	var resp Response

	conn, err := net.Dial("unix", socket)
	if err != nil {
		return resp, fmt.Errorf("failed to connect to agent: %w", err)
	}
	defer func() { _ = conn.Close() }()

	err = json.NewEncoder(conn).Encode(req)
	if err != nil {
		return resp, fmt.Errorf("failed to send request to agent: %w", err)
	}

	err = json.NewDecoder(conn).Decode(&resp)
	if err != nil {
		return resp, fmt.Errorf("failed to read response from agent: %w", err)
	}

	return resp, nil
}

// Handle reads one request from conn, answers it with handler and closes conn.
func Handle(conn net.Conn, handler func(Request) Response) error {
	defer func() { _ = conn.Close() }()

	var req Request
	err := json.NewDecoder(conn).Decode(&req)
	if err != nil {
		return fmt.Errorf("failed to read request: %w", err)
	}

	err = json.NewEncoder(conn).Encode(handler(req))
	if err != nil {
		return fmt.Errorf("failed to send response: %w", err)
	}

	return nil
}
