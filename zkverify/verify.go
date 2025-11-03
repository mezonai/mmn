package zkverify

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/mezonai/mmn/logx"
)

const (
	CACHE_EXPIRY_DURATION = 30 * time.Minute
	CLEANER_INTERVAL      = 10 * time.Minute
)

type CacheEntry struct {
	value    bool
	expireAt time.Time
}

type ZkVerify struct {
	vk    groth16.VerifyingKey
	cache sync.Map // map[string]CacheEntry
}

func NewZkVerify(keyPath string) *ZkVerify {
	vkB64, err := os.ReadFile(keyPath)
	if err != nil {
		logx.Error("ZkVerify", fmt.Sprintf("Failed to read zk verifying key: %v", err))
		return nil
	}
	vkBytes, err := base64.StdEncoding.DecodeString(string(vkB64))
	if err != nil {
		logx.Error("ZkVerify", fmt.Sprintf("Failed to decode zk verifying key: %v", err))
		return nil
	}
	vk := groth16.NewVerifyingKey(ecc.BN254)
	if _, err := vk.ReadFrom(bytes.NewReader(vkBytes)); err != nil {
		logx.Error("ZkVerify", fmt.Sprintf("Failed to read set verifying key: %v", err))
		return nil
	}

	zv := &ZkVerify{vk: vk}
	go zv.cleaner()
	return zv
}

func (v *ZkVerify) cleaner() {
	for {
		time.Sleep(CLEANER_INTERVAL)
		now := time.Now()
		v.cache.Range(func(key, value interface{}) bool {
			entry := value.(CacheEntry)
			if now.After(entry.expireAt) {
				v.cache.Delete(key)
			}
			return true
		})
	}
}

func makeCacheKey(sender, pubKey, proofB64, pubB64 string) string {
	h := sha256.New()
	h.Write([]byte(sender))
	h.Write([]byte(pubKey))
	h.Write([]byte(proofB64))
	h.Write([]byte(pubB64))
	return hex.EncodeToString(h.Sum(nil))
}

func (v *ZkVerify) Verify(sender, pubKey, proofB64, pubB64 string) bool {
	cacheKey := makeCacheKey(sender, pubKey, proofB64, pubB64)

	if val, ok := v.cache.Load(cacheKey); ok {
		entry := val.(CacheEntry)
		if time.Now().Before(entry.expireAt) {
			return entry.value
		}
		v.cache.Delete(cacheKey)
	}

	result := v.verifyInternal(sender, pubKey, proofB64, pubB64)

	v.cache.Store(cacheKey, CacheEntry{
		value:    result,
		expireAt: time.Now().Add(CACHE_EXPIRY_DURATION),
	})

	return result
}

func (v *ZkVerify) verifyInternal(sender, pubKey, proofB64, pubB64 string) bool {
	proofBytes, err := base64.StdEncoding.DecodeString(proofB64)
	if err != nil {
		logx.Error("ZkVerify", fmt.Sprintf("Failed to decode proof: %v", err))
		return false
	}
	proof := groth16.NewProof(ecc.BN254)
	if _, err := proof.ReadFrom(bytes.NewReader(proofBytes)); err != nil {
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
