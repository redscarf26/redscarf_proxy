package modernworld

import (
	"encoding/binary"
	"fmt"
)

const (
	maxPlayerNameQueries   = 32
	maxDeclinedNameCases   = 5
	playerGUIDType         = uint64(2)
	queryPlayerNameFailure = byte(1)
	queryPlayerNameSuccess = byte(0)
)

type LegacyNameIdentity struct {
	GUID  uint64
	Name  string
	Race  byte
	Sex   byte
	Class byte
}

func ParseQueryPlayerNames(body []byte) ([]GUID128, error) {
	r := movementReader{data: body}
	count, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read player-name count: %w", err)
	}
	if count == 0 || count > maxPlayerNameQueries {
		return nil, fmt.Errorf("player-name query count %d is out of range", count)
	}
	guids := make([]GUID128, 0, count)
	for index := uint32(0); index < count; index++ {
		guid, readErr := r.guid128()
		if readErr != nil {
			return nil, fmt.Errorf("read player-name GUID %d: %w", index, readErr)
		}
		guids = append(guids, guid)
	}
	return guids, nil
}

func EncodeLegacyNameQuery(guid uint64) []byte {
	return binary.LittleEndian.AppendUint64(nil, guid)
}

func LegacyPlayerGUIDFromModern(guid GUID128) (uint64, bool) {
	if guid.Low == 0 && guid.High == 0 {
		return 0, true
	}
	if guid.High>>58 != playerGUIDType {
		return 0, false
	}
	return uint64(uint32(guid.Low)), true
}

func EncodeQueryPlayerNameFailure(guid GUID128) []byte {
	body := binary.LittleEndian.AppendUint32(nil, 1)
	body = append(body, queryPlayerNameFailure)
	body = appendPackedGUID128(body, guid.Low, guid.High)
	bits := newBitWriter(body)
	bits.writeBit(false)
	bits.writeBit(false)
	return bits.flush()
}

// LegacyNameQueryIdentity extracts the part of SMSG_NAME_QUERY_RESPONSE used
// by chat and party caches. TranslateNameQueryResponse remains the strict,
// full packet validator; callers should only cache this result after that
// translation succeeds.
func LegacyNameQueryIdentity(legacy []byte) (guid uint64, name string, found bool, err error) {
	r := movementReader{data: legacy}
	if guid, err = r.guid64(); err != nil {
		return 0, "", false, fmt.Errorf("read name-query GUID: %w", err)
	}
	fail, err := r.u8()
	if err != nil {
		return 0, "", false, fmt.Errorf("read name-query result: %w", err)
	}
	if fail != 0 {
		return guid, "", false, nil
	}
	name, err = r.cstring()
	if err != nil {
		return 0, "", false, fmt.Errorf("read player name: %w", err)
	}
	if name == "" {
		return 0, "", false, fmt.Errorf("player name is empty")
	}
	return guid, name, true, nil
}

func ParseLegacyNameIdentity(legacy []byte) (LegacyNameIdentity, bool, error) {
	var identity LegacyNameIdentity
	r := movementReader{data: legacy}
	var err error
	if identity.GUID, err = r.guid64(); err != nil {
		return identity, false, fmt.Errorf("read name-query GUID: %w", err)
	}
	fail, err := r.u8()
	if err != nil {
		return identity, false, fmt.Errorf("read name-query result: %w", err)
	}
	if fail != 0 {
		if r.remaining() != 0 {
			return identity, false, fmt.Errorf("failed name-query has %d trailing bytes", r.remaining())
		}
		return identity, false, nil
	}
	if identity.Name, err = r.cstring(); err != nil {
		return identity, false, fmt.Errorf("read player name: %w", err)
	}
	if _, err = r.cstring(); err != nil { // realm
		return identity, false, fmt.Errorf("read player realm: %w", err)
	}
	if identity.Race, err = r.u8(); err != nil {
		return identity, false, fmt.Errorf("read player race: %w", err)
	}
	if identity.Sex, err = r.u8(); err != nil {
		return identity, false, fmt.Errorf("read player sex: %w", err)
	}
	if identity.Class, err = r.u8(); err != nil {
		return identity, false, fmt.Errorf("read player class: %w", err)
	}
	if r.remaining() > 0 {
		hasDeclined, readErr := r.u8()
		if readErr != nil || hasDeclined > 1 {
			return identity, false, fmt.Errorf("read declined-name flag")
		}
		if hasDeclined != 0 {
			for index := 0; index < maxDeclinedNameCases; index++ {
				if _, err = r.cstring(); err != nil {
					return identity, false, fmt.Errorf("read declined name %d: %w", index, err)
				}
			}
		}
	}
	if r.remaining() != 0 {
		return identity, false, fmt.Errorf("name-query identity has %d trailing bytes", r.remaining())
	}
	return identity, true, nil
}

