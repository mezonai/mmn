package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	jwt "github.com/golang-jwt/jwt/v4"
	"github.com/mezonai/mmn/client"
	"github.com/mezonai/mmn/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	JwtSecret     = "defaultencryptionkey"
	ZkVerifyUrl   = "http://172.16.100.180:8282"
	FaucetAddress = "HVZMR6CvrVV5mKWhuYW2Ujcd9nRHeg7zCNkkQXeT43Ka"
	FaucetTxHash  = "69DuXmtK5RdS5vanPJGrMjpASeWTrzJUSahvfWQgqscW"
)

type Account struct {
	PublicKey  string
	PrivateKey ed25519.PrivateKey
	Nonce      uint64
	Balance    uint64
	Address    string
	ZkProof    string
	ZkPub      string
}

type SessionTokenClaims struct {
	TokenId   string `json:"tid,omitempty"`
	UserId    int64  `json:"uid,omitempty"`
	Username  string `json:"usn,omitempty"`
	ExpiresAt int64  `json:"exp,omitempty"`
}

func (stc *SessionTokenClaims) Valid() error {
	if stc.ExpiresAt <= time.Now().UTC().Unix() {
		return fmt.Errorf("token is expired")
	}
	return nil
}

type ProveData struct {
	Proof       string `json:"proof"`
	PublicInput string `json:"public_input"`
}

type ProveResponse struct {
	Success bool      `json:"success"`
	Data    ProveData `json:"data"`
	Message string    `json:"message"`
}

func main() {
	grpcAddress := "localhost:9001"

	// Create gRPC connection
	conn, err := grpc.Dial(grpcAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(50*1024*1024)),
		grpc.WithDefaultCallOptions(grpc.MaxCallSendMsgSize(50*1024*1024)),
	)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()
	grpcClient := proto.NewTxServiceClient(conn)
	accountClient := proto.NewAccountServiceClient(conn)
	ctx := context.Background()

	// userId1 := int64(1111)
	// address1 := client.GenerateAddress(strconv.FormatInt(userId1, 10))
	// pubKey1, privateKey1, _ := createKeyFromHex("4303ad33ea39c199ccca4e88df8665013b1d6c4f36f76f4b9b35e7e86550ff11")
	// jwt1 := generateJwt(userId1)
	// publicKeyHex1 := base58.Encode(pubKey1)

	// userId2 := int64(2222)
	// address2 := client.GenerateAddress(strconv.FormatInt(userId2, 10))
	// pubKey2, privateKey2, _ := createKeyFromHex("e8595db7ef8a51bfe6676d246987fd977513910e553222e95238511bf0785cad")
	// jwt2 := generateJwt(userId2)
	// publicKeyHex2 := base58.Encode(pubKey2)

	userId3 := int64(3333)
	address3 := client.GenerateAddress(strconv.FormatInt(userId3, 10))
	// pubKey3, privateKey3, _ := createKeyFromHex("88d95f44531ee0730fcafd52350ea72c7aff05ab7772adad3688c187b812a088")
	// jwt3 := generateJwt(userId3)
	// publicKeyHex3 := base58.Encode(pubKey3)

	// proofRes1, err1 := generateZkProof(strconv.FormatInt(userId1, 10), address1, publicKeyHex1, jwt1)
	// if err1 != nil {
	// 	fmt.Println("error: generate zk proof fail", err1.Error())
	// 	return
	// }
	// zkPub1 := proofRes1.Data.PublicInput
	// zkProof1 := proofRes1.Data.Proof

	// proofRes2, err2 := generateZkProof(strconv.FormatInt(userId2, 10), address2, publicKeyHex2, jwt2)
	// if err2 != nil {
	// 	fmt.Println("error: generate zk proof fail for address2", err2.Error())
	// 	return
	// }
	// zkPub2 := proofRes2.Data.PublicInput
	// zkProof2 := proofRes2.Data.Proof

	// proofRes3, err3 := generateZkProof(strconv.FormatInt(userId3, 10), address3, publicKeyHex3, jwt3)
	// if err3 != nil {
	// 	fmt.Println("error: generate zk proof fail", err3.Error())
	// 	return
	// }
	// zkPub3 := proofRes3.Data.PublicInput
	// zkProof3 := proofRes3.Data.Proof

	// fmt.Println("Checking whitelist status for address1:", address1)

	checkWhitelistStatus(ctx, grpcClient)

	// testAddProposer(ctx, grpcClient, privateKey1, address1, address3, zkProof1, zkPub1)
	// testApprove(ctx, grpcClient, privateKey2, address2, address3, zkProof2, zkPub2)
	// addFaucetProposal(ctx, grpcClient, privateKey3, FaucetAddress, "1000000",  "", address3, zkProof3, zkPub3)
	// approveProposal(ctx, grpcClient, privateKey2, FaucetTxHash, address2, zkProof2, zkPub2)
	// approveProposal(ctx, grpcClient, privateKey1, FaucetTxHash, address1, zkProof1, zkPub1)
	checkFaucetAndRecipientBalances(ctx, accountClient, FaucetAddress, address3)

}

