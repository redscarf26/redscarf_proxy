package modernworld

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestNextMailTimeTranslation(t *testing.T) {
	if err := ParseQueryNextMailTime(nil); err != nil {
		t.Fatal(err)
	}
	legacy := binary.LittleEndian.AppendUint32(nil, math.Float32bits(0))
	legacy = binary.LittleEndian.AppendUint32(legacy, 1)
	legacy = binary.LittleEndian.AppendUint64(legacy, 0x42)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 0)
	legacy = binary.LittleEndian.AppendUint32(legacy, 41)
	legacy = binary.LittleEndian.AppendUint32(legacy, math.Float32bits(15))
	result, err := ParseLegacyNextMailTime(legacy)
	if err != nil || len(result.Entries) != 1 || result.Entries[0].SenderGUID != 0x42 || result.Entries[0].TimeLeft != 15 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	body := EncodeNextMailTime(result, func(guid uint64) GUID128 { return GUID128{Low: guid, High: 1} })
	_, _, consumed, err := readPackedGUID128(body[8:])
	if err != nil || binary.LittleEndian.Uint32(body[8+consumed:12+consumed]) != math.Float32bits(15) {
		t.Fatalf("modern next mail=%x err=%v", body, err)
	}
}
