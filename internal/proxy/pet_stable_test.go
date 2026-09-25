package proxy

import (
	"bytes"
	"encoding/binary"
	"io"
	"log/slog"
	"testing"
	"time"

	"redscarf/internal/legacyauth"
	"redscarf/internal/legacyworld"
	"redscarf/internal/modernworld"
)

func TestStableControlsForwardToLegacy(t *testing.T) {
	legacyNPC := uint64(0xf130005100001234)
	modernNPC := modernworld.GUID128{Low: 0x1234, High: uint64(3) << 58}
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
		objectGUIDs: map[uint64]modernworld.GUID128{legacyNPC: modernNPC},
	}

	npcBody := appendTestModernPackedGUID(nil, modernNPC)
	for _, request := range []struct {
		name   string
		opcode uint16
		legacy uint32
	}{
		{name: "request stabled pets", opcode: modernworld.CMSGRequestStabledPets, legacy: uint32(legacyworld.MSGListStabledPets)},
		{name: "stable pet", opcode: modernworld.CMSGStablePet, legacy: legacyworld.CMSGStablePet},
		{name: "buy stable slot", opcode: modernworld.CMSGBuyStableSlot, legacy: legacyworld.CMSGBuyStableSlot},
	} {
		handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: request.opcode, Body: npcBody})
		if err != nil || !handled {
			t.Fatalf("%s handled=%v err=%v", request.name, handled, err)
		}
		write := legacyConnection.nextWrite(t)
		if write.opcode != request.legacy || binary.LittleEndian.Uint64(write.body) != legacyNPC {
			t.Fatalf("%s write=%#v", request.name, write)
		}
	}

	numberBody := binary.LittleEndian.AppendUint32(nil, 9)
	numberBody = appendTestModernPackedGUID(numberBody, modernNPC)
	for _, request := range []struct {
		name   string
		opcode uint16
		legacy uint32
	}{
		{name: "unstable pet", opcode: modernworld.CMSGUnstablePet, legacy: legacyworld.CMSGUnstablePet},
		{name: "swap pet", opcode: modernworld.CMSGStableSwapPet, legacy: legacyworld.CMSGStableSwapPet},
	} {
		handled, err := server.handleModernPlayPacket(session, nil, modernworld.Packet{Opcode: request.opcode, Body: numberBody})
		if err != nil || !handled {
			t.Fatalf("%s handled=%v err=%v", request.name, handled, err)
		}
		write := legacyConnection.nextWrite(t)
		if write.opcode != request.legacy || len(write.body) != 12 ||
			binary.LittleEndian.Uint64(write.body) != legacyNPC || binary.LittleEndian.Uint32(write.body[8:]) != 9 {
			t.Fatalf("%s write=%#v", request.name, write)
		}
	}
	if session.lastStableMaster != legacyNPC {
		t.Fatalf("cached stable master=%#x", session.lastStableMaster)
	}

	unknown := &proxySession{legacy: &legacyauth.Session{Username: "TEST"}, legacyWorld: legacyConnection}
	handled, err := server.handleModernPlayPacket(unknown, nil, modernworld.Packet{Opcode: modernworld.CMSGRequestStabledPets, Body: appendTestModernPackedGUID(nil, modernworld.GUID128{Low: 0x9999, High: uint64(3) << 58})})
	if !handled || err == nil {
		t.Fatalf("unknown stable master handled=%v err=%v", handled, err)
	}
}

