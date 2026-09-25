package proxy

import "redscarf/internal/modernworld"

// AzerothCore marks server-triggered instant casts with CAST_FLAG_PENDING.
const legacyCastFlagPending uint32 = 0x1

func pendingSpellID(p modernworld.PendingCast) uint32 {
	if p.ServerSpellID != 0 {
		return p.ServerSpellID
	}
	return p.SpellID
}

func playerPending(p modernworld.PendingCast) bool { return p.PetGUID == 0 }

// Exact identity is shared by completion, failure and combat-log lookups.
// Visual IDs and queue position never establish a cross-spell relationship.
func (session *proxySession) findPendingCastForLocked(spellID uint32, start bool, accepts func(modernworld.PendingCast) bool) (int, bool) {
	if index, ok := session.findActiveAttemptLocked(spellID, start, accepts); ok {
		return index, true
	}
	unstarted := -1
	for index, p := range session.pendingCasts {
		if !accepts(p) || pendingSpellID(p) != spellID {
			continue
		}
		if p.Started {
			if !start {
				return index, true
			}
		} else if unstarted < 0 {
			unstarted = index
		}
	}
	return unstarted, unstarted >= 0
}

// The server packet answers the cast we sent. That is the active attempt.
// An older unstarted pending of the same spell (a hold whose NOT_READY was
// swallowed, or a mash that never left the proxy) must not take the START/GO.
func (session *proxySession) findActiveAttemptLocked(spellID uint32, start bool, accepts func(modernworld.PendingCast) bool) (int, bool) {
	active := session.spellQueue.active
	if active == nil || active.SpellID != spellID {
		return 0, false
	}
	index := -1
	for i, pending := range session.pendingCasts {
		if pending.ClientCastID == active.ClientCastID && accepts(pending) && pendingSpellID(pending) == spellID {
			index = i
			break
		}
	}
	if index < 0 {
		return 0, false
	}
	pending := session.pendingCasts[index]
	if start {
		if pending.Started {
			return 0, false
		}
		return index, true
	}
	if pending.Started {
		return index, true
	}
	for _, other := range session.pendingCasts {
		if accepts(other) && pendingSpellID(other) == spellID && other.Started {
			return 0, false
		}
	}
	return index, true
}

// Only an unambiguous, unstarted, player spell may acquire a downrank alias.
// Identical repeats retain FIFO order, just as exact-ID requests do.
// START additionally verifies the target; CAST_FAILED has no target field.
// GO and damage logs may use an established alias but cannot create one.
func (session *proxySession) findDownrankCastLocked(spellID uint32, target *uint64) (int, bool) {
	found := -1
	for index, p := range session.pendingCasts {
		if p.PetGUID != 0 || p.CastItemGUID != 0 || p.Started || p.ServerSpellID != 0 || !modernworld.IsLowerSpellRank(p.SpellID, spellID) {
			continue
		}
		if target != nil && (p.TargetGUID == 0 || p.TargetGUID != *target) {
			continue
		}
		if found >= 0 {
			first := session.pendingCasts[found]
			if first.SpellID != p.SpellID || first.TargetGUID != p.TargetGUID {
				return 0, false
			}
			continue
		}
		found = index
	}
	return found, found >= 0
}

func (session *proxySession) usePendingCastLocked(index int, spellID uint32, start, consume bool) modernworld.PendingCast {
	if start {
		session.pendingCasts[index].Started = true
		session.pendingCasts[index].ServerSpellID = spellID
		return session.pendingCasts[index]
	}
	pending := session.pendingCasts[index]
	if consume {
		session.pendingCasts = append(session.pendingCasts[:index], session.pendingCasts[index+1:]...)
		if pending.PetGUID == 0 {
			session.rememberCompletedCastLocked(pending.SpellID, pending.ServerCastID)
			if spellID != pending.SpellID {
				session.rememberCompletedCastLocked(spellID, pending.ServerCastID)
			}
		}
	}
	return pending
}

func (session *proxySession) matchPlayerCastFailureLocked(spellID uint32, consume bool) (modernworld.PendingCast, bool) {
	index, ok := session.findPendingCastLocked(spellID, false)
	if !ok {
		index, ok = session.findDownrankCastLocked(spellID, nil)
	}
	if !ok {
		return modernworld.PendingCast{}, false
	}
	return session.usePendingCastLocked(index, spellID, false, consume), true
}

func (session *proxySession) matchPetCastFailureLocked(spellID uint32) (modernworld.PendingCast, bool) {
	// Legacy PET_CAST_FAILED carries no caster. Restrict it to pet requests;
	// it must never acknowledge a player request with the same spell ID.
	index, ok := session.findPendingCastForLocked(spellID, false, func(p modernworld.PendingCast) bool { return p.PetGUID != 0 })
	if !ok {
		return modernworld.PendingCast{}, false
	}
	return session.usePendingCastLocked(index, spellID, false, true), true
}
