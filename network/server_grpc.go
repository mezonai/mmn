package network

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"mmn/blockstore"
	"mmn/consensus"
	"mmn/ledger"
	"mmn/mempool"
	pb "mmn/proto"
	"mmn/utils"
	"mmn/validator"
	"net"

	"google.golang.org/grpc"
)

type server struct {
	pb.UnimplementedBlockServiceServer
	pb.UnimplementedVoteServiceServer
	pb.UnimplementedTxServiceServer
	pb.UnimplementedAccountServiceServer
	pubKeys       map[string]ed25519.PublicKey
	blockDir      string
	ledger        *ledger.Ledger
	voteCollector *consensus.Collector
	grpcClient    *GRPCClient // to vote back
	selfID        string
	privKey       ed25519.PrivateKey
	validator     *validator.Validator
	blockStore    *blockstore.BlockStore
	mempool       *mempool.Mempool
}

func NewGRPCServer(addr string, pubKeys map[string]ed25519.PublicKey, blockDir string,
	ld *ledger.Ledger, collector *consensus.Collector,
	grpcClient *GRPCClient, selfID string, priv ed25519.PrivateKey, validator *validator.Validator, blockStore *blockstore.BlockStore, mempool *mempool.Mempool) *grpc.Server {

	s := &server{
		pubKeys:       pubKeys,
		blockDir:      blockDir,
		ledger:        ld,
		voteCollector: collector,
		grpcClient:    grpcClient,
		selfID:        selfID,
		privKey:       priv,
		blockStore:    blockStore,
		validator:     validator,
		mempool:       mempool,
	}
	grpcSrv := grpc.NewServer()
	pb.RegisterBlockServiceServer(grpcSrv, s)
	pb.RegisterVoteServiceServer(grpcSrv, s)
	pb.RegisterTxServiceServer(grpcSrv, s)
	pb.RegisterAccountServiceServer(grpcSrv, s)
	lis, _ := net.Listen("tcp", addr)
	go grpcSrv.Serve(lis)
	fmt.Printf("[gRPC] server listening on %s", addr)
	return grpcSrv
}

func (s *server) Broadcast(ctx context.Context, pbBlk *pb.Block) (*pb.BroadcastResponse, error) {
	// Convert pb.Block → block.Block
	blk, err := utils.FromProtoBlock(pbBlk)
	if err != nil {
		fmt.Printf("[follower] Block conversion error: %v", err)
		return &pb.BroadcastResponse{Ok: false, Error: "invalid block"}, nil
	}
	fmt.Printf("VerifySignature: leader ID: %s\n", blk.LeaderID)
	pubKey, ok := s.pubKeys[blk.LeaderID]
	if !ok {
		fmt.Printf("[follower] Unknown leader: %s", blk.LeaderID)
		return &pb.BroadcastResponse{Ok: false, Error: "unknown leader"}, nil
	}
	// Verify signature
	fmt.Printf("VerifySignature: verifying signature for block %x\n", blk.Hash)
	if !blk.VerifySignature(pubKey) {
		fmt.Printf("[follower] Invalid signature for slot %d", blk.Slot)
		return &pb.BroadcastResponse{Ok: false, Error: "bad signature"}, nil
	}

	// Verify PoH
	fmt.Printf("VerifyPoH: verifying PoH for block %x\n", blk.Hash)
	if err := blk.VerifyPoH(); err != nil {
		fmt.Printf("[follower] Invalid PoH: %v", err)
		return &pb.BroadcastResponse{Ok: false, Error: "invalid PoH"}, nil
	}

	// Verify block is valid
	fmt.Printf("VerifyBlock: verifying block %x\n", blk.Hash)
	if err := s.ledger.VerifyBlock(blk); err != nil {
		fmt.Printf("[follower] Invalid block: %v", err)
		return &pb.BroadcastResponse{Ok: false, Error: "invalid block"}, nil
	}

	// Persist block
	if err := s.blockStore.AddBlockPending(blk); err != nil {
		fmt.Printf("[follower] Store block error: %v", err)
	} else {
		fmt.Printf("[follower] Stored block slot=%d", blk.Slot)
	}

	// Reseed PoH for follower
	if s.validator.IsFollower(blk.Slot) {
		if blk.Slot > 0 && !s.blockStore.HasCompleteBlock(blk.Slot-1) {
			fmt.Printf("Skip reseed slot %d - missing ancestor", blk.Slot)
		} else {
			fmt.Printf("Reseed at slot %d", blk.Slot)
			seed := blk.LastEntryHash()
			s.validator.Recorder.ReseedAtSlot(seed, blk.Slot)
		}
	}

	// Broadcast vote
	vote := &consensus.Vote{
		Slot:      blk.Slot,
		BlockHash: blk.Hash,
		VoterID:   s.selfID,
	}
	vote.Sign(s.privKey)

	// Add vote to collector for follower self-vote
	fmt.Printf("[follower] Adding vote %d to collector for self-vote\n", vote.Slot)
	if committed, needApply, err := s.voteCollector.AddVote(vote); err != nil {
		fmt.Printf("[follower] Add vote error: %v", err)
		return &pb.BroadcastResponse{Ok: false, Error: "add vote failed"}, nil
	} else if committed && needApply {
		// Replay transactions in block
		if err := s.ledger.ApplyBlock(s.blockStore.Block(vote.Slot)); err != nil {
			fmt.Printf("[follower] Apply block error: %v", err)
			return &pb.BroadcastResponse{Ok: false, Error: "block apply failed"}, nil
		}
		// Mark block as finalized
		if err := s.blockStore.MarkFinalized(vote.Slot); err != nil {
			fmt.Printf("[follower] Mark block as finalized error: %v", err)
			return &pb.BroadcastResponse{Ok: false, Error: "mark block as finalized failed"}, nil
		}
		fmt.Printf("[follower] slot %d finalized! votes=%d", vote.Slot, len(s.voteCollector.VotesForSlot(vote.Slot)))
	}

	if err := s.grpcClient.BroadcastVote(context.Background(), vote); err != nil {
		fmt.Printf("[follower] Broadcast vote error: %v", err)
		return &pb.BroadcastResponse{Ok: false, Error: "broadcast vote failed"}, nil
	}

	return &pb.BroadcastResponse{Ok: true}, nil
}

