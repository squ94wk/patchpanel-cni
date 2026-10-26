package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/version"

	"github.com/squ94wk/patchpanel-cni/internal/protocol"
)

func forward(command string, args *skel.CmdArgs) (json.RawMessage, error) {
	resp, err := protocol.Call(protocol.SocketPath, protocol.Request{
		Command:     command,
		ContainerID: args.ContainerID,
		Netns:       args.Netns,
		IfName:      args.IfName,
		Args:        args.Args,
		Path:        args.Path,
		Stdin:       args.StdinData,
	})
	if err != nil {
		return nil, types.NewError(types.ErrIOFailure, "failed to reach patchpanel-agent", err.Error())
	}
	if resp.Error != nil {
		return nil, resp.Error
	}

	return resp.Result, nil
}

func main() {
	skel.PluginMainFuncs(skel.CNIFuncs{
		Add: func(args *skel.CmdArgs) error {
			result, err := forward("ADD", args)
			if err != nil {
				return err
			}

			_, err = os.Stdout.Write(result)
			if err != nil {
				return types.NewError(types.ErrIOFailure, "failed to write result", fmt.Sprint(err))
			}

			return nil
		},
		Del: func(args *skel.CmdArgs) error {
			_, err := forward("DEL", args)
			return err
		},
		Check: func(*skel.CmdArgs) error { return nil },
	}, version.PluginSupports("0.3.0", "0.3.1", "0.4.0", "1.0.0", "1.1.0"), "patchpanel")
}
