package staking

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/mezonai/mmn/logx"
)

// StakingAPI provides HTTP API endpoints for staking operations
type StakingAPI struct {
	stakeManager *StakeManager
	router       *mux.Router
}

// NewStakingAPI creates a new staking API
func NewStakingAPI(stakeManager *StakeManager) *StakingAPI {
	api := &StakingAPI{
		stakeManager: stakeManager,
		router:       mux.NewRouter(),
	}
	api.setupRoutes()
	return api
}

// setupRoutes configures API routes
func (api *StakingAPI) setupRoutes() {
	// Validator info endpoints
	api.router.HandleFunc("/validators", api.getValidators).Methods("GET")
	api.router.HandleFunc("/validators/{pubkey}", api.getValidator).Methods("GET")
	api.router.HandleFunc("/validators/active", api.getActiveValidators).Methods("GET")

	// Staking info endpoints
	api.router.HandleFunc("/stake/pool", api.getStakePool).Methods("GET")
	api.router.HandleFunc("/stake/distribution", api.getStakeDistribution).Methods("GET")
	api.router.HandleFunc("/stake/epoch", api.getEpochInfo).Methods("GET")

	// Leader schedule endpoints
	api.router.HandleFunc("/schedule/current", api.getCurrentSchedule).Methods("GET")
	api.router.HandleFunc("/schedule/slot/{slot}", api.getLeaderForSlot).Methods("GET")

	// Transaction endpoints
	api.router.HandleFunc("/stake/delegate", api.delegate).Methods("POST")
	api.router.HandleFunc("/stake/undelegate", api.undelegate).Methods("POST")
}

// GetRouter returns the configured router
func (api *StakingAPI) GetRouter() *mux.Router {
	return api.router
}

// Validator endpoints

func (api *StakingAPI) getValidators(w http.ResponseWriter, r *http.Request) {
	validators := api.stakeManager.stakePool.validators

	response := make(map[string]interface{})
	for pubkey, validator := range validators {
		response[pubkey] = map[string]interface{}{
			"pubkey":          validator.Pubkey,
			"stake_amount":    validator.StakeAmount.String(),
			"state":           validator.State,
			"activation_slot": validator.ActivationSlot,
			"created_at":      validator.CreatedAt,
			"delegator_count": len(validator.Delegators),
		}
	}

	api.writeJSON(w, response)
}

func (api *StakingAPI) getValidator(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	pubkey := vars["pubkey"]

	validator, exists := api.stakeManager.GetValidatorInfo(pubkey)
	if !exists {
		http.Error(w, "Validator not found", http.StatusNotFound)
		return
	}

	response := map[string]interface{}{
		"pubkey":           validator.Pubkey,
		"stake_amount":     validator.StakeAmount.String(),
		"state":            validator.State,
		"activation_slot":  validator.ActivationSlot,
		"created_at":       validator.CreatedAt,
		"last_reward_slot": validator.LastRewardSlot,
		"delegators":       validator.Delegators,
	}

	api.writeJSON(w, response)
}

func (api *StakingAPI) getActiveValidators(w http.ResponseWriter, r *http.Request) {
	activeValidators := api.stakeManager.GetActiveValidators()

	response := make(map[string]interface{})
	for pubkey, validator := range activeValidators {
		response[pubkey] = map[string]interface{}{
			"pubkey":          validator.Pubkey,
			"stake_amount":    validator.StakeAmount.String(),
			"state":           validator.State,
			"delegator_count": len(validator.Delegators),
		}
	}

	api.writeJSON(w, response)
}

// Staking info endpoints

func (api *StakingAPI) getStakePool(w http.ResponseWriter, r *http.Request) {
	stats := api.stakeManager.GetStakePoolStats()
	api.writeJSON(w, stats)
}

func (api *StakingAPI) getStakeDistribution(w http.ResponseWriter, r *http.Request) {
	distribution := api.stakeManager.GetStakeDistribution()
	api.writeJSON(w, distribution)
}

func (api *StakingAPI) getEpochInfo(w http.ResponseWriter, r *http.Request) {
	epochInfo := api.stakeManager.GetEpochInfo()
	api.writeJSON(w, epochInfo)
}

// Leader schedule endpoints

