package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/spf13/cobra"
)

type genkeyCommand struct {
	cmd *cobra.Command
}

func newGenkeyCommand() *genkeyCommand {
	gc := &genkeyCommand{}
	gc.cmd = &cobra.Command{
		Use:   "genkey",
		Short: "Generate a new gateway API key",
		RunE:  gc.genkey,
	}
	return gc
}

func (gc *genkeyCommand) genkey(_ *cobra.Command, _ []string) error {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	fmt.Println("sk-gw-" + hex.EncodeToString(b))
	return nil
}
