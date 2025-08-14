package cmd

import (
	"context"
	"crypto/rand"
	"fmt"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/mezonai/mmn/bootstrap"
	"github.com/mezonai/mmn/logx"
	"github.com/spf13/cobra"
)

var (
	bootstrapP2pPort string
)

var bootstrapCmd = &cobra.Command{
	Use:   "bootstrap",
	Short: "Run bootstrap node",
	Run: func(cmd *cobra.Command, args []string) {
		runBootstrap()
	},
}

func init() {
	rootCmd.AddCommand(bootstrapCmd)
	bootstrapCmd.Flags().StringVar(
		&bootstrapP2pPort,
		"bootstrap-p2p-port",
		"9000",
		"Bootstrap Node listen multiaddress /ip4/0.0.0.0/tcp/<port>",
	)

}

func runBootstrap() {
	ctx := context.Background()

	priv, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		logx.Error("BOOTSTRAP NODE", "Failed to generate private key:", err)
	}

	cfg := &bootstrap.Config{
		PrivateKey: priv,
		Bootstrap:  true, // set false if you don't want to bootstrap
	}

	host, ddht, err := bootstrap.CreateNode(ctx, cfg, bootstrapP2pPort)
	if err != nil {
		logx.Error("BOOTSTRAP NODE", "Failed to create node:", err)
	}

	logx.Info("BOOTSTRAP NODE", "Node ID:", host.ID().String())
	for _, addr := range host.Addrs() {
		fmt.Println("Listening on:", addr)
	}

	if !cfg.Bootstrap {
		if err := ddht.Bootstrap(ctx); err != nil {
			logx.Error("BOOTSTRAP NODE", "Failed to bootstrap DHT:", err)
		}
	}

	select {}
}
