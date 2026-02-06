package main

import (
	"github.com/michaelquigley/df/dl"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "llm-gateway",
	Short: "OpenAI-compatible API proxy to OpenAI/Anthropic/Ollama via zrok",
}

func main() {
	dl.Init(dl.DefaultOptions().SetTrimPrefix("github.com/openziti/"))
	if err := rootCmd.Execute(); err != nil {
		dl.Fatal(err)
	}
}

func init() {
	rootCmd.AddCommand(newRunCommand().cmd)
}
