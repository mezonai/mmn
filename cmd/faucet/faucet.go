package faucet

import (
	"github.com/spf13/cobra"
)

// FaucetCmd represents the faucet command
var FaucetCmd = &cobra.Command{
	Use:   "faucet",
	Short: "Perform actions with faucet wallet",
}

func init() {
	FaucetCmd.AddCommand(fundCmd)
}
