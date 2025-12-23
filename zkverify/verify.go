package zkverify

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/allegro/bigcache"
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/mezonai/mmn/logx"
)

type ZkVerify struct {
	vk      groth16.VerifyingKey
	zkCache *bigcache.BigCache
}

func NewZkVerify(keyPath string) (*ZkVerify, error) {
	vkB64, err := os.ReadFile(keyPath)
	if err != nil {
		logx.Error("ZkVerify", fmt.Sprintf("Failed to read zk verifying key: %v", err))
		return nil, err
	}
	vkBytes, err := base64.StdEncoding.DecodeString(string(vkB64))
	if err != nil {
		logx.Error("ZkVerify", fmt.Sprintf("Failed to decode zk verifying key: %v", err))
		return nil, err
	}
	vk := groth16.NewVerifyingKey(ecc.BN254)
	if _, err := vk.ReadFrom(bytes.NewReader(vkBytes)); err != nil {
		logx.Error("ZkVerify", fmt.Sprintf("Failed to read set verifying key: %v", err))
		return nil, err
	}

	cacheConfig := bigcache.Config{
		Shards:           128,
		LifeWindow:       30 * time.Minute,
		CleanWindow:      10 * time.Minute,
		MaxEntrySize:     64,
		Verbose:          false,
		HardMaxCacheSize: 0,
	}
	zkCache, err := bigcache.NewBigCache(cacheConfig)
	if err != nil {
		logx.Error("ZkVerify", fmt.Sprintf("Failed to create bigcache: %v", err))
		return nil, err
	}

	zv := &ZkVerify{
		vk:      vk,
		zkCache: zkCache,
	}

	return zv, nil
}

func makeCacheKey(sender, pubKey, proofB64, pubB64 string) string {
	return sender + "|" + pubKey + "|" + proofB64 + "|" + pubB64
}

func (v *ZkVerify) Verify(sender, pubKey, proofB64, pubB64 string) bool {
	cacheKey := makeCacheKey(sender, pubKey, proofB64, pubB64)

	cacheBytes, err := v.zkCache.Get(cacheKey)
	if err == nil {
		var cacheResult bool
		if err := json.Unmarshal(cacheBytes, &cacheResult); err == nil {
			return cacheResult
		}
	}

	result := v.verifyInternal(sender, pubKey, proofB64, pubB64)

	resultBytes, _ := json.Marshal(result)
	err = v.zkCache.Set(cacheKey, resultBytes)
	if err != nil {
		logx.Error("ZkVerify", fmt.Sprintf("Failed to set cache: %v", err))
	}

	return result
}

func (v *ZkVerify) verifyInternal(sender, pubKey, proofB64, pubB64 string) bool {
	proofBytes, err := base64.StdEncoding.DecodeString(proofB64)
	if err != nil {
		logx.Error("ZkVerify", fmt.Sprintf("Failed to decode proof: %v", err))
		return false
	}
	proof := groth16.NewProof(ecc.BN254)
	_, err = proof.ReadFrom(bytes.NewReader(proofBytes))
	if err != nil {
		logx.Error("ZkVerify", fmt.Sprintf("Failed to read proof: %v", err))
		return false
	}

	pwBytes, err := base64.StdEncoding.DecodeString(pubB64)
	if err != nil {
		logx.Error("ZkVerify", fmt.Sprintf("Failed to decode public: %v", err))
		return false
	}
	pw, err := frontend.NewWitness(nil, ecc.BN254.ScalarField())
	if err != nil {
		logx.Error("ZkVerify", fmt.Sprintf("Failed to create public: %v", err))
		return false
	}
	if _, err := pw.ReadFrom(bytes.NewReader(pwBytes)); err != nil {
		logx.Error("ZkVerify", fmt.Sprintf("Failed to read public: %v", err))
		return false
	}

	pubVector := pw.Vector()
	var pubString = fmt.Sprintf("%v", pubVector)
	pubStringTrimmed := strings.TrimSpace(strings.Trim(pubString, "[]"))
	var pubStringArray = []string{}
	if pubStringTrimmed != "" {
		pubStringArray = strings.Split(pubStringTrimmed, ",")
		for i := range pubStringArray {
			pubStringArray[i] = strings.TrimSpace(pubStringArray[i])
		}
	}

	if len(pubStringArray) != 4 {
		logx.Error("ZkVerify", "Public input length invalid")
		return false
	}

	if pubStringArray[3] != stringToBigIntBN254FromBytes(sender).String() {
		logx.Error("ZkVerify", "Sender invalid")
		return false
	}
	if pubStringArray[2] != stringToBigIntBN254FromBytes(pubKey).String() {
		logx.Error("ZkVerify", "Key invalid")
		return false
	}

	if err := groth16.Verify(proof, v.vk, pw); err != nil {
		logx.Error("ZkVerify", fmt.Sprintf("Failed to verify: %v", err))
		return false
	} else {
		logx.Debug("ZkVerify", "Verify success")
		return true
	}
}

func stringToBigIntBN254FromBytes(s string) *big.Int {
	scalarField := ecc.BN254.ScalarField()
	value := new(big.Int).SetBytes([]byte(s))
	return value.Mod(value, scalarField)
}
