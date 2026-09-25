package proxy

import (
	"time"

	"redscarf/internal/legacyworld"
	"redscarf/internal/modernworld"
)

// 3.4.3 sends the next CMSG_CAST_SPELL during the last ~400ms of recovery.
// 3.3.5 AzerothCore has no spell queue: a same-spell CMSG during a read-cast
// interrupts it, and a split MoveUpdate heartbeat can too. The first player
// CMSG_CAST_SPELL is forwarded immediately. A later CMSG for the same spell is
// held without its heartbeat until SPELL_GO or a terminal CAST_FAILED, then
// released once. Unstarted NOT_READY failures may be retried once in flight
// until SpellQueueRetryLifetime. RetryDeadline is an admission cutoff: it
// never emits CAST_FAILED by itself. A NOT_READY that answers an in-flight
// retry inside the fuse is delivered; ignoring it leaves the CastID
// unresolved. A held cast replaced before it is sent gets SpellPrepare and
// CAST_FAILED locally.
const (
	SpellQueueRetryLifetime = 1500 * time.Millisecond
	spellQueueRetryFuse     = 50 * time.Millisecond

	spellFailedDontReport      uint16 = 27
	spellFailedInterrupted     uint16 = 40
	spellFailedNotReady        uint16 = 67
	spellFailedNoPower         uint16 = 53
	spellFailedSpellInProgress uint16 = 105
)

type spellQueueClass int

const (
	queueUnknown spellQueueClass = iota
	queueNormalSpell
	queueOffGCD
)

type SpellQueueAttempt struct {
	Generation    uint64
	SpellID       uint32
	ClientCastID  modernworld.GUID128
	CastBody      []byte
	CreatedAt     time.Time
	RetryDeadline time.Time
	Started       bool
	RetryInFlight bool
	LastRetryAt   time.Time
	RetrySeq      uint32
	SettledSeq    uint32
	Class         spellQueueClass
	needRetry     bool
}

type heldPlayerCast struct {
	SpellID      uint32
	ClientCastID modernworld.GUID128
	CastBody     []byte
}

type spellQueue struct {
	active            *SpellQueueAttempt
	held              *heldPlayerCast
	generation        uint64
	retiredSpellID    uint32
	retiredGeneration uint64
	staleUntil        time.Time
	staleSpellID      uint32
	abandoned         []modernworld.PendingCast
	nowFn             func() time.Time
	manualRetry       bool
}

type playerCastForward struct {
	castBody []byte
	held     bool
}

func (queue *spellQueue) now() time.Time {
	if queue.nowFn != nil {
		return queue.nowFn()
	}
	return time.Now()
}

func (session *proxySession) enqueuePlayerCastLocked(pending modernworld.PendingCast, castBody []byte) playerCastForward {
	now := session.spellQueue.now()
	if session.spellQueue.active != nil && session.spellQueue.active.ClientCastID != pending.ClientCastID {
		old := session.spellQueue.active
		if old.SpellID == pending.SpellID {
			session.replaceHeldLocked(pending, castBody)
			return playerCastForward{held: true}
		}
		session.discardHeldLocked(true)
		session.spellQueue.retiredSpellID = old.SpellID
		session.spellQueue.retiredGeneration = old.Generation
		session.spellQueue.staleSpellID = old.SpellID
		session.spellQueue.staleUntil = now.Add(spellQueueRetryFuse)
		// Keep the previous ClientCastID in pendingCasts. 3.4.3 paints a
		// yellow ring from CMSG_CAST_SPELL; SpellPrepare/GO/CAST_FAILED must
		// still see that ID. legacy proxy's SpellQueue is keyed by spell ID, so
		// competing instants (hunter sting/arcane/steady) stay mapped.
		// Started read-casts were already preserved; unstarted competing
		// casts need the same treatment.
	}
	session.discardHeldLocked(true)
	session.spellQueue.generation++
	session.spellQueue.active = &SpellQueueAttempt{
		Generation:    session.spellQueue.generation,
		SpellID:       pending.SpellID,
		ClientCastID:  pending.ClientCastID,
		CastBody:      append([]byte(nil), castBody...),
		CreatedAt:     now,
		RetryDeadline: now.Add(SpellQueueRetryLifetime),
		RetryInFlight: true,
		LastRetryAt:   now,
		RetrySeq:      1,
		Class:         queueNormalSpell,
	}
	return playerCastForward{castBody: castBody}
}