func createKeyFromHex(privateKeyHex string) (ed25519.PublicKey, ed25519.PrivateKey, error) {
	// Decode hex string to bytes
	privateKeyBytes, err := hex.DecodeString(privateKeyHex)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode hex: %v", err)
	}

	// Create Ed25519 key from seed
	privateKey := ed25519.NewKeyFromSeed(privateKeyBytes)
	publicKey := privateKey.Public().(ed25519.PublicKey)

	return publicKey, privateKey, nil
}

func generateJwt(userID int64) string {
	exp := time.Now().UTC().Add(time.Duration(24) * time.Hour).Unix()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, &SessionTokenClaims{
		TokenId:   strconv.FormatInt(userID, 10),
		UserId:    userID,
		Username:  strconv.FormatInt(userID, 10),
		ExpiresAt: exp,
	})
	signedToken, _ := token.SignedString([]byte(JwtSecret))
	return signedToken
}

func generateZkProof(userID, address, ephemeralPK, jwt string) (*ProveResponse, error) {
	url := ZkVerifyUrl + "/prove"
	// Create request body
	requestBody := map[string]string{
		"user_id":      userID,
		"address":      address,
		"ephemeral_pk": ephemeralPK,
		"jwt":          jwt,
	}

	// Convert to JSON
	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %v", err)
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	// Set headers
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Content-Type", "application/json")

	// Send request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("prove request failed: %d %s - %s",
			resp.StatusCode, resp.Status, string(body))
	}

	// Parse response
	var proveResp ProveResponse
	if err := json.NewDecoder(resp.Body).Decode(&proveResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	return &proveResp, nil
}

func isValidUTF8(s string) bool {
	return utf8.ValidString(s)
}

func checkWhitelistStatus(ctx context.Context, client proto.TxServiceClient) {
	reqProposer := &proto.GetProposerWhitelistRequest{}

	resProposer, err := client.GetProposerWhitelist(ctx, reqProposer)
	if err != nil {
		fmt.Printf("✗ Failed to check whitelist status: %v\n", err)
		return
	}

	fmt.Printf("Whitelist for proposer %s:\n", resProposer)

	reqApprover := &proto.GetApproverWhitelistRequest{}

	resApprover, err := client.GetApproverWhitelist(ctx, reqApprover)
	if err != nil {
		fmt.Printf("✗ Failed to check whitelist status: %v\n", err)
		return
	}

	fmt.Printf("Whitelist for proposer %s:\n", resApprover)
}