func TestListStabledPetsRelaysValuesUpdate(t *testing.T) {
	const (
		legacyPlayer = uint64(0x42)
		legacyMaster = uint64(0xf130005100001111)
		currentEntry = uint32(26125)
		stabledEntry = uint32(3098)
	)
	legacySummon := uint64(0xf140000008000001)
	modernPlayer := modernworld.GUID128{Low: 0x42, High: 1}
	modernMaster := modernworld.GUID128{Low: 0x1111, High: uint64(3) << 58}
	modernSummon := modernworld.ModernGUIDForLegacy(legacySummon, 0)
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:              &legacyauth.Session{Username: "TEST"},
		legacyWorld:         legacyConnection,
		currentCharacter:    legacyPlayer,
		activePlayerCreated: true,
		objectGUIDs: map[uint64]modernworld.GUID128{
			legacyPlayer: modernPlayer,
			legacyMaster: modernMaster,
			legacySummon: modernSummon,
		},
		objectFields: map[uint64]map[int]uint32{
			legacyPlayer: {8: uint32(legacySummon), 9: uint32(legacySummon >> 32)},
			legacySummon: {67: 155},
		},
		creatureDisplay: map[uint32]uint32{stabledEntry: 447},
	}
	done := startLegacyRelay(t, server, session, legacyConnection)

	body := binary.LittleEndian.AppendUint64(nil, legacyMaster)
	body = append(body, 2, 4)
	body = binary.LittleEndian.AppendUint32(body, 8)
	body = binary.LittleEndian.AppendUint32(body, currentEntry)
	body = binary.LittleEndian.AppendUint32(body, 80)
	body = append(body, "哈奇"...)
	body = append(body, 0, 1)
	body = binary.LittleEndian.AppendUint32(body, 9)
	body = binary.LittleEndian.AppendUint32(body, stabledEntry)
	body = binary.LittleEndian.AppendUint32(body, 40)
	body = append(body, "Wolf"...)
	body = append(body, 0, 2)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGListStabledPets, Body: body}

	packets := waitQueuedInstance(t, session, 2)
	stopLegacyRelay(t, legacyConnection, done)

	if packets[0].Opcode != modernworld.SMSGPetGuids {
		t.Fatalf("pet guids opcode=%d", packets[0].Opcode)
	}
	wantGuids := modernworld.EncodePetGuids([]modernworld.GUID128{modernSummon})
	if !bytes.Equal(packets[0].Body, wantGuids) {
		t.Fatalf("pet guids=%x want=%x", packets[0].Body, wantGuids)
	}
	if packets[1].Opcode != modernworld.SMSGUpdateObject {
		t.Fatalf("values opcode=%d", packets[1].Opcode)
	}
	if session.lastStableMaster != legacyMaster {
		t.Fatalf("cached stable master=%#x", session.lastStableMaster)
	}
	list := modernworld.LegacyStabledPets{
		Master:         legacyMaster,
		NumStableSlots: 4,
		Pets: []modernworld.LegacyStabledPet{
			{PetNumber: 8, CreatureID: currentEntry, DisplayID: 155, Level: 80, Name: "哈奇", Flags: 1, PetSlot: 6},
			{PetNumber: 9, CreatureID: stabledEntry, DisplayID: 447, Level: 40, Name: "Wolf", Flags: 3, PetSlot: 6},
		},
	}
	wantUpdate, err := modernworld.EncodePetStableValuesUpdate(0, modernPlayer, modernMaster, list)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(packets[1].Body, wantUpdate) {
		t.Fatalf("stable values=%x want=%x", packets[1].Body, wantUpdate)
	}
}

func TestListStabledPetsEmptyAndMissingDisplay(t *testing.T) {
	const (
		legacyPlayer = uint64(0x42)
		legacyMaster = uint64(0xf130005100001111)
		stabledEntry = uint32(3098)
	)
	modernPlayer := modernworld.GUID128{Low: 0x42, High: 1}
	modernMaster := modernworld.GUID128{Low: 0x1111, High: uint64(3) << 58}
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:              &legacyauth.Session{Username: "TEST"},
		legacyWorld:         legacyConnection,
		currentCharacter:    legacyPlayer,
		activePlayerCreated: true,
		objectGUIDs: map[uint64]modernworld.GUID128{
			legacyPlayer: modernPlayer,
			legacyMaster: modernMaster,
		},
		objectFields:     map[uint64]map[int]uint32{legacyPlayer: {}},
		creatureDisplay:  map[uint32]uint32{},
		queriedCreatures: map[uint32]struct{}{},
	}
	done := startLegacyRelay(t, server, session, legacyConnection)

	empty := binary.LittleEndian.AppendUint64(nil, legacyMaster)
	empty = append(empty, 0, 2)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGListStabledPets, Body: empty}
	emptyPackets := waitQueuedInstance(t, session, 2)
	if emptyPackets[0].Opcode != modernworld.SMSGPetGuids || binary.LittleEndian.Uint32(emptyPackets[0].Body) != 0 {
		t.Fatalf("empty pet guids=%x", emptyPackets[0].Body)
	}
	if emptyPackets[1].Opcode != modernworld.SMSGUpdateObject {
		t.Fatalf("empty values opcode=%d", emptyPackets[1].Opcode)
	}

	session.worldMu.Lock()
	session.pendingInstance = nil
	session.worldMu.Unlock()

	missing := binary.LittleEndian.AppendUint64(nil, legacyMaster)
	missing = append(missing, 1, 4)
	missing = binary.LittleEndian.AppendUint32(missing, 9)
	missing = binary.LittleEndian.AppendUint32(missing, stabledEntry)
	missing = binary.LittleEndian.AppendUint32(missing, 40)
	missing = append(missing, "Wolf"...)
	missing = append(missing, 0, 2)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGListStabledPets, Body: missing}

	query := legacyConnection.nextWrite(t)
	if query.opcode != legacyworld.CMSGCreatureQuery || binary.LittleEndian.Uint32(query.body[:4]) != stabledEntry {
		t.Fatalf("creature query=%#v", query)
	}
	missingPackets := waitQueuedInstance(t, session, 2)
	stopLegacyRelay(t, legacyConnection, done)
	if missingPackets[0].Opcode != modernworld.SMSGPetGuids || missingPackets[1].Opcode != modernworld.SMSGUpdateObject {
		t.Fatalf("missing-display opcodes=%d/%d", missingPackets[0].Opcode, missingPackets[1].Opcode)
	}
	list := modernworld.LegacyStabledPets{
		Master:         legacyMaster,
		NumStableSlots: 4,
		Pets: []modernworld.LegacyStabledPet{
			{PetNumber: 9, CreatureID: stabledEntry, Level: 40, Name: "Wolf", Flags: 3, PetSlot: 6},
		},
	}
	want, err := modernworld.EncodePetStableValuesUpdate(0, modernPlayer, modernMaster, list)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(missingPackets[1].Body, want) {
		t.Fatalf("missing-display values still sent immediately")
	}
}

