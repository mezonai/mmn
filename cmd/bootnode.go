package cmd

import (
	"context"
	"crypto/rand"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/mezonai/mmn/bootstrap"
	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/p2p"
	"github.com/spf13/cobra"
)

var (
	bootPrivKeyPath  string
	bootstrapP2pPort string
)

var bootstrapCmd = &cobra.Command{
	Use:   "bootnode",
	Short: "Run bootnode",
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
	bootstrapCmd.Flags().StringVar(&bootPrivKeyPath, "privkey-path", "", "Path to private key file")
}

func runBootstrap() {
	ctx := context.Background()

	var priv crypto.PrivKey
	var err error

	if bootPrivKeyPath != "" {
		// Load private key from file
		ed25519PrivKey, err := config.LoadEd25519PrivKey(bootPrivKeyPath)
		if err != nil {
			logx.Error("BOOTSTRAP NODE", "Failed to load private key from file:", err)
			return
		}
		// Convert Ed25519 private key to libp2p crypto format
		priv, err = p2p.UnmarshalEd25519PrivateKey(ed25519PrivKey)
		if err != nil {
			logx.Error("BOOTSTRAP NODE", "Failed to convert private key:", err)
			return
		}
		logx.Info("BOOTSTRAP NODE", "Loaded private key from:", bootPrivKeyPath)
	} else {
		// Generate random private key if no file provided
		priv, _, err = crypto.GenerateEd25519Key(rand.Reader)
		if err != nil {
			logx.Error("BOOTSTRAP NODE", "Failed to generate private key:", err)
			return
		}
		logx.Info("BOOTSTRAP NODE", "Generated random private key")
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
		logx.Info("BOOTNODE", "Listening on:", addr)
	}

	if !cfg.Bootstrap {
		if err := ddht.Bootstrap(ctx); err != nil {
			logx.Error("BOOTSTRAP NODE", "Failed to bootstrap DHT:", err)
		}
	}

	select {}
}
