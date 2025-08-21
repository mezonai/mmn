package network

import (
	"context"
	"fmt"
	"math/big"

	pb "github.com/mezonai/mmn/proto"
	"github.com/mezonai/mmn/staking"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// DynamicVoteServer implements the gRPC dynamic voting service
type DynamicVoteServer struct {
	pb.UnimplementedDynamicVoteServiceServer
	stakeValidator *staking.StakeValidator
}

// NewDynamicVoteServer creates a new dynamic vote server
func NewDynamicVoteServer(stakeValidator *staking.StakeValidator) *DynamicVoteServer {
	return &DynamicVoteServer{
		stakeValidator: stakeValidator,
	}
}

// StartVotingRound starts a new voting round
func (dvs *DynamicVoteServer) StartVotingRound(ctx context.Context, req *pb.StartVotingRoundRequest) (*pb.StartVotingRoundResponse, error) {
	// Parse required stake
	requiredStake, ok := new(big.Int).SetString(req.RequiredStake, 10)
	if !ok {
		return &pb.StartVotingRoundResponse{
			Success: false,
			Message: "Invalid required stake format",
		}, nil
	}

	// Convert vote type
	var voteType staking.VoteType
	switch req.VoteType {
	case pb.VoteType_VOTE_TYPE_BLOCK:
		voteType = staking.VoteTypeBlock
	case pb.VoteType_VOTE_TYPE_EPOCH:
		voteType = staking.VoteTypeEpoch
	case pb.VoteType_VOTE_TYPE_SLASHING:
		voteType = staking.VoteTypeSlashing
	case pb.VoteType_VOTE_TYPE_GOVERNANCE:
		voteType = staking.VoteTypeGovernance
	default:
		return &pb.StartVotingRoundResponse{
			Success: false,
			Message: "Invalid vote type",
		}, nil
	}

	// Start the voting round using the validator's vote manager
	err := dvs.stakeValidator.GetVoteManager().StartVotingRound(req.RoundId, voteType, req.Target, requiredStake)
	if err != nil {
		return &pb.StartVotingRoundResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	return &pb.StartVotingRoundResponse{
		Success: true,
		Message: fmt.Sprintf("Voting round %s started successfully", req.RoundId),
	}, nil
}

// CastVote allows a validator to cast a vote
func (dvs *DynamicVoteServer) CastVote(ctx context.Context, req *pb.CastVoteRequest) (*pb.CastVoteResponse, error) {
	err := dvs.stakeValidator.CastVote(req.RoundId, req.Support)
	if err != nil {
		return &pb.CastVoteResponse{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	return &pb.CastVoteResponse{
		Success: true,
		Message: "Vote cast successfully",
	}, nil
}

// GetVotingRoundStatus returns the status of a voting round
func (dvs *DynamicVoteServer) GetVotingRoundStatus(ctx context.Context, req *pb.GetVotingRoundStatusRequest) (*pb.GetVotingRoundStatusResponse, error) {
	round, exists := dvs.stakeValidator.GetVoteStatus(req.RoundId)
	if !exists {
		return &pb.GetVotingRoundStatusResponse{
			Exists: false,
		}, nil
	}

	pbRound := convertVotingRoundToProto(round)
	return &pb.GetVotingRoundStatusResponse{
		Exists: true,
		Round:  pbRound,
	}, nil
}

// GetActiveVotingRounds returns all active voting rounds
func (dvs *DynamicVoteServer) GetActiveVotingRounds(ctx context.Context, req *pb.GetActiveVotingRoundsRequest) (*pb.GetActiveVotingRoundsResponse, error) {
	activeRounds := dvs.stakeValidator.GetActiveVotes()

	pbRounds := make([]*pb.VotingRound, 0, len(activeRounds))
	for _, round := range activeRounds {
		pbRounds = append(pbRounds, convertVotingRoundToProto(round))
	}

	return &pb.GetActiveVotingRoundsResponse{
		Rounds: pbRounds,
	}, nil
}

// Helper function to convert staking.VotingRound to pb.VotingRound
func convertVotingRoundToProto(round *staking.VotingRound) *pb.VotingRound {
	// Calculate statistics
	supportStake := big.NewInt(0)
	opposeStake := big.NewInt(0)
	totalVoted := big.NewInt(0)

	votes := make([]*pb.Vote, 0, len(round.Votes))
	for _, vote := range round.Votes {
		totalVoted.Add(totalVoted, vote.StakeWeight)
		if vote.Support {
			supportStake.Add(supportStake, vote.StakeWeight)
		} else {
			opposeStake.Add(opposeStake, vote.StakeWeight)
		}

		// Convert vote type
		var pbVoteType pb.VoteType
		switch vote.VoteType {
		case staking.VoteTypeBlock:
			pbVoteType = pb.VoteType_VOTE_TYPE_BLOCK
		case staking.VoteTypeEpoch:
			pbVoteType = pb.VoteType_VOTE_TYPE_EPOCH
		case staking.VoteTypeSlashing:
			pbVoteType = pb.VoteType_VOTE_TYPE_SLASHING
		case staking.VoteTypeGovernance:
			pbVoteType = pb.VoteType_VOTE_TYPE_GOVERNANCE
		}

		pbVote := &pb.Vote{
			VoterPubKey:   vote.VoterPubKey,
			VoteType:      pbVoteType,
			Target:        vote.Target,
			Support:       vote.Support,
			StakeWeight:   vote.StakeWeight.String(),
			Timestamp:     timestamppb.New(vote.Timestamp),
			VoteSignature: vote.VoteSignature,
		}
		votes = append(votes, pbVote)
	}

	// Convert vote type
	var pbVoteType pb.VoteType
	switch round.VoteType {
	case staking.VoteTypeBlock:
		pbVoteType = pb.VoteType_VOTE_TYPE_BLOCK
	case staking.VoteTypeEpoch:
		pbVoteType = pb.VoteType_VOTE_TYPE_EPOCH
	case staking.VoteTypeSlashing:
		pbVoteType = pb.VoteType_VOTE_TYPE_SLASHING
	case staking.VoteTypeGovernance:
		pbVoteType = pb.VoteType_VOTE_TYPE_GOVERNANCE
	}

	// Convert status
	var pbStatus pb.VotingStatus
	switch round.Status {
	case staking.VotingActive:
		pbStatus = pb.VotingStatus_VOTING_ACTIVE
	case staking.VotingPassed:
		pbStatus = pb.VotingStatus_VOTING_PASSED
	case staking.VotingFailed:
		pbStatus = pb.VotingStatus_VOTING_FAILED
	case staking.VotingExpired:
		pbStatus = pb.VotingStatus_VOTING_EXPIRED
	}

	return &pb.VotingRound{
		RoundId:           round.RoundID,
		VoteType:          pbVoteType,
		Target:            round.Target,
		StartTime:         timestamppb.New(round.StartTime),
		EndTime:           timestamppb.New(round.EndTime),
		Votes:             votes,
		RequiredStake:     round.RequiredStake.String(),
		Status:            pbStatus,
		TotalSupportStake: supportStake.String(),
		TotalOpposeStake:  opposeStake.String(),
		TotalVotedStake:   totalVoted.String(),
		VoterCount:        int32(len(round.Votes)),
	}
}
