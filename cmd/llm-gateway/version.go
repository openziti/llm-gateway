package main

import (
	"fmt"

	"github.com/openziti/llm-gateway/build"
	"github.com/spf13/cobra"
)

type versionCommand struct {
	cmd *cobra.Command
}

func newVersionCommand() *versionCommand {
	vc := &versionCommand{}
	vc.cmd = &cobra.Command{
		Use:   "version",
		Short: "Show the llm-gateway version",
		Run:   vc.version,
	}
	return vc
}

func (vc *versionCommand) version(_ *cobra.Command, _ []string) {
	fmt.Println(build.String())
}
