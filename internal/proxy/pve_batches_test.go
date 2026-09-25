package proxy

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"math"
	"testing"

	"redscarf/internal/legacyworld"
	"redscarf/internal/modernworld"
)

// Exercise the encrypted instance connection and actual legacy relay, including
// the flush boundary between Values and standalone Movement in one batch.
func testPVEBatchRelay(t *testing.T, legacy *fakeLegacyWorldConnection, client *modernworld.PacketConn, guid uint64) {
	t.Helper()
	read := func(op uint16) modernworld.Packet {
		t.Helper()
		p, err := client.ReadPacket()
		if err != nil || p.Opcode != op {
			t.Fatalf("relay want %#x, got %#v: %v", op, p, err)
		}
		return p
	}
	movement := appendTestLegacyPackedGUID([]byte{1}, guid)
	movement = binary.LittleEndian.AppendUint16(movement, 0x40)
	for _, v := range []float32{11, 22, 33, 1} {
		movement = binary.LittleEndian.AppendUint32(movement, math.Float32bits(v))
	}
	body := binary.LittleEndian.AppendUint32(nil, 3)
	body = append(body, testLegacyValues(guid, map[int]uint32{24: 776})[4:]...)
	body = append(body, movement...)
	body = append(body, testLegacyValues(guid, map[int]uint32{24: 775})[4:]...)
	for _, compressed := range []bool{false, true} {
		packet := legacyworld.Packet{Opcode: legacyworld.SMSGUpdateObject, Body: body}
		if compressed {
			var buf bytes.Buffer
			w := zlib.NewWriter(&buf)
			if _, err := w.Write(body); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			packet.Opcode = legacyworld.SMSGCompressedUpdateObject
			packet.Body = append(binary.LittleEndian.AppendUint32(nil, uint32(len(body))), buf.Bytes()...)
		}
		legacy.reads <- packet
		read(modernworld.SMSGUpdateObject)
		got, err := modernworld.ParseModernPlayerMovement(read(modernworld.SMSGMoveUpdate).Body)
		if err != nil || got.Move.X != 11 || got.Move.Y != 22 || got.Move.Z != 33 {
			t.Fatal(got, err)
		}
		read(modernworld.SMSGUpdateObject)
	}
	for _, kind := range []uint32{0, 2, 7, 1, 3, 4, 5, 6} {
		b := binary.LittleEndian.AppendUint32(nil, kind)
		if kind <= 2 {
			b = append(appendTestLegacyPackedGUID(b, guid), 2)
		}
		if kind >= 3 && kind <= 6 {
			b = append(b, 8)
		}
		if kind == 5 {
			b = append(b, 9)
		}
		legacy.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGUpdateInstanceEncounterUnit, Body: b}
		if kind == 0 {
			read(modernworld.SMSGInstanceEncounterStart)
		}
		op := map[uint32]uint16{0: 0x27b0, 1: 0x27b1, 2: 0x27b2, 3: 0x27b3, 4: 0x27b4, 5: 0x27b9, 6: 0x27b5, 7: 0x27b0}[kind]
		read(op)
		if kind == 1 {
			read(modernworld.SMSGInstanceEncounterEnd)
		}
	}
	for _, id := range []uint32{123, 0} {
		b := binary.LittleEndian.AppendUint32(appendTestLegacyPackedGUID(nil, guid), id)
		legacy.reads <- legacyworld.Packet{Opcode: modernworld.LegacySMSGPlayerVehicleData, Body: b}
		out := read(modernworld.SMSGSetVehicleRecID)
		if binary.LittleEndian.Uint32(out.Body[len(out.Body)-4:]) != id {
			t.Fatal(out)
		}
	}
	legacy.reads <- legacyworld.Packet{Opcode: modernworld.LegacySMSGCancelVehicleAura}
	read(modernworld.SMSGOnCancelExpectedRideVehicleAura)
}

