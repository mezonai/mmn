package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	pb "github.com/mezonai/mmn/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	// Connect to gRPC server
	conn, err := grpc.Dial("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewTxServiceClient(conn)

	// Generate test keypairs
	proposerPubKey, proposerPrivKey, _ := ed25519.GenerateKey(nil)
	approver1PubKey, approver1PrivKey, _ := ed25519.GenerateKey(nil)
	approver2PubKey, approver2PrivKey, _ := ed25519.GenerateKey(nil)

	proposerPubKeyStr := hex.EncodeToString(proposerPubKey)
	approver1PubKeyStr := hex.EncodeToString(approver1PubKey)
	approver2PubKeyStr := hex.EncodeToString(approver2PubKey)

	fmt.Printf("Generated test keypairs:\n")
	fmt.Printf("Proposer: %s\n", proposerPubKeyStr)
	fmt.Printf("Approver 1: %s\n", approver1PubKeyStr)
	fmt.Printf("Approver 2: %s\n", approver2PubKeyStr)
	fmt.Println()

	// Test 1: Add proposer to whitelist
	fmt.Println("=== Test 1: Add proposer to whitelist ===")
	message1 := fmt.Sprintf("faucet_action:add_to_proposer_whitelist:%d", time.Now().Unix())
	signature1 := ed25519.Sign(proposerPrivKey, []byte(message1))

	_, err = client.AddToProposerWhitelist(context.Background(), &pb.AddToProposerWhitelistRequest{
		Address:      proposerPubKeyStr,
		SignerPubkey: proposerPubKeyStr,
		Signature:    hex.EncodeToString(signature1),
	})
	if err != nil {
		log.Printf("Failed to add proposer to whitelist: %v", err)
	} else {
		fmt.Println("✅ Proposer added to whitelist successfully")
	}

	// Test 2: Add approvers to whitelist
	fmt.Println("\n=== Test 2: Add approvers to whitelist ===")
	message2 := fmt.Sprintf("faucet_action:add_to_approver_whitelist:%d", time.Now().Unix())
	signature2 := ed25519.Sign(approver1PrivKey, []byte(message2))

	_, err = client.AddToApproverWhitelist(context.Background(), &pb.AddToApproverWhitelistRequest{
		Address:      approver1PubKeyStr,
		SignerPubkey: approver1PubKeyStr,
		Signature:    hex.EncodeToString(signature2),
	})
	if err != nil {
		log.Printf("Failed to add approver 1 to whitelist: %v", err)
	} else {
		fmt.Println("✅ Approver 1 added to whitelist successfully")
	}

	message3 := fmt.Sprintf("faucet_action:add_to_approver_whitelist:%d", time.Now().Unix())
	signature3 := ed25519.Sign(approver2PrivKey, []byte(message3))

	_, err = client.AddToApproverWhitelist(context.Background(), &pb.AddToApproverWhitelistRequest{
		Address:      approver2PubKeyStr,
		SignerPubkey: approver2PubKeyStr,
		Signature:    hex.EncodeToString(signature3),
	})
	if err != nil {
		log.Printf("Failed to add approver 2 to whitelist: %v", err)
	} else {
		fmt.Println("✅ Approver 2 added to whitelist successfully")
	}

	// Test 3: Check whitelist status
	fmt.Println("\n=== Test 3: Check whitelist status ===")
	resp, err := client.CheckWhitelistStatus(context.Background(), &pb.CheckWhitelistStatusRequest{
		Address: proposerPubKeyStr,
	})
	if err != nil {
		log.Printf("Failed to check whitelist status: %v", err)
	} else {
		fmt.Printf("✅ Proposer status - IsProposer: %v, IsApprover: %v\n", resp.IsProposer, resp.IsApprover)
	}

	// Test 4: Create faucet request
	fmt.Println("\n=== Test 4: Create faucet request ===")
	message4 := fmt.Sprintf("faucet_action:create_faucet_request:%d", time.Now().Unix())
	signature4 := ed25519.Sign(proposerPrivKey, []byte(message4))

	createResp, err := client.CreateFaucetRequest(context.Background(), &pb.CreateFaucetRequestRequest{
		MultisigAddress: "test_multisig_address", // This should be a real multisig address
		Recipient:       "test_recipient_address",
		Amount:          "1000",
		TextData:        "Test faucet request",
		SignerPubkey:    proposerPubKeyStr,
		Signature:       hex.EncodeToString(signature4),
	})
	if err != nil {
		log.Printf("Failed to create faucet request: %v", err)
	} else {
		fmt.Printf("✅ Faucet request created successfully: %s\n", createResp.TxHash)
	}

	// Test 5: Add signatures
	if createResp.Success {
		fmt.Println("\n=== Test 5: Add signatures ===")
		
		// Approver 1 signs
		message5 := fmt.Sprintf("faucet_action:add_signature:%d", time.Now().Unix())
		signature5 := ed25519.Sign(approver1PrivKey, []byte(message5))

		addSigResp1, err := client.AddSignature(context.Background(), &pb.AddSignatureRequest{
			TxHash:       createResp.TxHash,
			SignerPubkey: approver1PubKeyStr,
			Signature:    hex.EncodeToString(signature5),
		})
		if err != nil {
			log.Printf("Failed to add signature from approver 1: %v", err)
		} else {
			fmt.Printf("✅ Approver 1 signature added. Total signatures: %d\n", addSigResp1.SignatureCount)
		}

		// Approver 2 signs
		message6 := fmt.Sprintf("faucet_action:add_signature:%d", time.Now().Unix())
		signature6 := ed25519.Sign(approver2PrivKey, []byte(message6))

		addSigResp2, err := client.AddSignature(context.Background(), &pb.AddSignatureRequest{
			TxHash:       createResp.TxHash,
			SignerPubkey: approver2PubKeyStr,
			Signature:    hex.EncodeToString(signature6),
		})
		if err != nil {
			log.Printf("Failed to add signature from approver 2: %v", err)
		} else {
			fmt.Printf("✅ Approver 2 signature added. Total signatures: %d\n", addSigResp2.SignatureCount)
		}

		// Test 6: Check transaction status
		fmt.Println("\n=== Test 6: Check transaction status ===")
		statusResp, err := client.GetMultisigTransactionStatus(context.Background(), &pb.GetMultisigTransactionStatusRequest{
			TxHash: createResp.TxHash,
		})
		if err != nil {
			log.Printf("Failed to get transaction status: %v", err)
		} else {
			fmt.Printf("✅ Transaction status: %s\n", statusResp.Status)
			fmt.Printf("   Signatures: %d/%d\n", statusResp.SignatureCount, statusResp.RequiredSignatures)
		}

		// Test 7: Check final status (transaction should be executed automatically)
		fmt.Println("\n=== Test 7: Check final status ===")
		time.Sleep(1 * time.Second) // Wait a bit for automatic execution
		
		finalStatusResp, err := client.GetMultisigTransactionStatus(context.Background(), &pb.GetMultisigTransactionStatusRequest{
			TxHash: createResp.TxHash,
		})
		if err != nil {
			log.Printf("Failed to get final transaction status: %v", err)
		} else {
			fmt.Printf("✅ Final transaction status: %s\n", finalStatusResp.Status)
			fmt.Printf("   Signatures: %d/%d\n", finalStatusResp.SignatureCount, finalStatusResp.RequiredSignatures)
			
			if finalStatusResp.Status == "executed" {
				fmt.Println("🎉 Transaction was executed automatically!")
			} else {
				fmt.Println("⚠️  Transaction is still pending - may need more signatures")
			}
		}
	}

	fmt.Println("\n=== Test completed ===")
}
