package modernworld

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestPacketHistoryPayloadAndBounds(t *testing.T) {
	c := &PacketConn{}
	c.history.record(SMSGAuthChallenge, []byte("secret"))
	body := []byte{1, 2, 3}
	c.history.record(SMSGUpdateObject, body)
	body[0] = 9
	var output bytes.Buffer
	if err := c.WriteRecentPackets(&output); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&output)
	var auth, update packetHistoryRecord
	if err := decoder.Decode(&auth); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&update); err != nil {
		t.Fatal(err)
	}
	if auth.Hex != "" || update.Hex != "010203" {
		t.Fatalf("unexpected retained payloads: %+v %+v", auth, update)
	}
	for i := 0; i < 600; i++ {
		c.history.record(SMSGUpdateObject, make([]byte, 65536))
	}
	if len(c.history.records) > 512 || c.history.size > 4*1024*1024 {
		t.Fatal("history exceeded budget")
	}
	if c.history.records[len(c.history.records)-1].Sequence != 602 {
		t.Fatal("newest packet missing")
	}
}
