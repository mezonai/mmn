package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"mmn/logx"

	"github.com/libp2p/go-libp2p/core/crypto"
)

func main() {
	ctx := context.Background()

	priv, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		logx.Error("BOOTSTRAP NODE", "Failed to generate private key:", err)
	}

	cfg := &Config{
		PrivateKey: priv,
		Bootstrap:  true, // set false if you don't want to bootstrap
	}

	host, ddht, err := CreateNode(ctx, cfg)
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