func TranslateNameQueryResponse(legacy []byte, realmAddress uint32) ([]byte, error) {
	return TranslateNameQueryResponseWithAccounts(legacy, realmAddress, nil, nil)
}

// TranslateNameQueryResponseWithAccounts is the 3.4.3 name-cache response
// variant used when the proxy knows the account identities for a player. The
// client uses those two GUIDs when resolving a name passed to SetLootMethod;
// leaving them empty makes the name visible in the party frame but unusable as
// a loot master target.
func TranslateNameQueryResponseWithAccounts(legacy []byte, realmAddress uint32,
	wowAccountResolver func(uint64) GUID128, bnetAccountResolver func(uint64) GUID128) ([]byte, error) {
	return TranslateNameQueryResponseForGUID(legacy, realmAddress, GUID128{}, wowAccountResolver, bnetAccountResolver)
}

func TranslateNameQueryResponseForGUID(legacy []byte, realmAddress uint32, override GUID128,
	wowAccountResolver func(uint64) GUID128, bnetAccountResolver func(uint64) GUID128) ([]byte, error) {
	r := movementReader{data: legacy}
	legacyGUID, err := r.guid64()
	if err != nil {
		return nil, fmt.Errorf("read name-query GUID: %w", err)
	}
	fail, err := r.u8()
	if err != nil {
		return nil, fmt.Errorf("read name-query result: %w", err)
	}
	guid := ModernGUIDForLegacy(legacyGUID, 0)
	if override.Low != 0 || override.High != 0 {
		guid = override
	}
	if fail != 0 {
		if r.remaining() != 0 {
			return nil, fmt.Errorf("failed name-query has %d trailing bytes", r.remaining())
		}
		return EncodeQueryPlayerNameFailure(guid), nil
	}
	name, err := r.cstring()
	if err != nil {
		return nil, fmt.Errorf("read player name: %w", err)
	}
	if _, err = r.cstring(); err != nil {
		return nil, fmt.Errorf("read realm name: %w", err)
	}
	race, err := r.u8()
	if err != nil {
		return nil, fmt.Errorf("read race: %w", err)
	}
	sex, err := r.u8()
	if err != nil {
		return nil, fmt.Errorf("read sex: %w", err)
	}
	classID, err := r.u8()
	if err != nil {
		return nil, fmt.Errorf("read class: %w", err)
	}
	var declined [maxDeclinedNameCases]string
	if r.remaining() > 0 {
		hasDeclined, readErr := r.u8()
		if readErr != nil {
			return nil, fmt.Errorf("read declined-name flag: %w", readErr)
		}
		if hasDeclined != 0 {
			for index := range declined {
				declined[index], err = r.cstring()
				if err != nil {
					return nil, fmt.Errorf("read declined name %d: %w", index, err)
				}
			}
		}
	}
	if r.remaining() != 0 {
		return nil, fmt.Errorf("name-query has %d trailing bytes", r.remaining())
	}
	if name == "" {
		return nil, fmt.Errorf("player name is empty")
	}
	var wowAccount, bnetAccount GUID128
	if wowAccountResolver != nil {
		wowAccount = wowAccountResolver(legacyGUID)
	}
	if bnetAccountResolver != nil {
		bnetAccount = bnetAccountResolver(legacyGUID)
	}
	return encodeQueryPlayerNameSuccess(guid, wowAccount, bnetAccount, realmAddress, name, race, sex, classID, declined), nil
}