func TestPetStableResultAndActionSoundRelay(t *testing.T) {
	const legacyUnit = uint64(0xf140000008000001)
	modernUnit := modernworld.ModernGUIDForLegacy(legacyUnit, 0)
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
		objectGUIDs: map[uint64]modernworld.GUID128{legacyUnit: modernUnit},
	}
	done := startLegacyRelay(t, server, session, legacyConnection)

	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGPetStableResult, Body: []byte{0x08}}
	resultPackets := waitQueuedInstance(t, session, 1)
	if resultPackets[0].Opcode != modernworld.SMSGPetStableResult || !bytes.Equal(resultPackets[0].Body, []byte{0x08}) {
		t.Fatalf("stable result=%#v", resultPackets[0])
	}

	session.worldMu.Lock()
	session.pendingInstance = nil
	session.worldMu.Unlock()

	soundBody := binary.LittleEndian.AppendUint64(nil, legacyUnit)
	soundBody = binary.LittleEndian.AppendUint32(soundBody, 2)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGPetActionSound, Body: soundBody}
	soundPackets := waitQueuedInstance(t, session, 1)
	stopLegacyRelay(t, legacyConnection, done)
	if soundPackets[0].Opcode != modernworld.SMSGPetActionSound {
		t.Fatalf("action sound opcode=%d", soundPackets[0].Opcode)
	}
	want := modernworld.EncodePetActionSound(modernUnit, 2)
	if !bytes.Equal(soundPackets[0].Body, want) {
		t.Fatalf("action sound=%x want=%x", soundPackets[0].Body, want)
	}
}

