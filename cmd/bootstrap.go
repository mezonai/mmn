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

var bootstrapCmd = &cobra.Command{
	Use:   "bootstrap",
	Short: "Run node in bootstrap mode",
	Run: func(cmd *cobra.Command, args []string) {
		runBootstrap()
	},
}

func init() {
	rootCmd.AddCommand(bootstrapCmd)
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

	host, ddht, err := bootstrap.CreateNode(ctx, cfg)
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
