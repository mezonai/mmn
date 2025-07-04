package network

import (
	"context"
	"crypto/ed25519"
	"log"
	"mmn/blockstore"
	"mmn/consensus"
	"mmn/ledger"
	pb "mmn/proto"
	"net"

	"google.golang.org/grpc"
)

type server struct {
	pb.UnimplementedBlockServiceServer
	pubKeys       map[string]ed25519.PublicKey
	blockDir      string
	ledger        *ledger.Ledger
	voteCollector *consensus.Collector
	grpcClient    *GRPCClient // để vote ngược lại
	selfID        string
	privKey       ed25519.PrivateKey
	blockStore    *blockstore.BlockStore
}

func NewGRPCServer(addr string, pubKeys map[string]ed25519.PublicKey, blockDir string,
	ld *ledger.Ledger, collector *consensus.Collector,
	grpcClient *GRPCClient, selfID string, priv ed25519.PrivateKey, blockStore *blockstore.BlockStore) *grpc.Server {

	s := &server{
		pubKeys:       pubKeys,
		blockDir:      blockDir,
		ledger:        ld,
		voteCollector: collector,
		grpcClient:    grpcClient,
		selfID:        selfID,
		privKey:       priv,
		blockStore:    blockStore,
	}
	grpcSrv := grpc.NewServer()
	pb.RegisterBlockServiceServer(grpcSrv, s)
	lis, _ := net.Listen("tcp", addr)
	go grpcSrv.Serve(lis)
	log.Printf("[gRPC] server listening on %s", addr)
	return grpcSrv
}

func (s *server) Broadcast(ctx context.Context, pbBlk *pb.Block) (*pb.BroadcastResponse, error) {
	// Convert pb.Block → block.Block
	blk, err := FromProtoBlock(pbBlk)
	if err != nil {
		log.Printf("[follower] Block conversion error: %v", err)
		return &pb.BroadcastResponse{Ok: false, Error: "invalid block"}, nil
	}
	pubKey, ok := s.pubKeys[blk.LeaderID]
	if !ok {
		log.Printf("[follower] Unknown leader: %s", blk.LeaderID)
		return &pb.BroadcastResponse{Ok: false, Error: "unknown leader"}, nil
	}
	// Verify signature
	if !blk.VerifySignature(pubKey) {
		log.Printf("[follower] Invalid signature for slot %d", blk.Slot)
		return &pb.BroadcastResponse{Ok: false, Error: "bad signature"}, nil
	}
	// Persist block
	if err := s.blockStore.AddBlock(blk); err != nil {
		log.Printf("[follower] Store block error: %v", err)
	} else {
		log.Printf("[follower] Stored block slot=%d", blk.Slot)
	}
	// Replay transactions in block
	if err := s.ledger.ApplyBlock(blk); err != nil {
		log.Printf("[follower] Apply block error: %v", err)
		return &pb.BroadcastResponse{Ok: false, Error: "block apply failed"}, nil
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
	committed, err := s.voteCollector.AddVote(&v)
	if err != nil {
		return &pb.VoteResponse{Ok: false, Error: err.Error()}, nil
	}
	if committed {
		log.Printf("[consensus] slot %d finalized! votes=%d", v.Slot, len(s.voteCollector.VotesForSlot(v.Slot)))
		// TODO: trigger commit: e.g. apply any pending actions for slot v.Slot
	}

	return &pb.VoteResponse{Ok: true}, nil
}
