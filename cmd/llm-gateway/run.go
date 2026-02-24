package main

import (
	"github.com/michaelquigley/df/dl"
	"github.com/openziti/llm-gateway/gateway"
	"github.com/spf13/cobra"
)

type runCommand struct {
	cmd      *cobra.Command
	address  string
	zrok     bool
	zrokMode string
}

func newRunCommand() *runCommand {
	rc := &runCommand{}
	rc.cmd = &cobra.Command{
		Use:   "run <configPath>",
		Short: "Run the llm-gateway server",
		Args:  cobra.ExactArgs(1),
		RunE:  rc.run,
	}
	rc.cmd.Flags().StringVar(&rc.address, "address", "", "listen address (overrides config)")
	rc.cmd.Flags().BoolVar(&rc.zrok, "zrok", false, "enable zrok sharing (overrides config)")
	rc.cmd.Flags().StringVar(&rc.zrokMode, "zrok-mode", "", "zrok share mode: public, private (overrides config)")
	return rc
}

func (rc *runCommand) run(_ *cobra.Command, args []string) error {
	configPath := args[0]
	cfg, err := gateway.LoadConfig(configPath)
	if err != nil {
		return err
	}
	dl.Infof("loaded config '%s'", configPath)

	// apply CLI overrides
	if rc.address != "" {
		cfg.Listen = rc.address
	}
	if rc.zrok {
		if cfg.Zrok == nil {
			cfg.Zrok = &gateway.ZrokConfig{}
		}
		if cfg.Zrok.Share == nil {
			cfg.Zrok.Share = &gateway.ZrokShareConfig{}
		}
		cfg.Zrok.Share.Enabled = true
	}
	if rc.zrokMode != "" {
		if cfg.Zrok == nil {
			cfg.Zrok = &gateway.ZrokConfig{}
		}
		if cfg.Zrok.Share == nil {
			cfg.Zrok.Share = &gateway.ZrokShareConfig{}
		}
		cfg.Zrok.Share.Mode = rc.zrokMode
	}

	gw, err := gateway.New(cfg)
	if err != nil {
		return err
	}
	return gw.Run()
}