func (session *proxySession) replaceHeldLocked(pending modernworld.PendingCast, castBody []byte) {
	if session.spellQueue.held != nil && session.spellQueue.held.ClientCastID != pending.ClientCastID {
		session.discardHeldLocked(true)
	}
	session.spellQueue.held = &heldPlayerCast{
		SpellID:      pending.SpellID,
		ClientCastID: pending.ClientCastID,
		CastBody:     append([]byte(nil), castBody...),
	}
}

// failClient is false when the 3.4.3 client already dismissed the cast
// (cancel, or the session is going away). Every other drop was never sent
// to 3.3.5, so the yellow ring stays up unless we synthesize CAST_FAILED.
func (session *proxySession) discardHeldLocked(failClient bool) {
	if session.spellQueue.held == nil {
		return
	}
	clientCastID := session.spellQueue.held.ClientCastID
	session.spellQueue.held = nil
	pending, ok := session.removePendingCastLocked(clientCastID)
	if ok && failClient {
		session.abandonCastLocked(pending)
	}
}

func (session *proxySession) promoteHeldLocked() []byte {
	held := session.spellQueue.held
	session.spellQueue.held = nil
	if held == nil || !session.pendingUnstartedLocked(held.ClientCastID) {
		return nil
	}
	now := session.spellQueue.now()
	session.spellQueue.generation++
	session.spellQueue.active = &SpellQueueAttempt{
		Generation:    session.spellQueue.generation,
		SpellID:       held.SpellID,
		ClientCastID:  held.ClientCastID,
		CastBody:      append([]byte(nil), held.CastBody...),
		CreatedAt:     now,
		RetryDeadline: now.Add(SpellQueueRetryLifetime),
		RetryInFlight: true,
		LastRetryAt:   now,
		RetrySeq:      1,
		Class:         queueNormalSpell,
	}
	return append([]byte(nil), held.CastBody...)
}

func (session *proxySession) dropPendingCastLocked(clientCastID modernworld.GUID128) {
	session.removePendingCastLocked(clientCastID)
}

func (session *proxySession) removePendingCastLocked(clientCastID modernworld.GUID128) (modernworld.PendingCast, bool) {
	for index, pending := range session.pendingCasts {
		if pending.ClientCastID != clientCastID {
			continue
		}
		session.pendingCasts = append(session.pendingCasts[:index], session.pendingCasts[index+1:]...)
		return pending, true
	}
	return modernworld.PendingCast{}, false
}

func (session *proxySession) abandonCastLocked(pending modernworld.PendingCast) {
	if pending.PetGUID != 0 {
		return
	}
	if session.spellQueue.active != nil && session.spellQueue.active.ClientCastID == pending.ClientCastID {
		session.spellQueue.generation++
		session.spellQueue.active = nil
	}
	session.spellQueue.abandoned = append(session.spellQueue.abandoned, pending)
}

func (session *proxySession) trimPendingCastsLocked(limit int) {
	for len(session.pendingCasts) >= limit {
		pending := session.pendingCasts[0]
		session.pendingCasts = session.pendingCasts[1:]
		if session.spellQueue.held != nil && session.spellQueue.held.ClientCastID == pending.ClientCastID {
			session.spellQueue.held = nil
		}
		session.abandonCastLocked(pending)
	}
}

