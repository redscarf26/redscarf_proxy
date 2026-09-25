package modernworld

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"strings"
)

const (
	CMSGWho = uint16(0x3683)
	SMSGWho = uint16(0x2BAE)
)

const maxWhoResults = 63

type WhoRequest struct {
	MinLevel    int32
	MaxLevel    int32
	RaceFilter  uint64
	ClassFilter int32
	Name        string
	Guild       string
	Words       []string
	Areas       []int32
	RequestID   uint32
	Origin      byte
	FromAddon   bool
}

type WhoEntry struct {
	Name      string
	GuildName string
	Level     byte
	ClassID   byte
	RaceID    byte
	Sex       byte
	AreaID    int32
}

func ParseWhoRequest(body []byte) (WhoRequest, error) {
	var request WhoRequest
	r := movementReader{data: body}
	areasCount, err := r.bits(4)
	if err != nil {
		return request, fmt.Errorf("read who area count: %w", err)
	}
	request.FromAddon, err = r.bit()
	if err != nil {
		return request, fmt.Errorf("read who addon flag: %w", err)
	}
	minLevel, err := r.i32()
	if err != nil {
		return request, fmt.Errorf("read who minimum level: %w", err)
	}
	maxLevel, err := r.i32()
	if err != nil {
		return request, fmt.Errorf("read who maximum level: %w", err)
	}
	races, err := r.u64()
	if err != nil {
		return request, fmt.Errorf("read who race filter: %w", err)
	}
	classes, err := r.i32()
	if err != nil {
		return request, fmt.Errorf("read who class filter: %w", err)
	}
	nameLength, err := r.bits(6)
	if err != nil {
		return request, fmt.Errorf("read who name length: %w", err)
	}
	virtualRealmLength, err := r.bits(9)
	if err != nil {
		return request, fmt.Errorf("read who virtual-realm length: %w", err)
	}
	guildLength, err := r.bits(7)
	if err != nil {
		return request, fmt.Errorf("read who guild length: %w", err)
	}
	guildRealmLength, err := r.bits(9)
	if err != nil {
		return request, fmt.Errorf("read who guild-realm length: %w", err)
	}
	wordsCount, err := r.bits(3)
	if err != nil {
		return request, fmt.Errorf("read who word count: %w", err)
	}
	for range 3 { // ShowEnemies, ShowArenaPlayers, ExactName
		if _, err = r.bit(); err != nil {
			return request, fmt.Errorf("read who option: %w", err)
		}
	}
	hasServerInfo, err := r.bit()
	if err != nil {
		return request, fmt.Errorf("read who server-info flag: %w", err)
	}
	r.align()
	request.Words = make([]string, 0, wordsCount)
	for index := uint32(0); index < wordsCount; index++ {
		length, readErr := r.bits(7)
		if readErr != nil {
			return request, fmt.Errorf("read who word %d length: %w", index, readErr)
		}
		word, readErr := r.stringN(int(length))
		if readErr != nil {
			return request, fmt.Errorf("read who word %d: %w", index, readErr)
		}
		request.Words = append(request.Words, word)
	}
	request.Name, err = r.stringN(int(nameLength))
	if err != nil {
		return request, fmt.Errorf("read who name: %w", err)
	}
	if _, err = r.stringN(int(virtualRealmLength)); err != nil {
		return request, fmt.Errorf("read who virtual realm: %w", err)
	}
	request.Guild, err = r.stringN(int(guildLength))
	if err != nil {
		return request, fmt.Errorf("read who guild: %w", err)
	}
	if _, err = r.stringN(int(guildRealmLength)); err != nil {
		return request, fmt.Errorf("read who guild realm: %w", err)
	}
	if hasServerInfo {
		if _, err = r.i32(); err != nil {
			return request, fmt.Errorf("read who server faction: %w", err)
		}
		if _, err = r.i32(); err != nil {
			return request, fmt.Errorf("read who server locale: %w", err)
		}
		if _, err = r.u32(); err != nil {
			return request, fmt.Errorf("read who server realm: %w", err)
		}
	}
	request.RequestID, err = r.u32()
	if err != nil {
		return request, fmt.Errorf("read who request ID: %w", err)
	}
	request.Origin, err = r.u8()
	if err != nil {
		return request, fmt.Errorf("read who origin: %w", err)
	}
	request.Areas = make([]int32, areasCount)
	for index := range request.Areas {
		request.Areas[index], err = r.i32()
		if err != nil {
			return request, fmt.Errorf("read who area %d: %w", index, err)
		}
	}
	if r.remaining() != 0 {
		return request, fmt.Errorf("who request has %d trailing bytes", r.remaining())
	}
	request.MinLevel = minLevel
	request.MaxLevel = maxLevel
	request.RaceFilter = races
	request.ClassFilter = classes
	return request, nil
}

