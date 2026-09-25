package modernworld

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestTransferPendingContinentAndShip(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 571)
	pending, err := ParseLegacyTransferPending(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if pending.MapID != 571 || pending.Ship != nil {
		t.Fatalf("unexpected continent transfer: %#v", pending)
	}
	body := EncodeTransferPending(pending)
	if len(body) != 17 || binary.LittleEndian.Uint32(body[:4]) != 571 || body[16] != 0 {
		t.Fatalf("continent transfer body=%x", body)
	}

	legacy = binary.LittleEndian.AppendUint32(legacy, 20808)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	pending, err = ParseLegacyTransferPending(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if pending.Ship == nil || pending.Ship.ID != 20808 || pending.Ship.OriginMapID != 0 {
		t.Fatalf("unexpected ship transfer: %#v", pending)
	}
	body = EncodeTransferPending(pending)
	if len(body) != 25 || body[16]&0xc0 != 0x80 || binary.LittleEndian.Uint32(body[17:21]) != 20808 {
		t.Fatalf("ship transfer body=%x", body)
	}
	if _, err := ParseLegacyTransferPending([]byte{1, 2, 3}); err == nil {
		t.Fatal("truncated transfer-pending was accepted")
	}
}

func TestTransferAbortedReasonBits(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 33)
	legacy = append(legacy, 2, 3)
	aborted, err := ParseLegacyTransferAborted(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if aborted.MapID != 33 || aborted.Reason != 2 || aborted.Arg != 3 || aborted.MapDifficultyXConditionID != -6 {
		t.Fatalf("unexpected abort: %#v", aborted)
	}
	body := EncodeTransferAborted(aborted)
	if len(body) != 10 || body[4] != 3 || int32(binary.LittleEndian.Uint32(body[5:9])) != -6 || body[9]&0xfc != 0x08 {
		t.Fatalf("abort body=%x", body)
	}
	short := binary.LittleEndian.AppendUint32(nil, 0)
	short = append(short, 1)
	if _, err := ParseLegacyTransferAborted(short); err != nil {
		t.Fatal(err)
	}
}

func TestNewWorldAddsReasonAndOffset(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 571)
	for _, value := range []float32{1.5, 2.5, 3.5, -0.25} {
		legacy = appendFloat32(legacy, value)
	}
	world, err := ParseLegacyNewWorld(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if world.MapID != 571 || world.Position != [3]float32{1.5, 2.5, 3.5} || world.Reason != 4 {
		t.Fatalf("unexpected new world: %#v", world)
	}
	if world.Orientation < 6.03 {
		t.Fatalf("orientation was not normalized: %v", world.Orientation)
	}
	body := EncodeNewWorld(world)
	if len(body) != 36 || binary.LittleEndian.Uint32(body[20:24]) != 4 || binary.LittleEndian.Uint32(body[24:]) != 0 {
		t.Fatalf("new-world body=%x", body)
	}
}

func TestSuspendTokenAndWorldServerInfoLayouts(t *testing.T) {
	token := EncodeSuspendToken(DefaultSuspendToken())
	if len(token) != 5 || binary.LittleEndian.Uint32(token[:4]) != 3 || token[4] != 0x40 {
		t.Fatalf("suspend token=%x", token)
	}
	resume := EncodeSuspendToken(SuspendToken{SequenceIndex: 3, Reason: 1})
	if !equalBytes(token, resume) {
		t.Fatalf("resume token diverged from suspend: %x vs %x", resume, token)
	}
	sequence, err := ParseSuspendTokenResponse(binary.LittleEndian.AppendUint32(nil, 3))
	if err != nil || sequence != 3 {
		t.Fatalf("suspend response sequence=%d err=%v", sequence, err)
	}
	if _, err := ParseSuspendTokenResponse([]byte{3}); err == nil {
		t.Fatal("short suspend response was accepted")
	}

	continent := EncodeWorldServerInfo(WorldServerInfoForMap(0))
	if len(continent) != 6 || binary.LittleEndian.Uint32(continent[:4]) != 0 || continent[5] != 0 {
		t.Fatalf("continent world-server-info=%x", continent)
	}
	instance := EncodeWorldServerInfo(WorldServerInfoForMap(33))
	if len(instance) != 10 || binary.LittleEndian.Uint32(instance[:4]) != 1 || instance[5] != 0x10 || binary.LittleEndian.Uint32(instance[6:]) != 5 {
		t.Fatalf("instance world-server-info=%x", instance)
	}
	if got := EncodeUpdateLastInstance(33); len(got) != 4 || binary.LittleEndian.Uint32(got) != 33 {
		t.Fatalf("last instance=%x", got)
	}
}

func TestMoveTeleportAckRoundTrip(t *testing.T) {
	move := LegacyMovement{MoveFlags: 1, MoveTime: 99, X: 10, Y: 20, Z: 30, Orientation: 1.25, TransportSeat: -1}
	legacyBody, err := EncodeLegacyPlayerMovement(PlayerMovement{Move: move}, 0x42, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Server MSG_MOVE_TELEPORT_ACK is packed GUID + counter + MovementInfo without a second GUID.
	r := movementReader{data: legacyBody}
	if _, err := r.guid64(); err != nil {
		t.Fatal(err)
	}
	ackBody := appendLegacyPackedGUID(nil, 0x42)
	ackBody = binary.LittleEndian.AppendUint32(ackBody, 7)
	ackBody = append(ackBody, legacyBody[r.offset:]...)
	guid, counter, parsed, err := ParseLegacyMoveTeleportAck(ackBody)
	if err != nil {
		t.Fatal(err)
	}
	if guid != 0x42 || counter != 7 || parsed.X != 10 || parsed.MoveTime != 99 {
		t.Fatalf("guid=%x counter=%d move=%#v", guid, counter, parsed)
	}
	mover := GUID128{Low: 0x42, High: 1}
	modern := EncodeMoveTeleport(MoveTeleportFromLegacy(mover, counter, parsed, GUID128{}))
	guidSize := len(appendPackedGUID128(nil, mover.Low, mover.High))
	if len(modern) != guidSize+4+12+4+1+1 || binary.LittleEndian.Uint32(modern[guidSize:guidSize+4]) != 7 || modern[len(modern)-1]&0xc0 != 0 {
		t.Fatalf("modern teleport=%x", modern)
	}
	if math.Float32frombits(binary.LittleEndian.Uint32(modern[guidSize+4:guidSize+8])) != 10 {
		t.Fatalf("modern teleport X was not preserved: %x", modern)
	}

	clientAck := appendPackedGUID128(nil, mover.Low, mover.High)
	clientAck = binary.LittleEndian.AppendUint32(clientAck, 7)
	clientAck = binary.LittleEndian.AppendUint32(clientAck, 12345)
	parsedAck, err := ParseMoveTeleportAck(clientAck)
	if err != nil {
		t.Fatal(err)
	}
	legacyAck := EncodeLegacyMoveTeleportAck(0x42, parsedAck)
	ackReader := movementReader{data: legacyAck}
	gotGUID, err := ackReader.guid64()
	if err != nil {
		t.Fatal(err)
	}
	gotCounter, err := ackReader.u32()
	if err != nil {
		t.Fatal(err)
	}
	gotTime, err := ackReader.u32()
	if err != nil {
		t.Fatal(err)
	}
	if gotGUID != 0x42 || gotCounter != 7 || gotTime != 12345 || ackReader.remaining() != 0 {
		t.Fatalf("legacy ack guid=%x counter=%d time=%d remaining=%d", gotGUID, gotCounter, gotTime, ackReader.remaining())
	}
}

func TestMoveTeleportVehicleAndTransportBits(t *testing.T) {
	seat := int8(2)
	body := EncodeMoveTeleport(MoveTeleport{
		Mover: GUID128{Low: 0x42, High: 1}, Transport: GUID128{Low: 0x99, High: 2},
		MoveCounter: 1, Position: [3]float32{1, 2, 3}, Orientation: 0.5, VehicleSeat: &seat,
	})
	guidSize := len(appendPackedGUID128(nil, 0x42, 1))
	flags := body[guidSize+4+12+4+1]
	if flags&0xc0 != 0xc0 {
		t.Fatalf("expected transport and vehicle bits, flags=%x body=%x", flags, body)
	}
	if body[guidSize+4+12+4+2] != 2 {
		t.Fatalf("vehicle seat missing: %x", body)
	}
}

func TestWorldPortAndLoadingScreen(t *testing.T) {
	if err := ParseWorldPortResponse(nil); err != nil {
		t.Fatal(err)
	}
	if err := ParseWorldPortResponse([]byte{1}); err == nil {
		t.Fatal("world-port-response with a body was accepted")
	}
	notify, err := ParseLoadingScreenNotify(EncodeLoadingScreenNotify(LoadingScreenNotify{MapID: 571, Showing: true}))
	if err != nil {
		t.Fatal(err)
	}
	if notify.MapID != 571 || !notify.Showing {
		t.Fatalf("unexpected loading screen: %#v", notify)
	}
	if _, err := ParseLoadingScreenNotify(nil); err == nil {
		t.Fatal("empty loading-screen-notify was accepted")
	}
}

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
