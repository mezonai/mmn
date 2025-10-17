package cmd

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"os"
	"strings"
	"time"

	mmn "github.com/mezonai/mmn/client"
	"github.com/spf13/cobra"
)

var (
	blNodeURL     string
	blAddr        string
	blReason      string
	blPrivKey     string
	blPrivKeyFile string
)

var blacklistCmd = &cobra.Command{
	Use:   "blacklist",
	Short: "Manage blacklist via JSON-RPC",
}

var blacklistAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add address to blacklist",
	RunE: func(cmd *cobra.Command, args []string) error {
		if blAddr == "" {
			return fmt.Errorf("--address is required")
		}
		if blNodeURL == "" {
			blNodeURL = transferConfig.NodeURL
		}
		// Load admin private key for signing
		privKeyStr, err := loadBLPrivateKey()
		if err != nil {
			return err
		}
		adminAddr, adminPriv, err := parseBLPrivateKey(privKeyStr)
		if err != nil {
			return err
		}
		client, err := mmn.NewClient(mmn.Config{Endpoint: blNodeURL})
		if err != nil {
			return err
		}
		defer client.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Build and sign message, then send signature via SignedBL
		// Message format must match server verification: "<admin_address>|add|<address>|<reason>"
		message := fmt.Sprintf("%s|add|%s|%s", adminAddr, blAddr, blReason)
		sig := ed25519.Sign(adminPriv, []byte(message))
		signed := mmn.SignedBL{
			AdminAddress: adminAddr,
			Address:      blAddr,
			Reason:       blReason,
			Sig:          sig,
		}
		if err := client.AddToBlacklist(ctx, signed); err != nil {
			return err
		}
		fmt.Println("OK")
		return nil
	},
}

var blacklistListCmd = &cobra.Command{
	Use:   "list",
	Short: "List blacklisted addresses",
	RunE: func(cmd *cobra.Command, args []string) error {
		if blNodeURL == "" {
			blNodeURL = transferConfig.NodeURL
		}
		client, err := mmn.NewClient(mmn.Config{Endpoint: blNodeURL})
		if err != nil {
			return err
		}
		defer client.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// If the server requires auth for listing, pass an empty or signed payload. Here we pass empty.
		entries, err := client.ListBlacklist(ctx, mmn.SignedBL{})
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			fmt.Println("(empty)")
			return nil
		}
		for addr, reason := range entries {
			fmt.Printf("%s\t%s\n", addr, reason)
		}
		return nil
	},
}

var blacklistRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove address from blacklist",
	RunE: func(cmd *cobra.Command, args []string) error {
		if blAddr == "" {
			return fmt.Errorf("--address is required")
		}
		if blNodeURL == "" {
			blNodeURL = transferConfig.NodeURL
		}
		// Load admin private key for signing
		privKeyStr, err := loadBLPrivateKey()
		if err != nil {
			return err
		}
		adminAddr, adminPriv, err := parseBLPrivateKey(privKeyStr)
		if err != nil {
			return err
		}
		client, err := mmn.NewClient(mmn.Config{Endpoint: blNodeURL})
		if err != nil {
			return err
		}
		defer client.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Sign remove as well
		// Message format must match server verification: "<admin_address>|remove|<address>|remove"
		message := fmt.Sprintf("%s|remove|%s|%s", adminAddr, blAddr, "remove")
		sig := ed25519.Sign(adminPriv, []byte(message))
		signed := mmn.SignedBL{
			AdminAddress: adminAddr,
			Address:      blAddr,
			Reason:       "spam blacklist",
			Sig:          sig,
		}
		if err := client.RemoveFromBlacklist(ctx, signed); err != nil {
			return err
		}
		fmt.Println("OK")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(blacklistCmd)
	blacklistCmd.AddCommand(blacklistAddCmd)
	blacklistCmd.AddCommand(blacklistListCmd)
	blacklistCmd.AddCommand(blacklistRemoveCmd)
	blacklistAddCmd.Flags().StringVar(&blNodeURL, "node-url", "localhost:9001", "gRPC node URL (host:port)")
	blacklistAddCmd.Flags().StringVar(&blAddr, "address", "", "address to blacklist")
	blacklistAddCmd.Flags().StringVar(&blReason, "reason", "manual", "reason for blacklisting")
	blacklistAddCmd.Flags().StringVar(&blPrivKey, "private-key", "", "admin private key in hex")
	blacklistAddCmd.Flags().StringVar(&blPrivKeyFile, "private-key-file", "", "admin private key file (hex)")
	blacklistListCmd.Flags().StringVar(&blNodeURL, "node-url", "localhost:9001", "gRPC node URL (host:port)")
	blacklistRemoveCmd.Flags().StringVar(&blNodeURL, "node-url", "localhost:9001", "gRPC node URL (host:port)")
	blacklistRemoveCmd.Flags().StringVar(&blAddr, "address", "", "address to remove from blacklist")
	blacklistRemoveCmd.Flags().StringVar(&blPrivKey, "private-key", "", "admin private key in hex")
	blacklistRemoveCmd.Flags().StringVar(&blPrivKeyFile, "private-key-file", "", "admin private key file (hex)")
}

// loadBLPrivateKey loads admin private key from flag or file
func loadBLPrivateKey() (string, error) {
	if blPrivKey != "" {
		return blPrivKey, nil
	}
	if blPrivKeyFile == "" {
		return "", fmt.Errorf("--private-key or --private-key-file is required")
	}
	b, err := os.ReadFile(strings.TrimSpace(blPrivKeyFile))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// parseBLPrivateKey converts a hex private key to ed25519 key and returns base58 pubkey string
func parseBLPrivateKey(privKeyHex string) (string, ed25519.PrivateKey, error) {
	s := strings.TrimSpace(privKeyHex)
	addr, pk, err := parsePrivateKey(s)
	if err != nil {
		return "", nil, err
	}
	return addr, pk, nil
}