// add proposer + apprve proposer
func testApprove(ctx context.Context, client proto.TxServiceClient, privateKey ed25519.PrivateKey, approverAddr, address string, zkProof, zkPub string) {
	fmt.Println("Checking whitelist status for approver:", approverAddr)
	checkWhitelistStatus(ctx, client)

	action := "ADD_PROPOSER"
	message := fmt.Sprintf("FAUCET_ACTION:%s", action)
	signature := ed25519.Sign(privateKey, []byte(message))

	type UserSig struct {
		PubKey []byte `json:"pub_key"`
		Sig    []byte `json:"sig"`
	}

	pubKey := privateKey.Public().(ed25519.PublicKey)

	userSig := UserSig{
		PubKey: pubKey,
		Sig:    signature,
	}

	signatureJSON, err := json.Marshal(userSig)
	if err != nil {
		fmt.Printf("✗ Failed to marshal signature: %v\n", err)
		return
	}
	signatureHex := hex.EncodeToString(signatureJSON)

	if zkProof != "" {
		fmt.Printf("ZkProof: %s...\n", zkProof[:50])
		fmt.Printf("ZkPub: %s...\n", zkPub[:50])
	}

	// Create gRPC request
	req := &proto.AddToProposerWhitelistRequest{
		Address:      address,
		SignerPubkey: approverAddr,
		Signature:    signatureHex,
		ZkProof:      zkProof,
		ZkPub:        zkPub,
	}

	fmt.Println("Sending AddSignature gRPC request...", approverAddr)
	resp, err := client.AddToProposerWhitelist(ctx, req)
	if err != nil {
		fmt.Printf("✗ gRPC call failed: %v\n", err)
		return
	}

	if resp.Success {
		fmt.Printf("✓ Success! Message: %s\n", resp.Message)
	} else {
		fmt.Printf("✗ Request failed: %s\n", resp.Message)
	}
}

func testAddProposer(ctx context.Context, client proto.TxServiceClient, privateKey ed25519.PrivateKey, approverAddr, proposerAddr, zkProof, zkPub string) {
	// Build signature
	action := "ADD_PROPOSER"
	message := fmt.Sprintf("FAUCET_ACTION:%s", action)
	signature := ed25519.Sign(privateKey, []byte(message))

	type UserSig struct {
		PubKey []byte `json:"pub_key"`
		Sig    []byte `json:"sig"`
	}

	pubKey := privateKey.Public().(ed25519.PublicKey)

	userSig := UserSig{
		PubKey: pubKey,
		Sig:    signature,
	}

	signatureJSON, err := json.Marshal(userSig)
	if err != nil {
		fmt.Printf("✗ Failed to marshal signature: %v\n", err)
		return
	}
	signatureHex := hex.EncodeToString(signatureJSON)

	if zkProof != "" {
		fmt.Printf("ZkProof: %s...\n", zkProof[:50])
		fmt.Printf("ZkPub: %s...\n", zkPub[:50])
	}

	// Create gRPC request
	req := &proto.AddToProposerWhitelistRequest{
		Address:      proposerAddr,
		SignerPubkey: approverAddr,
		Signature:    signatureHex,
		ZkProof:      zkProof,
		ZkPub:        zkPub,
	}

	resp, err := client.AddToProposerWhitelist(ctx, req)
	if err != nil {
		fmt.Printf("✗ gRPC call failed: %v\n", err)
		return
	}

	if resp.Success {
		fmt.Printf("✓ Success! Message: %s\n", resp.Message)
	} else {
		fmt.Printf("✗ Request failed: %s\n", resp.Message)
	}
}

