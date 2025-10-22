package cmd

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/holiman/uint256"
	"github.com/mezonai/mmn/faucet"
	"github.com/mezonai/mmn/logx"
	"github.com/spf13/cobra"
)

var (
	faucetDataDir string
)

func init() {
	rootCmd.AddCommand(faucetCmd)

	faucetCmd.PersistentFlags().StringVar(&faucetDataDir, "data-dir", "./data/faucet", "Faucet data directory")

	faucetCmd.AddCommand(faucetProposeCmd)
	faucetCmd.AddCommand(faucetListCmd)
	faucetCmd.AddCommand(faucetApproveCmd)
	faucetCmd.AddCommand(faucetRejectCmd)
	faucetCmd.AddCommand(faucetBroadcastCmd)

	// Propose flags
	faucetProposeCmd.Flags().String("from", "", "Faucet sender address (base58)")
	faucetProposeCmd.Flags().String("to", "", "Recipient address (base58)")
	faucetProposeCmd.Flags().String("amount", "", "Amount in decimal string, e.g. 1_000_000")
	faucetProposeCmd.Flags().String("message", "Funding at "+time.Now().Format(time.DateTime), "Message")
	faucetProposeCmd.Flags().Int("threshold", 2, "Approval threshold")
	faucetProposeCmd.Flags().StringSlice("signers", []string{}, "List of signer addresses")

	// Approve/Reject flags
	faucetApproveCmd.Flags().String("id", "", "Proposal ID")
	faucetApproveCmd.Flags().String("signer", "", "Signer address (base58)")
	faucetApproveCmd.Flags().String("note", "", "Approval note")

	faucetRejectCmd.Flags().String("id", "", "Proposal ID")
	faucetRejectCmd.Flags().String("reason", "", "Rejection reason")

	// Broadcast flags
	faucetBroadcastCmd.Flags().String("id", "", "Proposal ID")
}

var faucetCmd = &cobra.Command{
	Use:   "faucet",
	Short: "Faucet management commands",
}

var faucetProposeCmd = &cobra.Command{
	Use:   "propose",
	Short: "Create a faucet proposal",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := faucet.NewFileStore(faucetDataDir)
		if err != nil {
			return err
		}

		from, _ := cmd.Flags().GetString("from")
		to, _ := cmd.Flags().GetString("to")
		amountStr, _ := cmd.Flags().GetString("amount")
		msg, _ := cmd.Flags().GetString("message")
		threshold, _ := cmd.Flags().GetInt("threshold")
		signers, _ := cmd.Flags().GetStringSlice("signers")

		if from == "" || to == "" || amountStr == "" || threshold <= 0 || len(signers) == 0 {
			return fmt.Errorf("missing required flags: --from, --to, --amount, --threshold, --signers")
		}

		// normalize amount: allow underscores
		amountStr = strings.ReplaceAll(amountStr, "_", "")
		if _, err := uint256.FromDecimal(amountStr); err != nil {
			return fmt.Errorf("invalid amount: %w", err)
		}

		p := &faucet.Proposal{
			ID:        uuid.NewString(),
			Sender:    from,
			Recipient: to,
			Amount:    amountStr,
			Message:   msg,
			Nonce:     0, // filled at broadcast
			CreatedAt: time.Now(),
			ExpiresAt: time.Now().Add(48 * time.Hour),
			Threshold: threshold,
			Signers:   signers,
			Approvals: map[string]string{},
			Status:    faucet.ProposalPending,
		}
		if err := store.Upsert(p); err != nil {
			return err
		}
		logx.Info("FAUCET", fmt.Sprintf("Created proposal %s -> %s amount=%s id=%s", from, to, amountStr, p.ID))
		fmt.Println(p.ID)
		return nil
	},
}

var faucetListCmd = &cobra.Command{
	Use:   "list",
	Short: "List faucet proposals",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := faucet.NewFileStore(faucetDataDir)
		if err != nil {
			return err
		}
		items, err := store.List()
		if err != nil {
			return err
		}
		for _, p := range items {
			fmt.Printf("%s\t%s\t%s\t%s\t%d/%d\t%s\n", p.ID, p.Sender, p.Recipient, p.Amount, len(p.Approvals), p.Threshold, p.Status)
		}
		return nil
	},
}

var faucetApproveCmd = &cobra.Command{
	Use:   "approve",
	Short: "Approve faucet proposal",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := faucet.NewFileStore(faucetDataDir)
		if err != nil {
			return err
		}
		id, _ := cmd.Flags().GetString("id")
		signer, _ := cmd.Flags().GetString("signer")
		note, _ := cmd.Flags().GetString("note")
		if id == "" || signer == "" {
			return fmt.Errorf("missing --id or --signer")
		}
		p, err := store.Get(id)
		if err != nil {
			return err
		}
		allowed := false
		for _, s := range p.Signers {
			if s == signer {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("signer not allowed")
		}
		if p.Approvals == nil {
			p.Approvals = map[string]string{}
		}
		p.Approvals[signer] = note
		if p.IsFullyApproved() {
			p.Status = faucet.ProposalFullyApproved
		} else {
			p.Status = faucet.ProposalPartiallyApproved
		}
		if err := store.Upsert(p); err != nil {
			return err
		}
		fmt.Printf("approved %s by %s (%d/%d)\n", id, signer, len(p.Approvals), p.Threshold)
		return nil
	},
}

var faucetRejectCmd = &cobra.Command{
	Use:   "reject",
	Short: "Reject faucet proposal",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := faucet.NewFileStore(faucetDataDir)
		if err != nil {
			return err
		}
		id, _ := cmd.Flags().GetString("id")
		reason, _ := cmd.Flags().GetString("reason")
		if id == "" {
			return fmt.Errorf("missing --id")
		}
		p, err := store.Get(id)
		if err != nil {
			return err
		}
		p.Status = faucet.ProposalRejected
		if p.Approvals == nil {
			p.Approvals = map[string]string{}
		}
		p.Approvals["__rejected__"] = reason
		return store.Upsert(p)
	},
}

var faucetBroadcastCmd = &cobra.Command{
	Use:   "broadcast",
	Short: "Broadcast fully approved faucet proposal",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := faucet.NewFileStore(faucetDataDir)
		if err != nil {
			return err
		}
		id, _ := cmd.Flags().GetString("id")
		if id == "" {
			return fmt.Errorf("missing --id")
		}
		p, err := store.Get(id)
		if err != nil {
			return err
		}
		if !p.IsFullyApproved() {
			return fmt.Errorf("proposal not fully approved")
		}

		// For now: just mark as broadcasted; future: build and send tx via gRPC
		p.Status = faucet.ProposalBroadcasted
		return store.Upsert(p)
	},
}

// helper for parsing hex ed25519 private key (reserved for future sign-on-broadcast)
func parseEd25519Hex(priv string) (ed25519.PrivateKey, error) {
	b, err := hex.DecodeString(strings.TrimSpace(priv))
	if err != nil {
		return nil, err
	}
	return ed25519.NewKeyFromSeed(b[len(b)-ed25519.SeedSize:]), nil
}