func TestPetStableResultSuccessReloadsStabledPets(t *testing.T) {
	const (
		legacyPlayer = uint64(0x42)
		legacyMaster = uint64(0xf130005100001111)
	)
	modernPlayer := modernworld.GUID128{Low: 0x42, High: 1}
	modernMaster := modernworld.GUID128{Low: 0x1111, High: uint64(3) << 58}
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:              &legacyauth.Session{Username: "TEST"},
		legacyWorld:         legacyConnection,
		currentCharacter:    legacyPlayer,
		activePlayerCreated: true,
		objectGUIDs: map[uint64]modernworld.GUID128{
			legacyPlayer: modernPlayer,
			legacyMaster: modernMaster,
		},
		objectFields: map[uint64]map[int]uint32{legacyPlayer: {}},
	}
	done := startLegacyRelay(t, server, session, legacyConnection)

	open := binary.LittleEndian.AppendUint64(nil, legacyMaster)
	open = append(open, 0, 0)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGListStabledPets, Body: open}
	if packets := waitQueuedInstance(t, session, 2); packets[0].Opcode != modernworld.SMSGPetGuids || packets[1].Opcode != modernworld.SMSGUpdateObject {
		t.Fatalf("open opcodes=%d/%d", packets[0].Opcode, packets[1].Opcode)
	}

	session.worldMu.Lock()
	session.pendingInstance = nil
	session.worldMu.Unlock()

	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGPetStableResult, Body: []byte{modernworld.PetStableSuccessBuySlot}}
	resultPackets := waitQueuedInstance(t, session, 1)
	if resultPackets[0].Opcode != modernworld.SMSGPetStableResult || !bytes.Equal(resultPackets[0].Body, []byte{modernworld.PetStableSuccessBuySlot}) {
		t.Fatalf("buy result=%#v", resultPackets[0])
	}
	reload := legacyConnection.nextWrite(t)
	if reload.opcode != uint32(legacyworld.MSGListStabledPets) || binary.LittleEndian.Uint64(reload.body) != legacyMaster {
		t.Fatalf("reload write=%#v", reload)
	}

	session.worldMu.Lock()
	session.pendingInstance = nil
	session.worldMu.Unlock()

	refreshed := binary.LittleEndian.AppendUint64(nil, legacyMaster)
	refreshed = append(refreshed, 0, 1)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGListStabledPets, Body: refreshed}
	refreshPackets := waitQueuedInstance(t, session, 2)
	if refreshPackets[0].Opcode != modernworld.SMSGPetGuids || refreshPackets[1].Opcode != modernworld.SMSGUpdateObject {
		t.Fatalf("refresh opcodes=%d/%d", refreshPackets[0].Opcode, refreshPackets[1].Opcode)
	}
	want, err := modernworld.EncodePetStableValuesUpdate(0, modernPlayer, modernMaster, modernworld.LegacyStabledPets{
		Master:         legacyMaster,
		NumStableSlots: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(refreshPackets[1].Body, want) {
		t.Fatalf("refresh values=%x want=%x", refreshPackets[1].Body, want)
	}

	session.worldMu.Lock()
	session.pendingInstance = nil
	session.worldMu.Unlock()
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGPetStableResult, Body: []byte{0x01}}
	failPackets := waitQueuedInstance(t, session, 1)
	stopLegacyRelay(t, legacyConnection, done)
	if failPackets[0].Opcode != modernworld.SMSGPetStableResult || failPackets[0].Body[0] != 0x01 {
		t.Fatalf("failure result=%#v", failPackets[0])
	}
	select {
	case write := <-legacyConnection.writes:
		t.Fatalf("failure reloaded stabled pets: %#v", write)
	default:
	}
}

func TestCreatureQueryCachesStableDisplay(t *testing.T) {
	legacyConnection := newFakeLegacyWorldConnection()
	server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	session := &proxySession{
		legacy:      &legacyauth.Session{Username: "TEST"},
		legacyWorld: legacyConnection,
	}
	done := startLegacyRelay(t, server, session, legacyConnection)

	legacy := binary.LittleEndian.AppendUint32(nil, 69)
	legacy = append(legacy, "Thing"...)
	legacy = append(legacy, 0, 0, 0, 0, 0, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 1)
	legacy = binary.LittleEndian.AppendUint32(legacy, 7)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 123)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x3fc00000) // 1.5
	legacy = binary.LittleEndian.AppendUint32(legacy, 0x3f800000) // 1
	legacy = append(legacy, 1)
	for range 6 {
		legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	}
	legacy = binary.LittleEndian.AppendUint32(legacy, 42)
	legacyConnection.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGCreatureQueryResponse, Body: legacy}
	_ = waitQueuedInstance(t, session, 1)
	stopLegacyRelay(t, legacyConnection, done)

	session.worldMu.Lock()
	display := session.creatureDisplay[69]
	session.worldMu.Unlock()
	if display != 123 {
		t.Fatalf("cached display=%d", display)
	}
}

func startLegacyRelay(t *testing.T, server *Server, session *proxySession, conn *fakeLegacyWorldConnection) chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		server.relayLegacyWorld(session, nil, conn)
		close(done)
	}()
	return done
}

func stopLegacyRelay(t *testing.T, conn *fakeLegacyWorldConnection, done chan struct{}) {
	t.Helper()
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("legacy relay did not stop")
	}
}

func waitQueuedInstance(t *testing.T, session *proxySession, n int) []modernworld.Packet {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		session.worldMu.Lock()
		queued := len(session.pendingInstance)
		session.worldMu.Unlock()
		if queued == n {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("queued %d packets, want %d", queued, n)
		}
		time.Sleep(time.Millisecond)
	}
	session.worldMu.Lock()
	packets := append([]modernworld.Packet(nil), session.pendingInstance...)
	session.worldMu.Unlock()
	return packets
}
