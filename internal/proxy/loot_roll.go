package proxy

import "redscarf/internal/modernworld"

// Item IDs are not identities: different corpses may drop the same item in
// the same slot while both roll dialogs remain open.
type lootRollIdentity struct {
	object modernworld.GUID128
	slot   uint8
}

func (session *proxySession) legacyLootRollRequestLocked(request modernworld.LootRollRequest) (uint64, bool) {
	id := lootRollIdentity{request.LootObj, request.Slot}
	if reference, ok := session.lootRolls[id]; ok {
		return reference.legacyGUID, false
	}
	for _, completed := range session.completedLootRolls {
		if completed == id {
			return 0, true
		}
	}
	return 0, false
}

func (session *proxySession) completeLootRollLocked(reference lootRollReference) {
	id := lootRollIdentity{reference.modernGUID, reference.itemKey.Slot}
	delete(session.lootRolls, id)
	// Retain a bounded set for late client responses after a roll timeout or
	// completion. Unknown GUIDs still fail validation and are never forwarded.
	const maxCompleted = 256
	if len(session.completedLootRolls) >= maxCompleted {
		copy(session.completedLootRolls, session.completedLootRolls[1:])
		session.completedLootRolls = session.completedLootRolls[:maxCompleted-1]
	}
	session.completedLootRolls = append(session.completedLootRolls, id)
}
