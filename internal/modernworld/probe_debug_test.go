package modernworld

import "testing"

func TestStealthProbeInjectsSitWithoutObjectSection(t *testing.T) {
	stealthProbe = true
	defer func() { stealthProbe = false }()

	changed := map[int]uint32{legacyUnitBytes1: 0x20000}
	merged := map[int]uint32{legacyUnitBytes1: 0x20000}
	_, gotMerged := applyStealthProbe(changed, merged)
	if gotMerged[legacyUnitBytes1]&0xff != 1 {
		t.Fatalf("stand state = %#x", gotMerged[legacyUnitBytes1])
	}

	body, _, err := EncodeValuesUpdate(LegacyObjectUpdate{
		Type: LegacyUpdateValues, GUID: 0x42,
		Values: LegacyUpdateValuesBlock{Fields: changed},
	}, ValuesUpdateOptions{
		ObjectType: 4, Active: true, Fields: merged,
		GUID: GUID128{Low: 0x42, High: uint64(2) << 58},
	})
	if err != nil {
		t.Fatal(err)
	}
	values := valuesUpdatePayload(t, body)
	if got := binaryLittleEndianUint32(values); got != 0x20 {
		t.Fatalf("changed mask = 0x%x, want unit-only 0x20", got)
	}
}

func binaryLittleEndianUint32(values []byte) uint32 {
	return uint32(values[0]) | uint32(values[1])<<8 | uint32(values[2])<<16 | uint32(values[3])<<24
}