func (session *proxySession) takeAbandonedCastPacketsLocked() []modernworld.Packet {
	if len(session.spellQueue.abandoned) == 0 {
		return nil
	}
	packets := make([]modernworld.Packet, 0, len(session.spellQueue.abandoned)*2)
	for _, pending := range session.spellQueue.abandoned {
		if !pending.Started {
			packets = append(packets, modernworld.Packet{
				Opcode: modernworld.SMSGSpellPrepare,
				Body:   modernworld.EncodeSpellPrepare(pending.ClientCastID, pending.ServerCastID),
			})
		}
		info := modernworld.SpellFailureInfo{SpellID: pending.SpellID, Reason: spellFailedSpellInProgress, Arg1: -1, Arg2: -1}
		packets = append(packets, modernworld.Packet{
			Opcode: modernworld.SMSGCastFailed,
			Body:   modernworld.EncodeCastFailed(pending.ServerCastID, info, pending.VisualID),
		})
	}
	session.spellQueue.abandoned = nil
	return packets
}

func (session *proxySession) writeInstancePackets(packets []modernworld.Packet) error {
	for _, packet := range packets {
		if err := session.sendInstance(packet); err != nil {
			return err
		}
	}
	return nil
}

func retryableCastFailure(reason uint16) bool {
	return reason == spellFailedNotReady
}

func (session *proxySession) dropRetiredFailureLocked(spellID uint32, reason uint16) bool {
	if reason != spellFailedNotReady {
		return false
	}
	if session.spellQueue.retiredSpellID != spellID {
		return false
	}
	if session.pendingUnstartedSpellLocked(spellID) {
		return false
	}
	active := session.spellQueue.active
	if active == nil {
		return true
	}
	if active.ClientCastID == (modernworld.GUID128{}) {
		return true
	}
	if active.SpellID != spellID {
		return true
	}
	now := session.spellQueue.now()
	return active.RetrySeq == 1 && active.RetryInFlight && now.Before(session.spellQueue.staleUntil) && session.spellQueue.staleSpellID == spellID
}

func (session *proxySession) attemptMatchesFailureLocked(spellID uint32) (*SpellQueueAttempt, modernworld.PendingCast, bool) {
	active := session.spellQueue.active
	if active == nil {
		return nil, modernworld.PendingCast{}, false
	}
	pending, found := session.matchPlayerCastFailureLocked(spellID, false)
	if found && active.ClientCastID == pending.ClientCastID {
		return active, pending, true
	}
	if !found && active.SpellID == spellID {
		return active, modernworld.PendingCast{}, true
	}
	return nil, modernworld.PendingCast{}, false
}

func (session *proxySession) swallowCastFailedLocked(spellID uint32, reason uint16) bool {
	if session.dropRetiredFailureLocked(spellID, reason) {
		return true
	}
	active, pending, found := session.attemptMatchesFailureLocked(spellID)
	if !found {
		return session.dropRetiredFailureLocked(spellID, reason)
	}
	if pending.ClientCastID != (modernworld.GUID128{}) && pending.Started {
		active.Started = true
	}
	if !retryableCastFailure(reason) {
		return false
	}
	if active.Started {
		active.needRetry = false
		return true
	}
	now := session.spellQueue.now()
	if active.SettledSeq >= active.RetrySeq {
		return true
	}
	// Settle this send. A 3.3.5 NOT_READY often returns inside the 50ms fuse;
	// treating that packet as a duplicate left RetryInFlight set, so the
	// CastID never received CAST_FAILED and the next same-spell START/GO
	// could bind to it.
	answeredRetry := active.RetryInFlight && active.RetrySeq > 1 && now.Sub(active.LastRetryAt) < spellQueueRetryFuse
	active.SettledSeq = active.RetrySeq
	active.RetryInFlight = false
	if answeredRetry || !now.Before(active.RetryDeadline) {
		active.needRetry = false
		return false
	}
	active.needRetry = true
	return true
}

func (session *proxySession) spellQueueRetryBodyLocked() []byte {
	return session.spellQueueRetryBodyForceLocked(false)
}

func (session *proxySession) spellQueueRetryBodyForceLocked(force bool) []byte {
	active := session.spellQueue.active
	if active == nil || active.Started || active.RetryInFlight {
		return nil
	}
	if session.spellQueue.manualRetry && !force {
		return nil
	}
	if !active.needRetry {
		return nil
	}
	now := session.spellQueue.now()
	if !now.Before(active.RetryDeadline) {
		return nil
	}
	if !session.pendingUnstartedLocked(active.ClientCastID) {
		return nil
	}
	active.needRetry = false
	active.RetryInFlight = true
	active.LastRetryAt = now
	active.RetrySeq++
	return append([]byte(nil), active.CastBody...)
}

