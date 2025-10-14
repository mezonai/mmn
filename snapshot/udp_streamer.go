package snapshot

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/mezonai/mmn/logx"
)

const (
	DEFALU_CHUNK_SIZE = 16 * 1024
)

func StartSnapshotUDPStreamer(snapshotDir string, listenAddr string) error {
	addr, err := net.ResolveUDPAddr("udp", listenAddr)
	if err != nil {
		return fmt.Errorf("resolve udp addr: %w", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("listen udp: %w", err)
	}
	logx.Info("SNAPSHOT:STREAMER", "listening on", listenAddr)

	go func() {
		defer conn.Close()
		buf := make([]byte, 65507)
		for {
			n, remote, err := conn.ReadFromUDP(buf)
			if err != nil {
				logx.Error("SNAPSHOT:STREAMER", "read error:", err)
				continue
			}
			var req SnapshotTransferRequest
			if err := json.Unmarshal(buf[:n], &req); err != nil {
				continue
			}
			receiver := &net.UDPAddr{IP: remote.IP, Port: req.ReceiverPort}
			path := filepath.Join(snapshotDir, FileName)
			fileInfo, err := os.Stat(path)
			if err != nil {
				logx.Error("SNAPSHOT:STREAMER", "stat snapshot-latest.json:", err)
				continue
			}
			// compute total chunks
			chunkSize := req.ChunkSize
			if chunkSize <= 0 {
				chunkSize = DEFALU_CHUNK_SIZE
			}
			file, err := os.Open(path)
			if err != nil {
				logx.Error("SNAPSHOT:STREAMER", "open snapshot:", err)
				continue
			}
			go func() {
				defer file.Close()
				total := int((fileInfo.Size() + int64(chunkSize) - 1) / int64(chunkSize))
				buffer := make([]byte, chunkSize)
				sessionID := fmt.Sprintf("%x", time.Now().UnixNano())
				for idx := 0; ; idx++ {
					n, er := file.Read(buffer)
					if n == 0 {
						break
					}
					data := make([]byte, n)
					copy(data, buffer[:n])
					chunk := SnapshotChunk{
						SessionID:   sessionID,
						ChunkIndex:  idx,
						TotalChunks: total,
						Data:        data,
						Checksum:    fmt.Sprintf("%x", sha256Sum(data)),
					}
					pkt, _ := json.Marshal(chunk)
					_, _ = conn.WriteToUDP(pkt, receiver)
					if er != nil {
						break
					}
				}
				logx.Info("SNAPSHOT:STREAMER", "completed stream to", receiver.String())
			}()
		}
	}()
	return nil
}

func sha256Sum(b []byte) [32]byte {
	return sha256.Sum256(b)
}
