package modernworld

import (
	"encoding/binary"
	"slices"
	"testing"
)

func TestSharedPartyOrderWireAcrossRecipients(t *testing.T) {
	members := []uint64{7, 3, 1}
	names := map[uint64]string{7: "Leader", 3: "Member", 1: "Newcomer"}
	for viewer, selfGUID := range members {
		for _, moved := range []bool{false, true} {
			groupOf := func(guid uint64) byte {
				if moved && guid == 7 {
					return 2
				}
				return 0
			}
			body := []byte{2, groupOf(selfGUID), 0, 0}
			body = binary.LittleEndian.AppendUint64(body, 100)
			body = binary.LittleEndian.AppendUint32(body, 9)
			body = binary.LittleEndian.AppendUint32(body, 2)
			for _, guid := range []uint64{1, 3, 7} {
				if guid == selfGUID {
					continue
				}
				body = append(body, names[guid]...)
				body = append(body, 0)
				body = binary.LittleEndian.AppendUint64(body, guid)
				body = append(body, 1, groupOf(guid), 0, 0)
			}
			body = binary.LittleEndian.AppendUint64(body, 3) // promoted leader stays in place
			body = append(body, 0)
			body = binary.LittleEndian.AppendUint64(body, 0)
			body = append(body, 2, 0, 1, 2)
			resolve := func(guid uint64) PartyPlayer {
				return PartyPlayer{GUID: ModernGUIDForLegacy(guid, 0), Name: names[guid]}
			}
			called := false
			choose := func(group, leader uint64, got []uint64) []uint64 {
				called = true
				if group != 100 || leader != 3 || !slices.Equal(got, []uint64{1, 3, 7}) {
					t.Fatalf("callback=%d/%d/%v", group, leader, got)
				}
				return slices.Clone(members)
			}
			update, order, err := TranslateLegacyGroupListWithOrder(body, resolve(selfGUID), selfGUID, 1, resolve, choose)
			if err != nil {
				t.Fatal(err)
			}
			gotNames, groups, myIndex := decodePartyUpdateNames(t, update)
			if !called || !slices.Equal(order, members) || myIndex != int32(viewer) || !slices.Equal(gotNames, []string{"Leader", "Member", "Newcomer"}) || groups[0] != groupOf(7) {
				t.Fatalf("order=%v names=%v groups=%v self=%d", order, gotNames, groups, myIndex)
			}
			called = false
			_, _, err = TranslateLegacyGroupListWithOrder(append(body, 0), resolve(selfGUID), selfGUID, 1, resolve, choose)
			if err == nil || called {
				t.Fatal("invalid packet changed shared order")
			}
		}
	}
}