func (session *proxySession) markSpellQueueStartedLocked(pending modernworld.PendingCast) {
	active := session.spellQueue.active
	if active == nil || pending.PetGUID != 0 || active.ClientCastID != pending.ClientCastID {
		return
	}
	active.Started = true
	active.RetryInFlight = false
	active.needRetry = false
	if active.SettledSeq < active.RetrySeq {
		active.SettledSeq = active.RetrySeq
	}
}

func (session *proxySession) completeSpellQueueCastLocked(pending modernworld.PendingCast) []byte {
	active := session.spellQueue.active
	if active == nil || pending.PetGUID != 0 || active.ClientCastID != pending.ClientCastID {
		return nil
	}
	session.spellQueue.generation++
	session.spellQueue.active = nil
	return session.promoteHeldLocked()
}

func (session *proxySession) failSpellQueueLocked(pending modernworld.PendingCast) []byte {
	active := session.spellQueue.active
	if active == nil || pending.PetGUID != 0 {
		return nil
	}
	if active.ClientCastID != pending.ClientCastID && active.SpellID != pending.SpellID {
		return nil
	}
	session.spellQueue.generation++
	session.spellQueue.active = nil
	return session.promoteHeldLocked()
}

func queueCastMatches(cast *SpellQueueAttempt, clientCastID modernworld.GUID128, spellID uint32) bool {
	if cast == nil {
		return false
	}
	if clientCastID != (modernworld.GUID128{}) {
		return cast.ClientCastID == clientCastID
	}
	return spellID != 0 && cast.SpellID == spellID
}

func (session *proxySession) resetSpellQueueLocked() {
	session.discardHeldLocked(false)
	session.spellQueue.generation++
	session.spellQueue.active = nil
	session.spellQueue.retiredSpellID = 0
	session.spellQueue.retiredGeneration = 0
	session.spellQueue.staleSpellID = 0
	session.spellQueue.staleUntil = time.Time{}
}

func (session *proxySession) cancelSpellQueueLocked(clientCastID modernworld.GUID128, spellID uint32) bool {
	heldOnly := session.spellQueue.held != nil && session.spellQueue.held.ClientCastID == clientCastID && !queueCastMatches(session.spellQueue.active, clientCastID, spellID)
	if heldOnly {
		session.discardHeldLocked(false)
		session.dropPendingCastLocked(clientCastID)
		return false
	}
	if queueCastMatches(session.spellQueue.active, clientCastID, spellID) {
		session.spellQueue.retiredSpellID = session.spellQueue.active.SpellID
		session.spellQueue.retiredGeneration = session.spellQueue.active.Generation
		session.dropPendingCastLocked(session.spellQueue.active.ClientCastID)
		session.spellQueue.generation++
		session.spellQueue.active = nil
		session.discardHeldLocked(true)
	}
	session.dropPendingCastLocked(clientCastID)
	return true
}

func (session *proxySession) pendingUnstartedLocked(clientCastID modernworld.GUID128) bool {
	for _, pending := range session.pendingCasts {
		if pending.ClientCastID == clientCastID {
			return !pending.Started
		}
	}
	return false
}

func (session *proxySession) pendingUnstartedSpellLocked(spellID uint32) bool {
	for _, pending := range session.pendingCasts {
		if pending.PetGUID == 0 && pending.SpellID == spellID && !pending.Started {
			return true
		}
	}
	return false
}

func (session *proxySession) retrySpellQueue(generation uint64) bool {
	session.worldMu.Lock()
	if session.spellQueue.generation != generation {
		session.worldMu.Unlock()
		return false
	}
	castBody := session.spellQueueRetryBodyForceLocked(true)
	legacyConn := session.legacyWorld
	session.worldMu.Unlock()
	if castBody == nil || legacyConn == nil {
		return false
	}
	if err := legacyConn.WritePacket(legacyworld.CMSGCastSpell, castBody); err != nil {
		return false
	}
	return true
}