func TestActualMapDifficultyResetsAcrossMapsAndCharacters(t *testing.T) {
	session := &proxySession{}
	session.resetCharacterSessionLocked()
	if session.mapReady {
		t.Fatal("character reset must await LoginVerifyWorld")
	}
	session.currentMapID = 631
	session.mapReady = true
	session.hasMapDifficulty = true
	session.mapDifficulty = modernworld.MapDifficulty{SpawnMode: 3, DynamicHeroic: true}
	if err := session.sendMapDifficulty(); err != nil {
		t.Fatal(err)
	}
	got := session.pendingInstance[len(session.pendingInstance)-1]
	if got.Opcode != modernworld.SMSGWorldServerInfo || binary.LittleEndian.Uint32(got.Body) != 6 || binary.LittleEndian.Uint32(got.Body[6:]) != 25 {
		t.Fatal(got)
	}
	session.resetWorldObjectsLocked(574)
	if session.hasMapDifficulty {
		t.Fatal("previous map mode leaked")
	}
	if err := session.sendMapDifficulty(); err != nil {
		t.Fatal(err)
	}
	got = session.pendingInstance[len(session.pendingInstance)-1]
	if binary.LittleEndian.Uint32(got.Body) != 0 || len(got.Body) != 6 {
		t.Fatal("unknown mode must not fabricate normal dungeon", got)
	}
	session.hasMapDifficulty = true
	session.mapDifficulty = modernworld.MapDifficulty{SpawnMode: 1}
	if err := session.sendMapDifficulty(); err != nil {
		t.Fatal(err)
	}
	got = session.pendingInstance[len(session.pendingInstance)-1]
	if binary.LittleEndian.Uint32(got.Body) != 2 {
		t.Fatal(got)
	}
	session.resetWorldObjectsLocked(571)
	if err := session.sendMapDifficulty(); err != nil {
		t.Fatal(err)
	}
	got = session.pendingInstance[len(session.pendingInstance)-1]
	if binary.LittleEndian.Uint32(got.Body) != 0 || len(got.Body) != 6 {
		t.Fatal(got)
	}
}

func TestEncounterVisibilityAndMapReset(t *testing.T) {
	session := &proxySession{encounterFrames: make(modernworld.EncounterFrames), objectGUIDs: map[uint64]modernworld.GUID128{42: {Low: 42, High: 77}}}
	session.encounterFrames.Apply(modernworld.EncounterEvent{Kind: 0, GUID: 42, Param1: 2}, session.visibleEncounterGUIDLocked)
	if err := session.flushEncounterFrames(); err != nil {
		t.Fatal(err)
	}
	if len(session.pendingInstance) != 0 {
		t.Fatal("frame preceded object create")
	}
	session.markObjectVisibleLocked(42)
	if err := session.flushEncounterFrames(); err != nil {
		t.Fatal(err)
	}
	if len(session.pendingInstance) != 2 ||
		session.pendingInstance[0].Opcode != modernworld.SMSGInstanceEncounterStart ||
		session.pendingInstance[1].Opcode != modernworld.SMSGInstanceEncounterEngageUnit {
		t.Fatal(session.pendingInstance)
	}
	session.forgetObjectVisibleLocked(42)
	if err := session.flushEncounterFrames(); err != nil {
		t.Fatal(err)
	}
	if len(session.pendingInstance) != 2 {
		t.Fatal("invisible frame replay")
	}
	session.markObjectVisibleLocked(42)
	if err := session.flushEncounterFrames(); err != nil {
		t.Fatal(err)
	}
	if len(session.pendingInstance) != 3 {
		t.Fatal("missing reengage")
	}
	session.resetWorldObjectsLocked(1)
	if len(session.encounterFrames) != 0 || session.encounterInProgress {
		t.Fatal("boss state leaked to another map")
	}
}

func TestVehicleActionsUseLegacyConnection(t *testing.T) {
	conn := newFakeLegacyWorldConnection()
	session := &proxySession{legacyWorld: conn}
	s := &Server{}
	for i, op := range []uint16{modernworld.CMSGRequestVehicleExit, modernworld.CMSGRequestVehiclePrevSeat, modernworld.CMSGRequestVehicleNextSeat} {
		handled, err := s.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: op})
		if err != nil || !handled {
			t.Fatal(handled, err)
		}
		write := conn.nextWrite(t)
		if write.opcode != uint32(0x476+i) || len(write.body) != 0 {
			t.Fatal(write)
		}
	}
}