// Vote RPC handler
func (s *server) Vote(ctx context.Context, in *pb.VoteRequest) (*pb.VoteResponse, error) {
	// 1. Convert pb.Vote → consensus.Vote
	var v consensus.Vote
	v.Slot = in.Slot
	copy(v.BlockHash[:], in.BlockHash)
	v.VoterID = in.VoterId
	v.Signature = in.Signature

	// 2. Verify signature
	pub, ok := s.pubKeys[v.VoterID]
	if !ok {
		return &pb.VoteResponse{Ok: false, Error: "unknown voter"}, nil
	}
	if !v.VerifySignature(pub) {
		return &pb.VoteResponse{Ok: false, Error: "invalid signature"}, nil
	}

	// 3. Add to collector
	fmt.Printf("[consensus] Adding vote %d to collector for peer vote\n", v.Slot)
	committed, needApply, err := s.voteCollector.AddVote(&v)
	if err != nil {
		fmt.Printf("[consensus] Add vote error: %v\n", err)
		return &pb.VoteResponse{Ok: false, Error: err.Error()}, nil
	}
	if committed && needApply {
		fmt.Printf("[consensus] slot %d committed, processing apply block! votes=%d\n", v.Slot, len(s.voteCollector.VotesForSlot(v.Slot)))
		// Replay transactions in block
		if err := s.ledger.ApplyBlock(s.blockStore.Block(v.Slot)); err != nil {
			fmt.Printf("[consensus] Apply block error: %v\n", err)
			return &pb.VoteResponse{Ok: false, Error: "block apply failed"}, nil
		}
		// Mark block as finalized
		if err := s.blockStore.MarkFinalized(v.Slot); err != nil {
			fmt.Printf("[consensus] Mark block as finalized error: %v\n", err)
			return &pb.VoteResponse{Ok: false, Error: "mark block as finalized failed"}, nil
		}
		fmt.Printf("[consensus] slot %d finalized!\n", v.Slot)
	}

	return &pb.VoteResponse{Ok: true}, nil
}

func (s *server) TxBroadcast(ctx context.Context, in *pb.SignedTxMsg) (*pb.TxResponse, error) {
	fmt.Printf("[gRPC] received tx %+v\n", in.TxMsg)
	tx, err := utils.FromProtoSignedTx(in)
	if err != nil {
		return &pb.TxResponse{Ok: false, Error: "invalid tx"}, nil
	}
	s.mempool.AddTx(tx, false)
	return &pb.TxResponse{Ok: true}, nil
}

func (s *server) AddTx(ctx context.Context, in *pb.SignedTxMsg) (*pb.AddTxResponse, error) {
	fmt.Printf("[gRPC] received tx %+v\n", in.TxMsg)
	tx, err := utils.FromProtoSignedTx(in)
	if err != nil {
		return &pb.AddTxResponse{Ok: false, Error: "invalid tx"}, nil
	}
	txHash, ok := s.mempool.AddTx(tx, true)
	if !ok {
		return &pb.AddTxResponse{Ok: false, Error: "mempool full"}, nil
	}
	return &pb.AddTxResponse{Ok: true, TxHash: txHash}, nil
}

func (s *server) GetAccount(ctx context.Context, in *pb.GetAccountRequest) (*pb.GetAccountResponse, error) {
	addr := in.Address
	acc := s.ledger.GetAccount(addr)
	return &pb.GetAccountResponse{
		Address: addr,
		Balance: acc.Balance,
		Nonce:   acc.Nonce,
	}, nil
}

func (s *server) GetTxHistory(ctx context.Context, in *pb.GetTxHistoryRequest) (*pb.GetTxHistoryResponse, error) {
	addr := in.Address
	total, txs := s.ledger.GetTxs(addr, in.Limit, in.Offset, in.Filter)
	txMetas := make([]*pb.TxMeta, len(txs))
	for i, tx := range txs {
		txMetas[i] = &pb.TxMeta{
			Sender:    tx.Sender,
			Recipient: tx.Recipient,
			Amount:    tx.Amount,
			Nonce:     tx.Nonce,
			Timestamp: tx.Timestamp,
			Status:    pb.TxMeta_CONFIRMED,
		}
	}
	return &pb.GetTxHistoryResponse{
		Total: total,
		Txs:   txMetas,
	}, nil
}
