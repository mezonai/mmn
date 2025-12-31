package mempool

import (
	"testing"
)

// Test 1: Add hashes and check if they are marked as duplicates
func TestDedupService_AddAndCheck(t *testing.T) {
	ds, err := NewDedupService(nil, nil)
	if err != nil {
		t.Fatalf("NewDedupService failed: %v", err)
	}

	// Add hashes
	txDedupHashes := []string{"hash1", "hash2", "hash3"}
	ds.Add(txDedupHashes)

	// Verify all hashes were added
	for _, hash := range txDedupHashes {
		isDup, err := ds.IsDuplicate(hash)
		if err != nil {
			t.Fatalf("IsDuplicate failed: %v", err)
		}
		if !isDup {
			t.Fatalf("Hash %s should be duplicate", hash)
		}
	}
}

// Test 2: Check duplicate - hash exists in cache
func TestDedupService_CheckDuplicate(t *testing.T) {
	ds, err := NewDedupService(nil, nil)
	if err != nil {
		t.Fatalf("NewDedupService failed: %v", err)
	}

	testHash := "existing_hash_123"
	ds.Add([]string{testHash})

	// Check hash exists
	isDup, err := ds.IsDuplicate(testHash)
	if err != nil {
		t.Fatalf("IsDuplicate failed: %v", err)
	}
	if !isDup {
		t.Fatal("Hash should be duplicate")
	}
}

// Test 3: Check not duplicate - hash does not exist in cache
func TestDedupService_CheckNotDuplicate(t *testing.T) {
	ds, err := NewDedupService(nil, nil)
	if err != nil {
		t.Fatalf("NewDedupService failed: %v", err)
	}

	// Check hash does not exist
	isDup, err := ds.IsDuplicate("nonexistent_hash")
	if err != nil {
		t.Fatalf("IsDuplicate failed: %v", err)
	}
	if isDup {
		t.Fatal("Hash should not be duplicate")
	}
}
