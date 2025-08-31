package snapshot

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"strconv"

	"github.com/mezonai/mmn/db"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/types"
)

// TransferProtocol defines the protocol used for snapshot transfer
type TransferProtocol string

const ()

// TransferStatus defines the status of a transfer
type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "pending"
	TransferStatusActive    TransferStatus = "active"
	TransferStatusComplete  TransferStatus = "complete"
	TransferStatusFailed    TransferStatus = "failed"
	TransferStatusCancelled TransferStatus = "cancelled"
)

// SnapshotTransferRequest represents a request for snapshot transfer
type SnapshotTransferRequest struct {
	PeerID       string `json:"peer_id"`
	Slot         uint64 `json:"slot"`
	ChunkSize    int    `json:"chunk_size"`
	ReceiverPort int    `json:"receiver_port"`
	Token        string `json:"token,omitempty"`
}

// SnapshotChunk represents a chunk of snapshot data for UDP transfer
type SnapshotChunk struct {
	SessionID   string `json:"session_id"`
	ChunkIndex  int    `json:"chunk_index"`
	TotalChunks int    `json:"total_chunks"`
	Data        []byte `json:"data"`
	Checksum    string `json:"checksum"`
}

// SnapshotDownloader handles downloading snapshots when new nodes join the network
type SnapshotDownloader struct {
	provider        db.DatabaseProvider
	snapshotDir     string
	udpConn         *net.UDPConn
	mu              sync.RWMutex
	activeDownloads map[string]*DownloadTask
	stopCh          chan struct{}
}

// DownloadTask represents a snapshot download task
type DownloadTask struct {
	ID             string
	PeerAddr       string
	PeerID         string
	Slot           uint64
	StartTime      time.Time
	Status         TransferStatus
	Progress       float64
	SessionID      string
	TotalChunks    int
	ReceivedChunks int
	Chunks         map[int][]byte
	ChunkSize      int
	RetryCount     int
	MaxRetries     int
	mu             sync.RWMutex
}

// NewSnapshotDownloader creates a new snapshot downloader
func NewSnapshotDownloader(provider db.DatabaseProvider, snapshotDir string) *SnapshotDownloader {
	return &SnapshotDownloader{
		provider:        provider,
		snapshotDir:     snapshotDir,
		activeDownloads: make(map[string]*DownloadTask),
		stopCh:          make(chan struct{}),
	}
}

// DownloadSnapshotFromPeer downloads a snapshot from a specific peer
func (sd *SnapshotDownloader) DownloadSnapshotFromPeer(ctx context.Context, peerAddr, peerID string, slot uint64, chunkSize int) (*DownloadTask, error) {
	// Create download task
	task := &DownloadTask{
		ID:         generateDownloadTaskID(peerID, slot),
		PeerAddr:   peerAddr,
		PeerID:     peerID,
		Slot:       slot,
		StartTime:  time.Now(),
		Status:     TransferStatusPending,
		Progress:   0.0,
		Chunks:     make(map[int][]byte),
		ChunkSize:  chunkSize,
		MaxRetries: 3,
	}

	// Store task
	sd.mu.Lock()
	sd.activeDownloads[task.ID] = task
	sd.mu.Unlock()

	// Start UDP download only
	go sd.downloadViaUDP(ctx, task)

	return task, nil
}

// downloadViaUDP downloads snapshot using UDP protocol
func (sd *SnapshotDownloader) downloadViaUDP(ctx context.Context, task *DownloadTask) {
	logx.Info("SNAPSHOT DOWNLOAD", "Starting UDP download from peer:", task.PeerID)

	// Step 1: Setup UDP connection
	if err := sd.setupUDPConnection(); err != nil {
		sd.updateTaskStatus(task, TransferStatusFailed)
		logx.Error("SNAPSHOT DOWNLOAD", "Failed to setup UDP connection:", err)
		return
	}

	// Step 2: Request snapshot transfer
	if err := sd.requestSnapshotTransferUDP(task); err != nil {
		sd.updateTaskStatus(task, TransferStatusFailed)
		logx.Error("SNAPSHOT DOWNLOAD", "Failed to request snapshot transfer:", err)
		return
	}

	// Step 3: Receive chunks
	if err := sd.receiveUDPChunks(task); err != nil {
		sd.updateTaskStatus(task, TransferStatusFailed)
		logx.Error("SNAPSHOT DOWNLOAD", "Failed to receive UDP chunks:", err)
		return
	}

	// Step 4: Assemble and verify snapshot
	if err := sd.assembleAndVerifySnapshot(task); err != nil {
		sd.updateTaskStatus(task, TransferStatusFailed)
		logx.Error("SNAPSHOT DOWNLOAD", "Failed to assemble snapshot:", err)
		return
	}

	sd.updateTaskStatus(task, TransferStatusComplete)
	logx.Info("SNAPSHOT DOWNLOAD", "UDP download completed from peer:", task.PeerID)
}

