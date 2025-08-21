// validator/shredder.go
package validator

import (
	"encoding/binary"

	"github.com/klauspost/reedsolomon"
	"github.com/mezonai/mmn/types"
)

type ShredderConfig struct {
	K, M       int
	MaxPayload int
}

type Shredder struct {
	cfg ShredderConfig
	rs  reedsolomon.Encoder
}

func New(cfg ShredderConfig) (*Shredder, error) {
	rs, err := reedsolomon.New(cfg.K, cfg.M)
	if err != nil {
		return nil, err
	}
	return &Shredder{cfg: cfg, rs: rs}, nil
}

func (s *Shredder) MakeShreds(slot uint64, entries [][]byte, sign func([]byte) [64]byte, version uint16, endOfSlotFlag uint8) (data, coding []types.Shred, err error) {
	packed := packEntries(entries)
	data = splitToDataShreds(slot, packed, s.cfg.MaxPayload, version)

	if len(data) > 0 {
		data[len(data)-1].Header.Flags |= endOfSlotFlag // bit0
		data[len(data)-1].Header.PayloadSize = uint16(len(packed) % s.cfg.MaxPayload)
		if data[len(data)-1].Header.PayloadSize == 0 {
			data[len(data)-1].Header.PayloadSize = uint16(s.cfg.MaxPayload)
		}
	}

	// 3) nhóm K data -> tạo M parity
	groups := groupK(data, s.cfg.K) // [][]Shred
	for _, g := range groups {
		// chuẩn bị buffer RS: K data + M parity (mỗi buffer = MaxPayload bytes)
		bufs := make([][]byte, 0, s.cfg.K+s.cfg.M)
		for i := 0; i < s.cfg.K; i++ {
			p := g[i].Payload
			if len(p) < s.cfg.MaxPayload {
				// pad 0 để đủ độ dài RS
				pad := make([]byte, s.cfg.MaxPayload-len(p))
				p = append(append([]byte(nil), p...), pad...)
			}
			bufs = append(bufs, p)
		}
		parity := make([][]byte, s.cfg.M)
		for i := range parity {
			parity[i] = make([]byte, s.cfg.MaxPayload)
		}
		bufs = append(bufs, parity...)

		if err := s.rs.Encode(bufs); err != nil {
			return nil, nil, err
		}

		// 4) build coding shreds
		fecIndex := g[0].Header.FECSetIndex
		for j := 0; j < s.cfg.M; j++ {
			sh := types.Shred{
				Header: types.ShredHeader{
					Slot:        slot,
					Index:       uint32(fecIndex)*uint32(s.cfg.K) + uint32(j),
					FECSetIndex: fecIndex,
					Version:     version,
					Type:        1, // coding
					PayloadSize: uint16(s.cfg.MaxPayload),
				},
				Payload: parity[j],
			}
			coding = append(coding, sh)
		}
	}

	// 5) ký từng shred (v1)
	for i := range data {
		data[i].Header.Signature = sign(sigBytes(&data[i]))
	}
	for i := range coding {
		coding[i].Header.Signature = sign(sigBytes(&coding[i]))
	}

	return data, coding, nil
}

func packEntries(entries [][]byte) []byte {
	total := 4
	for _, e := range entries {
		total += 4 + len(e)
	}
	buf := make([]byte, 0, total)

	var u32 [4]byte
	binary.LittleEndian.PutUint32(u32[:], uint32(len(entries)))
	buf = append(buf, u32[:]...)

	for _, e := range entries {
		binary.LittleEndian.PutUint32(u32[:], uint32(len(e)))
		buf = append(buf, u32[:]...)
		buf = append(buf, e...)
	}
	return buf
}

func splitToDataShreds(slot uint64, packed []byte, maxPayload int, version uint16) []types.Shred {
	if maxPayload <= 0 {
		return nil
	}
	// Số shred cần thiết
	n := (len(packed) + maxPayload - 1) / maxPayload
	out := make([]types.Shred, 0, n)

	var idx uint32
	for off := 0; off < len(packed); off += maxPayload {
		end := off + maxPayload
		if end > len(packed) {
			end = len(packed)
		}
		chunk := packed[off:end]

		// NOTE: data shard có thể ngắn ở cuối; padding 0 sẽ thực hiện khi Encode RS
		payload := make([]byte, len(chunk))
		copy(payload, chunk)

		sh := types.Shred{
			Header: types.ShredHeader{
				Slot:        slot,
				Index:       idx,
				FECSetIndex: 0, // GÁN SAU khi group theo K
				Version:     version,
				Type:        0, // data
				Flags:       0, // end_of_slot sẽ set sau cho shred cuối
				PayloadSize: uint16(len(chunk)),
			},
			Payload: payload,
		}
		out = append(out, sh)
		idx++
	}
	return out
}

// groupK: chia data shreds thành các nhóm K và GÁN FECSetIndex cho từng shred
func groupK(data []types.Shred, K int) [][]types.Shred {
	if K <= 0 {
		return [][]types.Shred{data}
	}
	groups := make([][]types.Shred, 0, (len(data)+K-1)/K)
	for i := 0; i < len(data); i += K {
		j := i + K
		if j > len(data) {
			j = len(data)
		}
		grp := data[i:j]
		fecIdx := uint32(i / K)
		for x := range grp {
			grp[x].Header.FECSetIndex = fecIdx
		}
		groups = append(groups, grp)
	}
	return groups
}