// EncodeQueryPlayerNameIdentity populates the build-54261 name cache from an
// identity already known to the proxy, for example from character enumeration.
// This is useful for party members because the client must resolve the roster
// name back to the same player GUID before it will send name-based group
// commands such as SetLootMethod.
func EncodeQueryPlayerNameIdentity(identity LegacyNameIdentity, level byte, realmAddress uint32, wowAccount, bnetAccount GUID128) ([]byte, error) {
	return EncodeQueryPlayerNameLookup(ModernGUIDForLegacy(identity.GUID, 0), identity, level, realmAddress, wowAccount, bnetAccount)
}

// EncodeQueryPlayerNameLookup writes a one-entry SMSG_QUERY_PLAYER_NAMES_RESPONSE
// keyed by the GUID128 the modern client actually asked about. Rebuilding that
// GUID from the legacy 64-bit id can miss High-bit details and leave the name
// cache empty, which build 54261 renders as "Unknown Target".
func EncodeQueryPlayerNameLookup(guid GUID128, identity LegacyNameIdentity, level byte, realmAddress uint32, wowAccount, bnetAccount GUID128) ([]byte, error) {
	if guid.Low == 0 && guid.High == 0 {
		return nil, fmt.Errorf("player lookup GUID is empty")
	}
	if identity.Name == "" {
		return nil, fmt.Errorf("player identity name is empty")
	}
	if level == 0 {
		level = 1
	}
	return encodeQueryPlayerNameSuccessWithLevel(guid, wowAccount, bnetAccount, realmAddress,
		identity.Name, identity.Race, identity.Sex, identity.Class, level, [maxDeclinedNameCases]string{}), nil
}

func encodeQueryPlayerNameSuccess(guid, wowAccount, bnetAccount GUID128, realmAddress uint32, name string, race, sex, classID byte, declined [maxDeclinedNameCases]string) []byte {
	return encodeQueryPlayerNameSuccessWithLevel(guid, wowAccount, bnetAccount, realmAddress, name, race, sex, classID, 1, declined)
}

func encodeQueryPlayerNameSuccessWithLevel(guid, wowAccount, bnetAccount GUID128, realmAddress uint32, name string, race, sex, classID, level byte, declined [maxDeclinedNameCases]string) []byte {
	body := binary.LittleEndian.AppendUint32(nil, 1)
	body = append(body, queryPlayerNameSuccess)
	body = appendPackedGUID128(body, guid.Low, guid.High)
	bits := newBitWriter(body)
	bits.writeBit(true)  // HasPlayerGuidLookupData
	bits.writeBit(false) // HasNameCacheUnused920
	body = bits.flush()

	lookup := newBitWriter(body)
	lookup.writeBit(false) // IsDeleted
	lookup.writeBits(uint32(len(name)), 6)
	for _, declinedName := range declined {
		lookup.writeBits(uint32(len(declinedName)), 7)
	}
	body = lookup.flush()
	for _, declinedName := range declined {
		body = append(body, declinedName...)
	}
	body = appendPackedGUID128(body, wowAccount.Low, wowAccount.High)   // AccountID
	body = appendPackedGUID128(body, bnetAccount.Low, bnetAccount.High) // BnetAccountID
	body = appendPackedGUID128(body, guid.Low, guid.High)
	body = binary.LittleEndian.AppendUint64(body, 0) // GuildClubMemberID
	body = binary.LittleEndian.AppendUint32(body, realmAddress)
	body = append(body, race, sex, classID, level, 0) // race/sex/class/level/unused
	return append(body, name...)
}