func EncodeLegacyWhoRequest(request WhoRequest) []byte {
	body := binary.LittleEndian.AppendUint32(nil, uint32(request.MinLevel))
	body = binary.LittleEndian.AppendUint32(body, uint32(request.MaxLevel))
	body = append(body, request.Name...)
	body = append(body, 0)
	body = append(body, request.Guild...)
	body = append(body, 0)
	body = binary.LittleEndian.AppendUint32(body, uint32(request.RaceFilter))
	body = binary.LittleEndian.AppendUint32(body, uint32(request.ClassFilter))
	body = binary.LittleEndian.AppendUint32(body, uint32(len(request.Areas)))
	for _, area := range request.Areas {
		body = binary.LittleEndian.AppendUint32(body, uint32(area))
	}
	body = binary.LittleEndian.AppendUint32(body, uint32(len(request.Words)))
	for _, word := range request.Words {
		body = append(body, word...)
		body = append(body, 0)
	}
	return body
}

func ParseLegacyWhoResponse(body []byte) ([]WhoEntry, error) {
	r := movementReader{data: body}
	count, err := r.u32()
	if err != nil {
		return nil, fmt.Errorf("read who result count: %w", err)
	}
	if count > 1024 {
		return nil, fmt.Errorf("who result count %d is out of range", count)
	}
	if _, err = r.u32(); err != nil { // total online count
		return nil, fmt.Errorf("read who online count: %w", err)
	}
	entries := make([]WhoEntry, count)
	for index := range entries {
		entry := &entries[index]
		entry.Name, err = readLegacyCString(&r, 63)
		if err != nil {
			return nil, fmt.Errorf("read who player %d name: %w", index, err)
		}
		entry.GuildName, err = readLegacyCString(&r, 127)
		if err != nil {
			return nil, fmt.Errorf("read who player %d guild: %w", index, err)
		}
		level, readErr := r.u32()
		if readErr != nil {
			return nil, fmt.Errorf("read who player %d level: %w", index, readErr)
		}
		classID, readErr := r.u32()
		if readErr != nil {
			return nil, fmt.Errorf("read who player %d class: %w", index, readErr)
		}
		raceID, readErr := r.u32()
		if readErr != nil {
			return nil, fmt.Errorf("read who player %d race: %w", index, readErr)
		}
		entry.Sex, err = r.u8()
		if err != nil {
			return nil, fmt.Errorf("read who player %d sex: %w", index, err)
		}
		entry.AreaID, err = r.i32()
		if err != nil {
			return nil, fmt.Errorf("read who player %d area: %w", index, err)
		}
		entry.Level = byte(level)
		entry.ClassID = byte(classID)
		entry.RaceID = byte(raceID)
	}
	if r.remaining() != 0 {
		return nil, fmt.Errorf("who response has %d trailing bytes", r.remaining())
	}
	return entries, nil
}

func EncodeWhoResponse(entries []WhoEntry, requestID, realmAddress uint32) []byte {
	if len(entries) > maxWhoResults {
		entries = entries[:maxWhoResults]
	}
	body := binary.LittleEndian.AppendUint32(nil, requestID)
	bits := newBitWriter(body)
	bits.writeBits(uint32(len(entries)), 6)
	body = bits.flush()
	for _, entry := range entries {
		name := truncateWireString(entry.Name, 63)
		guild := truncateWireString(entry.GuildName, 127)
		guid := modernPlayerGUIDForName(name)
		lookup := newBitWriter(body)
		lookup.writeBit(false)
		lookup.writeBits(uint32(len(name)), 6)
		for range maxDeclinedNameCases {
			lookup.writeBits(0, 7)
		}
		body = lookup.flush()
		body = appendPackedGUID128(body, 0, 0) // account GUID unavailable
		body = appendPackedGUID128(body, 0, 0) // Battle.net account GUID unavailable
		body = appendPackedGUID128(body, guid.Low, guid.High)
		body = binary.LittleEndian.AppendUint64(body, 0)
		body = binary.LittleEndian.AppendUint32(body, realmAddress)
		body = append(body, entry.RaceID, entry.Sex, entry.ClassID, entry.Level, 0)
		body = append(body, name...)
		body = appendPackedGUID128(body, 0, 0) // guild GUID unavailable
		guildRealm := uint32(0)
		if guild != "" {
			guildRealm = realmAddress
		}
		body = binary.LittleEndian.AppendUint32(body, guildRealm)
		body = binary.LittleEndian.AppendUint32(body, uint32(entry.AreaID))
		guildBits := newBitWriter(body)
		guildBits.writeBits(uint32(len(guild)), 7)
		guildBits.writeBit(false)
		body = guildBits.flush()
		body = append(body, guild...)
	}
	return body
}

func modernPlayerGUIDForName(name string) GUID128 {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(strings.ToLower(name)))
	low := hash.Sum32()
	if low == 0 {
		low = 1
	}
	return ModernGUIDForLegacy(uint64(low), 0)
}

func truncateWireString(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	return value[:maximum]
}
