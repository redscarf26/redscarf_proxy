package modernworld

import (
	"encoding/binary"
	"testing"
)

func TestTaxiClientRequests(t *testing.T) {
	modernGUID := appendPackedGUID128(nil, 0x43, 1)
	requestBody := append([]byte(nil), modernGUID...)
	requestBody = binary.LittleEndian.AppendUint32(requestBody, 12)
	requestBody = binary.LittleEndian.AppendUint32(requestBody, 100)
	requestBody = binary.LittleEndian.AppendUint32(requestBody, 200)
	request, err := ParseActivateTaxi(requestBody)
	if err != nil || request.FlightMaster.Low != 0x43 || request.Destination != 12 || request.FlyingMountID != 200 {
		t.Fatalf("activate request=%#v err=%v", request, err)
	}
	legacy := EncodeLegacyActivateTaxi(0xf130000001000043, 2, 12)
	if len(legacy) != 16 || binary.LittleEndian.Uint32(legacy[8:]) != 2 || binary.LittleEndian.Uint32(legacy[12:]) != 12 {
		t.Fatalf("legacy activate=%x", legacy)
	}
	express := EncodeLegacyActivateTaxiExpress(0x43, []uint32{2, 6, 12})
	if len(express) != 24 || binary.LittleEndian.Uint32(express[8:]) != 3 || binary.LittleEndian.Uint32(express[20:]) != 12 {
		t.Fatalf("legacy express=%x", express)
	}
}

func TestTaxiServerPackets(t *testing.T) {
	legacy := binary.LittleEndian.AppendUint32(nil, 1)
	legacy = binary.LittleEndian.AppendUint64(legacy, 0xf130000001000043)
	legacy = binary.LittleEndian.AppendUint32(legacy, 2)
	mask := make([]byte, modernTaxiMaskBytes)
	mask[0] = 0x22
	legacy = append(legacy, mask...)
	menu, err := ParseLegacyTaxiMenu(legacy)
	if err != nil {
		t.Fatal(err)
	}
	body := EncodeShowTaxiNodes(menu, GUID128{Low: 0x43, High: 1})
	if body[0] != 0x80 || binary.LittleEndian.Uint32(body[1:]) != 7 || binary.LittleEndian.Uint32(body[5:]) != 7 {
		t.Fatalf("modern taxi menu bytes=%d body=%x", len(body), body[:13])
	}
	_, _, guidBytes, err := readPackedGUID128(body[9:])
	if err != nil || len(body) != 1+8+guidBytes+4+112 || binary.LittleEndian.Uint32(body[9+guidBytes:]) != 2 || body[13+guidBytes] != 0x22 || body[13+guidBytes+56] != 0x22 {
		t.Fatalf("modern taxi menu guidBytes=%d err=%v", guidBytes, err)
	}
	status := EncodeTaxiNodeStatus(GUID128{Low: 0x43, High: 1}, true)
	_, _, consumed, _ := readPackedGUID128(status)
	if status[consumed] != 0x40 {
		t.Fatalf("learned status=%x", status)
	}
	if reply := EncodeActivateTaxiReply(3); len(reply) != 1 || reply[0] != 0x30 {
		t.Fatalf("activate reply=%x", reply)
	}
}

func TestShortestTaxiRoute(t *testing.T) {
	usable := make([]byte, modernTaxiMaskBytes)
	for _, node := range []uint32{2, 4, 5, 6, 7, 8, 12} {
		usable[(node-1)/8] |= 1 << ((node - 1) % 8)
	}
	route, err := ShortestTaxiRoute(12, 8, usable)
	if err != nil || len(route) < 3 || route[0] != 12 || route[len(route)-1] != 8 {
		t.Fatalf("route=%v err=%v", route, err)
	}
}
