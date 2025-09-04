package cmd

import (
	"os"

	"github.com/mezonai/mmn/logx"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "mmn",
	Short: "MMN blockchain node CLI",
	Long:  "Command line interface for running and managing an MMN blockchain node.",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		logx.Error("CMD", "Command execution failed:", err)
		os.Exit(1)
	}
}
