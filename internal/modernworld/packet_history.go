package modernworld

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"sync"
	"time"
)

// Bounded, per-connection history. Only object/combat packets retain payloads;
// authentication, account data and chat are metadata-only.
type packetHistory struct {
	mu       sync.Mutex
	records  []packetHistoryRecord
	size     int
	sequence uint64
}
type packetHistoryRecord struct {
	Sequence uint64    `json:"sequence"`
	Time     time.Time `json:"time"`
	Opcode   uint16    `json:"opcode"`
	Length   int       `json:"length"`
	Hex      string    `json:"hex,omitempty"`
}

func (h *packetHistory) record(opcode uint16, body []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sequence++
	r := packetHistoryRecord{Sequence: h.sequence, Time: time.Now(), Opcode: opcode, Length: len(body)}
	switch opcode {
	case SMSGUpdateObject, SMSGQueryCreature, SMSGAuraUpdate, SMSGOnMonsterMove, SMSGMoveUpdate:
		if len(body) <= 256*1024 {
			r.Hex = hex.EncodeToString(body)
		}
	}
	const budget = 4 * 1024 * 1024
	for len(h.records) > 0 && (len(h.records) >= 512 || h.size+len(r.Hex) > budget) {
		h.size -= len(h.records[0].Hex)
		h.records[0] = packetHistoryRecord{}
		h.records = h.records[1:]
	}
	h.records = append(h.records, r)
	h.size += len(r.Hex)
}

// WriteRecentPackets exports a snapshot without holding the network write lock.
func (c *PacketConn) WriteRecentPackets(w io.Writer) error {
	c.history.mu.Lock()
	records := append([]packetHistoryRecord(nil), c.history.records...)
	c.history.mu.Unlock()
	encoder := json.NewEncoder(w)
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			return err
		}
	}
	return nil
}
