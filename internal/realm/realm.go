// Package realm translates the 3.3.5a authentication realm list into the
// compressed JSON blobs consumed by the 3.4.3 GameUtilities service.
package realm

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"redscarf/internal/legacyauth"
)

const (
	SubRegion     = "1-1-0"
	realmBase     = uint32(0x01010000)
	realmOffline  = byte(0x02)
	versionMajor  = 3
	versionMinor  = 4
	versionBugfix = 3
)

type clientVersion struct {
	Major    int `json:"versionMajor"`
	Build    int `json:"versionBuild"`
	Minor    int `json:"versionMinor"`
	Revision int `json:"versionRevision"`
}

type realmEntry struct {
	Address         int           `json:"wowRealmAddress"`
	TimezonesID     int           `json:"cfgTimezonesID"`
	PopulationState int           `json:"populationState"`
	CategoriesID    int           `json:"cfgCategoriesID"`
	Version         clientVersion `json:"version"`
	RealmsID        int           `json:"cfgRealmsID"`
	Flags           int           `json:"flags"`
	Name            string        `json:"name"`
	ConfigsID       int           `json:"cfgConfigsID"`
	LanguagesID     int           `json:"cfgLanguagesID"`
}

type realmListUpdate struct {
	Update   realmEntry `json:"update"`
	Deleting bool       `json:"deleting"`
}

type realmListUpdates struct {
	Updates []realmListUpdate `json:"updates"`
}

type characterCountEntry struct {
	Address int `json:"wowRealmAddress"`
	Count   int `json:"count"`
}

type characterCountList struct {
	Counts []characterCountEntry `json:"counts"`
}

type serverAddress struct {
	IP   string `json:"ip"`
	Port int    `json:"port"`
}

type addressFamily struct {
	Family    int             `json:"family"`
	Addresses []serverAddress `json:"addresses"`
}

type serverAddresses struct {
	Families []addressFamily `json:"families"`
}

func Address(id uint32) uint32 {
	return realmBase | (id & 0xffff)
}

func EncodeList(realms []legacyauth.Realm, subRegion string, clientBuild uint32) ([]byte, error) {
	updates := realmListUpdates{Updates: make([]realmListUpdate, 0, len(realms))}
	if subRegion != "" && subRegion != SubRegion {
		return deflateJSON("JSONRealmListUpdates", updates)
	}
	for _, legacy := range realms {
		flags := legacy.Flags
		population := int(legacy.Population)
		if flags&realmOffline != 0 {
			population = 0
		} else if population < 1 {
			population = 1
		}
		configID := int(legacy.Type) + 1
		if legacy.Type >= 14 {
			configID = 1
		}
		updates.Updates = append(updates.Updates, realmListUpdate{
			Update: realmEntry{
				Address:         int(Address(legacy.ID)),
				TimezonesID:     1,
				PopulationState: population,
				CategoriesID:    int(legacy.Timezone),
				Version: clientVersion{
					Major:    versionMajor,
					Build:    int(clientBuild),
					Minor:    versionMinor,
					Revision: versionBugfix,
				},
				RealmsID:    int(legacy.ID),
				Flags:       int(flags),
				Name:        legacy.Name,
				ConfigsID:   configID,
				LanguagesID: 1,
			},
		})
	}
	return deflateJSON("JSONRealmListUpdates", updates)
}

func EncodeCharacterCounts(realms []legacyauth.Realm) ([]byte, error) {
	counts := characterCountList{Counts: make([]characterCountEntry, 0, len(realms))}
	for _, legacy := range realms {
		counts.Counts = append(counts.Counts, characterCountEntry{
			Address: int(Address(legacy.ID)),
			Count:   int(legacy.CharacterCount),
		})
	}
	return deflateJSON("JSONRealmCharacterCountList", counts)
}

func EncodeServerAddresses(ip string, port int) ([]byte, error) {
	addresses := serverAddresses{Families: []addressFamily{{
		Family:    1,
		Addresses: []serverAddress{{IP: ip, Port: port}},
	}}}
	return deflateJSON("JSONRealmListServerIPAddresses", addresses)
}

func deflateJSON(name string, value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	plain := make([]byte, 0, len(name)+len(encoded)+2)
	plain = append(plain, name...)
	plain = append(plain, ':')
	plain = append(plain, encoded...)
	plain = append(plain, 0)

	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(plain); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	result := make([]byte, 4+compressed.Len())
	binary.LittleEndian.PutUint32(result, uint32(len(plain)))
	copy(result[4:], compressed.Bytes())
	return result, nil
}

// InflateJSON is used by golden-flow auditing and tests. It validates the
// uncompressed-size prefix used by Blizzard's protobuf JSON blobs.
func InflateJSON(blob []byte) (string, []byte, error) {
	if len(blob) < 4 {
		return "", nil, fmt.Errorf("compressed JSON blob is too short")
	}
	wantSize := binary.LittleEndian.Uint32(blob[:4])
	zr, err := zlib.NewReader(bytes.NewReader(blob[4:]))
	if err != nil {
		return "", nil, err
	}
	plain, readErr := io.ReadAll(io.LimitReader(zr, 16<<20))
	closeErr := zr.Close()
	if readErr != nil {
		return "", nil, readErr
	}
	if closeErr != nil {
		return "", nil, closeErr
	}
	if uint32(len(plain)) != wantSize {
		return "", nil, fmt.Errorf("compressed JSON size %d, want %d", len(plain), wantSize)
	}
	if len(plain) == 0 || plain[len(plain)-1] != 0 {
		return "", nil, fmt.Errorf("compressed JSON lacks terminator")
	}
	plain = plain[:len(plain)-1]
	separator := bytes.IndexByte(plain, ':')
	if separator <= 0 {
		return "", nil, fmt.Errorf("compressed JSON lacks type prefix")
	}
	name := string(plain[:separator])
	if strings.TrimSpace(name) == "" {
		return "", nil, fmt.Errorf("compressed JSON type is empty")
	}
	return name, append([]byte(nil), plain[separator+1:]...), nil
}