// requestSnapshotTransferUDP requests a snapshot transfer from the peer using UDP control message
func (sd *SnapshotDownloader) requestSnapshotTransferUDP(task *DownloadTask) error {
	// Prepare request including our UDP listening port
	localAddr := sd.udpConn.LocalAddr().(*net.UDPAddr)
	req := SnapshotTransferRequest{
		PeerID:       task.PeerID,
		Slot:         task.Slot,
		ChunkSize:    resolveChunkSize(task.ChunkSize),
		ReceiverPort: localAddr.Port,
		Token:        os.Getenv("SNAPSHOT_TOKEN"),
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	peerUDP, err := net.ResolveUDPAddr("udp", task.PeerAddr)
	if err != nil {
		return fmt.Errorf("failed to resolve peer addr: %w", err)
	}
	_, err = sd.udpConn.WriteToUDP(data, peerUDP)
	if err != nil {
		return fmt.Errorf("failed to send UDP request: %w", err)
	}

	sd.updateTaskStatus(task, TransferStatusActive)
	return nil
}

func resolveChunkSize(def int) int {
	if def > 0 {
		return def
	}
	if v := os.Getenv("SNAPSHOT_CHUNK_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 16 * 1024
}

// receiveUDPChunks receives UDP chunks for the task
func (sd *SnapshotDownloader) receiveUDPChunks(task *DownloadTask) error {
	buffer := make([]byte, 65507) // Max UDP packet size
	timeout := time.After(30 * time.Minute)

	for {
		select {
		case <-timeout:
			return fmt.Errorf("UDP download timeout")
		default:
			// Set read deadline
			sd.udpConn.SetReadDeadline(time.Now().Add(30 * time.Second))

			n, remoteAddr, err := sd.udpConn.ReadFromUDP(buffer)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue // Continue on timeout
				}
				return fmt.Errorf("UDP read error: %w", err)
			}

			var chunk SnapshotChunk
			if err := json.Unmarshal(buffer[:n], &chunk); err != nil {
				logx.Error("SNAPSHOT DOWNLOAD", "Failed to unmarshal chunk:", err)
				continue
			}

			// Verify this chunk belongs to our task
			if task.SessionID == "" {
				task.SessionID = chunk.SessionID
			}
			if chunk.SessionID != task.SessionID {
				continue
			}

			// Verify checksum
			if !sd.verifyChunkChecksum(chunk.Data, chunk.Checksum) {
				logx.Error("SNAPSHOT DOWNLOAD", "Chunk checksum verification failed")
				continue
			}

			// Store chunk
			task.mu.Lock()
			task.Chunks[chunk.ChunkIndex] = chunk.Data
			task.ReceivedChunks++
			if task.TotalChunks == 0 {
				task.TotalChunks = chunk.TotalChunks
			}
			progress := float64(task.ReceivedChunks) / float64(task.TotalChunks) * 100.0
			task.Progress = progress
			task.mu.Unlock()

			// Send acknowledgment
			ack := struct {
				SessionID  string `json:"session_id"`
				ChunkIndex int    `json:"chunk_index"`
				Status     string `json:"status"`
			}{
				SessionID:  chunk.SessionID,
				ChunkIndex: chunk.ChunkIndex,
				Status:     "received",
			}

			ackData, _ := json.Marshal(ack)
			sd.udpConn.WriteToUDP(ackData, remoteAddr)

			// Check if all chunks received
			if task.TotalChunks > 0 && task.ReceivedChunks == task.TotalChunks {
				return nil
			}
		}
	}
}

// assembleAndVerifySnapshot assembles snapshot from chunks and verifies it
func (sd *SnapshotDownloader) assembleAndVerifySnapshot(task *DownloadTask) error {
	// Create snapshot file
	snapshotPath := filepath.Join(sd.snapshotDir, "snapshot-latest.json")
	file, err := os.Create(snapshotPath)
	if err != nil {
		return fmt.Errorf("failed to create snapshot file: %w", err)
	}
	defer file.Close()

	// Write chunks in order
	logx.Info("SNAPSHOT DOWNLOAD", "Assembling snapshot from chunks",
		"total_chunks", task.TotalChunks,
		"received_chunks", len(task.Chunks))
	for i := 0; i < task.TotalChunks; i++ {
		chunk, exists := task.Chunks[i]
		if !exists {
			return fmt.Errorf("missing chunk %d", i)
		}
		logx.Info("SNAPSHOT DOWNLOAD", "Writing chunk", "index", i, "size", len(chunk))
		_, err := file.Write(chunk)
		if err != nil {
			return fmt.Errorf("failed to write chunk %d: %w", i, err)
		}
	}
	logx.Info("SNAPSHOT DOWNLOAD", "All chunks written successfully")

	// Apply and load snapshot
	return sd.applyAndLoadSnapshot(task)
}