// addFaucetProposal creates a faucet request proposal for testing
func addFaucetProposal(ctx context.Context, client proto.TxServiceClient, privateKey ed25519.PrivateKey,
	multisigAddress, amount, textData, signerPubkey, zkProof, zkPub string) {
	action := "CREATE_FAUCET"
	message := fmt.Sprintf("FAUCET_ACTION:%s", action)
	signature := ed25519.Sign(privateKey, []byte(message))

	type UserSig struct {
		PubKey []byte `json:"pub_key"`
		Sig    []byte `json:"sig"`
	}

	pubKey := privateKey.Public().(ed25519.PublicKey)

	userSig := UserSig{
		PubKey: pubKey,
		Sig:    signature,
	}

	signatureJSON, err := json.Marshal(userSig)
	if err != nil {
		fmt.Printf("✗ Failed to marshal signature: %v\n", err)
		return
	}
	signatureHex := hex.EncodeToString(signatureJSON)

	if zkProof != "" {
		fmt.Printf("ZkProof: %s...\n", zkProof[:min(50, len(zkProof))])
		fmt.Printf("ZkPub: %s...\n", zkPub[:min(50, len(zkPub))])
	}

	// Create gRPC request
	req := &proto.CreateFaucetRequestRequest{
		MultisigAddress: multisigAddress,
		Amount:          amount,
		TextData:        textData,
		SignerPubkey:    signerPubkey,
		Signature:       signatureHex,
		ZkProof:         zkProof,
		ZkPub:           zkPub,
	}

	fmt.Printf("📝 Creating faucet proposal: %s to %s (amount: %s)\n", multisigAddress, amount)
	resp, err := client.CreateFaucetRequest(ctx, req)
	if err != nil {
		fmt.Printf("✗ gRPC call failed: %v\n", err)
		return
	}

	if resp.Success {
		fmt.Printf("✓ Success! Tx Hash: %s\n", resp.TxHash)
		fmt.Printf("  Message: %s\n", resp.Message)
	} else {
		fmt.Printf("✗ Request failed: %s\n", resp.Message)
	}
}

func approveProposal(ctx context.Context, client proto.TxServiceClient, privateKey ed25519.PrivateKey,
	txHash,signerPubkey, zkProof, zkPub string) {
	// Build signature for ADD_SIGNATURE action
	action := "ADD_SIGNATURE"
	message := fmt.Sprintf("FAUCET_ACTION:%s", action)
	signature := ed25519.Sign(privateKey, []byte(message))

	type UserSig struct {
		PubKey []byte `json:"pub_key"`
		Sig    []byte `json:"sig"`
	}

	pubKey := privateKey.Public().(ed25519.PublicKey)
	userSig := UserSig{PubKey: pubKey, Sig: signature}

	signatureJSON, err := json.Marshal(userSig)
	if err != nil {
		fmt.Printf("✗ Failed to marshal signature: %v\n", err)
		return
	}
	signatureHex := hex.EncodeToString(signatureJSON)

	// Create gRPC request
	req := &proto.AddSignatureRequest{
		TxHash:       txHash,
		SignerPubkey: signerPubkey,
		Signature:    signatureHex,
		ZkProof:      zkProof,
		ZkPub:        zkPub,
	}

	fmt.Printf("🖊️ Approving proposal %s\n", txHash)
	resp, err := client.AddSignature(ctx, req)
	if err != nil {
		fmt.Printf("✗ gRPC call failed: %v\n", err)
		return
	}

	if resp.Success {
		fmt.Printf("✓ Approved! Signatures: %d\n", resp.SignatureCount)
	} else {
		fmt.Printf("✗ Approval failed: %s\n", resp.Message)
	}
}

func getBalance(ctx context.Context, accountClient proto.AccountServiceClient, address string) (string, uint64, error) {
	req := &proto.GetAccountRequest{Address: address}
	resp, err := accountClient.GetAccount(ctx, req)
	if err != nil {
		return "", 0, err
	}
	return resp.Balance, resp.Nonce, nil
}

func checkFaucetAndRecipientBalances(ctx context.Context, accountClient proto.AccountServiceClient, faucetAddress, recipientAddress string) {
	fb, fn, err := getBalance(ctx, accountClient, faucetAddress)
	if err != nil {
		fmt.Printf("✗ Failed to get faucet balance: %v\n", err)
	} else {
		fmt.Printf("Faucet %s -> balance: %s, nonce: %d\n", faucetAddress, fb, fn)
	}

	rb, rn, err := getBalance(ctx, accountClient, recipientAddress)
	if err != nil {
		fmt.Printf("✗ Failed to get recipient balance: %v\n", err)
	} else {
		fmt.Printf("Recipient %s -> balance: %s, nonce: %d\n", recipientAddress, rb, rn)
	}
}