func (api *StakingAPI) getCurrentSchedule(w http.ResponseWriter, r *http.Request) {
	schedule := api.stakeManager.GetCurrentLeaderSchedule()
	if schedule == nil {
		http.Error(w, "No current schedule available", http.StatusNotFound)
		return
	}

	// Convert schedule to readable format
	epochInfo := api.stakeManager.GetEpochInfo()
	currentEpoch := epochInfo["current_epoch"].(uint64)
	startSlot := epochInfo["epoch_start_slot"].(uint64)
	endSlot := epochInfo["epoch_end_slot"].(uint64)

	response := map[string]interface{}{
		"epoch":      currentEpoch,
		"start_slot": startSlot,
		"end_slot":   endSlot,
		"schedule":   api.formatScheduleForSlotRange(schedule, startSlot, endSlot),
	}

	api.writeJSON(w, response)
}

func (api *StakingAPI) getLeaderForSlot(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	slotStr := vars["slot"]

	slot, err := strconv.ParseUint(slotStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid slot number", http.StatusBadRequest)
		return
	}

	schedule := api.stakeManager.GetCurrentLeaderSchedule()
	if schedule == nil {
		http.Error(w, "No current schedule available", http.StatusNotFound)
		return
	}

	leader, exists := schedule.LeaderAt(slot)
	if !exists {
		http.Error(w, "No leader assigned for slot", http.StatusNotFound)
		return
	}

	response := map[string]interface{}{
		"slot":   slot,
		"leader": leader,
	}

	api.writeJSON(w, response)
}

// Transaction endpoints

func (api *StakingAPI) delegate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DelegatorPubkey string `json:"delegator_pubkey"`
		ValidatorPubkey string `json:"validator_pubkey"`
		Amount          string `json:"amount"`
		Signature       string `json:"signature"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// In a real implementation, this would:
	// 1. Validate the signature
	// 2. Create a delegation transaction
	// 3. Submit to mempool
	// 4. Return transaction hash

	response := map[string]interface{}{
		"status":  "submitted",
		"message": "Delegation transaction submitted to mempool",
		"tx_hash": "placeholder_hash",
	}

	api.writeJSON(w, response)
}

func (api *StakingAPI) undelegate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DelegatorPubkey string `json:"delegator_pubkey"`
		ValidatorPubkey string `json:"validator_pubkey"`
		Amount          string `json:"amount"`
		Signature       string `json:"signature"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// In a real implementation, this would:
	// 1. Validate the signature
	// 2. Create an undelegation transaction
	// 3. Submit to mempool
	// 4. Return transaction hash

	response := map[string]interface{}{
		"status":  "submitted",
		"message": "Undelegation transaction submitted to mempool",
		"tx_hash": "placeholder_hash",
	}

	api.writeJSON(w, response)
}

// Helper methods

func (api *StakingAPI) writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		logx.Error("STAKING_API", "Failed to encode JSON response:", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (api *StakingAPI) formatScheduleForSlotRange(schedule StakingSchedule, startSlot, endSlot uint64) []map[string]interface{} {
	var result []map[string]interface{}

	// Sample first 100 slots to avoid huge responses
	maxSlots := uint64(100)
	actualEndSlot := endSlot
	if endSlot-startSlot > maxSlots {
		actualEndSlot = startSlot + maxSlots
	}

	currentLeader := ""
	slotStart := startSlot

	for slot := startSlot; slot <= actualEndSlot; slot++ {
		leader, exists := schedule.LeaderAt(slot)
		if !exists {
			continue
		}

		if leader != currentLeader {
			// End previous range
			if currentLeader != "" {
				result = append(result, map[string]interface{}{
					"start_slot": slotStart,
					"end_slot":   slot - 1,
					"leader":     currentLeader,
				})
			}
			// Start new range
			currentLeader = leader
			slotStart = slot
		}
	}

	// Add final range
	if currentLeader != "" {
		result = append(result, map[string]interface{}{
			"start_slot": slotStart,
			"end_slot":   actualEndSlot,
			"leader":     currentLeader,
		})
	}

	return result
}

// StakingSchedule interface to avoid import cycle
type StakingSchedule interface {
	LeaderAt(slot uint64) (string, bool)
}