// applyAndLoadSnapshot applies and loads a downloaded snapshot
func (sd *SnapshotDownloader) applyAndLoadSnapshot(task *DownloadTask) error {
	snapshotPath := filepath.Join(sd.snapshotDir, "snapshot-latest.json")

	// Read snapshot
	snapshotFile, err := ReadSnapshot(snapshotPath)
	if err != nil {
		return fmt.Errorf("failed to read snapshot: %w", err)
	}

	// Load accounts into database directly without verification
	if err := sd.storeAccountsBatch(snapshotFile.Accounts); err != nil {
		return fmt.Errorf("failed to store accounts: %w", err)
	}

	logx.Info("SNAPSHOT DOWNLOAD", "Snapshot applied and loaded successfully")
	return nil
}

// verifyChunkChecksum verifies chunk checksum
func (sd *SnapshotDownloader) verifyChunkChecksum(data []byte, expectedChecksum string) bool {
	hash := sha256.Sum256(data)
	actualChecksum := fmt.Sprintf("%x", hash[:])
	return actualChecksum == expectedChecksum
}

// updateTaskStatus updates task status
func (sd *SnapshotDownloader) updateTaskStatus(task *DownloadTask, status TransferStatus) {
	task.mu.Lock()
	task.Status = status
	task.mu.Unlock()
}

// generateDownloadTaskID generates a unique download task ID
func generateDownloadTaskID(peerID string, slot uint64) string {
	data := fmt.Sprintf("download-%s-%d-%d", peerID, slot, time.Now().UnixNano())
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", hash[:8])
}

// computeBankHashFromAccounts computes a bank hash deterministically from the provided accounts
func computeBankHashFromAccounts(accounts []types.Account) [32]byte {
	type item struct {
		addr string
		acc  types.Account
	}
	items := make([]item, 0, len(accounts))
	for _, a := range accounts {
		items = append(items, item{addr: a.Address, acc: a})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].addr < items[j].addr })

	h := sha256.New()
	buf := make([]byte, 8)
	for _, it := range items {
		addr := it.addr
		acc := it.acc
		binary.BigEndian.PutUint64(buf, uint64(len(addr)))
		h.Write(buf)
		h.Write([]byte(addr))
		binary.BigEndian.PutUint64(buf, acc.Balance.Uint64())
		binary.BigEndian.PutUint64(buf, acc.Nonce)
		h.Write(buf)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// storeAccountsBatch writes accounts to the underlying DB using a batch for efficiency
func (sd *SnapshotDownloader) storeAccountsBatch(accounts []types.Account) error {
	batch := sd.provider.Batch()
	for _, account := range accounts {
		data, err := json.Marshal(account)
		if err != nil {
			return fmt.Errorf("marshal account %s: %w", account.Address, err)
		}
		key := []byte("account:" + account.Address)
		batch.Put(key, data)
	}
	if err := batch.Write(); err != nil {
		return fmt.Errorf("batch write accounts: %w", err)
	}
	return nil
}

// GetActiveDownloads returns all active download tasks
func (sd *SnapshotDownloader) GetActiveDownloads() map[string]*DownloadTask {
	sd.mu.RLock()
	defer sd.mu.RUnlock()

	result := make(map[string]*DownloadTask)
	for k, v := range sd.activeDownloads {
		result[k] = v
	}
	return result
}

// GetDownloadStatus returns the status of a specific download
func (sd *SnapshotDownloader) GetDownloadStatus(taskID string) (*DownloadTask, bool) {
	sd.mu.RLock()
	defer sd.mu.RUnlock()

	task, exists := sd.activeDownloads[taskID]
	return task, exists
}

// CancelDownload cancels a download task
func (sd *SnapshotDownloader) CancelDownload(taskID string) bool {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	task, exists := sd.activeDownloads[taskID]
	if !exists {
		return false
	}

	task.mu.Lock()
	task.Status = TransferStatusCancelled
	task.mu.Unlock()

	return true
}

// setupUDPConnection initializes a UDP listener on an ephemeral port
func (sd *SnapshotDownloader) setupUDPConnection() error {
	addr, err := net.ResolveUDPAddr("udp", ":0")
	if err != nil {
		return fmt.Errorf("failed to resolve UDP address: %w", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on UDP: %w", err)
	}
	sd.udpConn = conn
	return nil
}

// Stop stops the snapshot downloader
func (sd *SnapshotDownloader) Stop() {
	close(sd.stopCh)
	if sd.udpConn != nil {
		sd.udpConn.Close()
	}
}
