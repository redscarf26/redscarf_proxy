package modernworld

import (
	"encoding/binary"
	"testing"
)

func TestLogoutRequestAndCancel(t *testing.T) {
	idle, err := ParseLogoutRequest([]byte{1})
	if err != nil || !idle {
		t.Fatalf("idle=%v err=%v", idle, err)
	}
	if _, err := ParseLogoutRequest(nil); err == nil {
		t.Fatal("expected a short logout-request error")
	}
	if err := ParseLogoutCancel(nil); err != nil {
		t.Fatal(err)
	}
	if err := ParseLogoutCancel([]byte{0}); err == nil {
		t.Fatal("expected a non-empty logout-cancel error")
	}
}

func TestLogoutResponses(t *testing.T) {
	if SMSGLogoutResponse != 0x2683 || SMSGLogoutComplete != 0x2684 || SMSGLogoutCancelAck != 0x2685 {
		t.Fatalf("logout opcodes: cancel=0x%x complete=0x%x response=0x%x",
			SMSGLogoutCancelAck, SMSGLogoutComplete, SMSGLogoutResponse)
	}
	legacy := binary.LittleEndian.AppendUint32(nil, 7)
	legacy = append(legacy, 1)
	response, err := ParseLegacyLogoutResponse(legacy)
	if err != nil || response.Result != 7 || !response.Instant {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	modern := EncodeLogoutResponse(response)
	if len(modern) != 5 || binary.LittleEndian.Uint32(modern[:4]) != 7 || modern[4] != 1 {
		t.Fatalf("modern response=%x", modern)
	}
	if complete := EncodeLogoutComplete(); len(complete) != 0 {
		t.Fatalf("logout complete=%x", complete)
	}
	if ack := EncodeLogoutCancelAck(); len(ack) != 0 {
		t.Fatalf("logout cancel ack=%x", ack)
	}
}
