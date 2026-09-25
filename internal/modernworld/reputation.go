package modernworld

import (
	"encoding/binary"
	"fmt"
)

// Reputation-frame translation. The 3.4.3 engine re-serialized the classic
// reputation controls with modern opcodes and smaller fields: the at-war
// checkbox pair sends only the one-byte reputation-list index, while the
// inactive toggle and watched-faction selector keep 32-bit list indices (plus
// a state bit for inactive). 3.3.5a AzerothCore keys every reputation control
// by the same ReputationListID (the player's position in the faction list), so
// the index passes through unchanged once widened to 32 bits. Layouts mirror
// HermesProxy-WOTLK World/Server/Packets/{SetFactionAtWar,SetFactionInactive,
// SetWatchedFaction,SetFactionVisible}.cs and World/Server/WorldSocket.cs
// HandleSetFaction*.
const (
	// Client -> server reputation-frame controls (build 54261).
	CMSGSetFactionAtWar    = uint16(0x34DE) // 13534
	CMSGSetFactionNotAtWar = uint16(0x34DF) // 13535
	CMSGSetFactionInactive = uint16(0x34E0) // 13536
	CMSGSetWatchedFaction  = uint16(0x34E1) // 13537

	// Server -> client: AzerothCore only reveals factions that were hidden
	// until the player earned reputation with them.
	SMSGSetFactionVisible = uint16(0x272A) // 10026
)

// ParseFactionAtWar reads the 3.4.3 one-byte reputation-list index sent by the
// at-war checkbox. 3.3.5a folds the on/off state into a trailing bool of the
// single CMSG_SET_FACTION_ATWAR opcode, so the checkbox pair is merged there.
func ParseFactionAtWar(body []byte) (uint8, error) {
	if len(body) != 1 {
		return 0, fmt.Errorf("faction-at-war has %d bytes, want 1", len(body))
	}
	return body[0], nil
}

type FactionInactiveChange struct {
	Index    uint32
	Inactive bool
}

// ParseFactionInactive reads the 32-bit reputation-list index plus the single
// inactive state bit that follow it in the build-54261 stream.
func ParseFactionInactive(body []byte) (FactionInactiveChange, error) {
	var change FactionInactiveChange
	r := movementReader{data: body}
	var err error
	if change.Index, err = r.u32(); err != nil {
		return change, fmt.Errorf("read faction-inactive index: %w", err)
	}
	if change.Inactive, err = r.bit(); err != nil {
		return change, fmt.Errorf("read faction-inactive state: %w", err)
	}
	r.align()
	if r.remaining() != 0 {
		return change, fmt.Errorf("faction-inactive has %d trailing bytes", r.remaining())
	}
	return change, nil
}

// ParseWatchedFaction reads the 32-bit reputation-list index of the faction
// whose progress bar is pinned to the character frame.
func ParseWatchedFaction(body []byte) (uint32, error) {
	if len(body) != 4 {
		return 0, fmt.Errorf("watched-faction has %d bytes, want 4", len(body))
	}
	return binary.LittleEndian.Uint32(body), nil
}

// EncodeLegacyFactionAtWar writes the WotLK CMSG_SET_FACTION_ATWAR body: the
// 32-bit reputation-list index followed by the at-war on/off byte. The single
// 3.3.5a opcode serves both the 3.4.3 at-war and not-at-war requests.
func EncodeLegacyFactionAtWar(index uint32, atWar bool) []byte {
	body := binary.LittleEndian.AppendUint32(nil, index)
	if atWar {
		return append(body, 1)
	}
	return append(body, 0)
}

// EncodeLegacyFactionInactive writes the WotLK CMSG_SET_FACTION_INACTIVE body:
// 32-bit reputation-list index followed by the inactive state byte.
func EncodeLegacyFactionInactive(index uint32, inactive bool) []byte {
	body := binary.LittleEndian.AppendUint32(nil, index)
	if inactive {
		return append(body, 1)
	}
	return append(body, 0)
}

// EncodeLegacyWatchedFaction writes the four-byte WotLK
// CMSG_SET_WATCHED_FACTION reputation-list index.
func EncodeLegacyWatchedFaction(index uint32) []byte {
	return binary.LittleEndian.AppendUint32(nil, index)
}

// ParseLegacyFactionVisible reads AzerothCore's four-byte ReputationListID
// carried by SMSG_SET_FACTION_VISIBLE.
func ParseLegacyFactionVisible(body []byte) (uint32, error) {
	if len(body) != 4 {
		return 0, fmt.Errorf("faction-visible has %d bytes, want 4", len(body))
	}
	return binary.LittleEndian.Uint32(body), nil
}

// EncodeFactionVisible re-emits the same 32-bit reputation-list index with the
// build-54261 SMSG_SET_FACTION_VISIBLE opcode.
func EncodeFactionVisible(index uint32) []byte {
	return binary.LittleEndian.AppendUint32(nil, index)
}
