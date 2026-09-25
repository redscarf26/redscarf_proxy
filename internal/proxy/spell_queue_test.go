package proxy

import (
	"bytes"
	"encoding/binary"
	"io"
	"log/slog"
	"testing"
	"time"

	"redscarf/internal/legacyauth"
	"redscarf/internal/legacyworld"
	"redscarf/internal/modernworld"
)

func queueSession(t *testing.T) (*Server, *proxySession, *fakeLegacyWorldConnection, *time.Time) {
	t.Helper()
	legacy := newFakeLegacyWorldConnection()
	now := time.Unix(1_700_000_000, 0)
	nowPtr := &now
	session := &proxySession{
		legacy:           &legacyauth.Session{Username: "TEST"},
		legacyWorld:      legacy,
		currentCharacter: 7,
		currentMapID:     530,
		objectGUIDs:      map[uint64]modernworld.GUID128{7: modernworld.ModernGUIDForLegacy(7, 530)},
		spellQueue: spellQueue{
			manualRetry: true,
			nowFn:       func() time.Time { return *nowPtr },
		},
	}
	return &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}, session, legacy, nowPtr
}

func TestSpellQueueForwardsOverlappingCast(t *testing.T) {
	_, session, _, _ := queueSession(t)
	first := pendingForTest(133, 1)
	session.pendingCasts = []modernworld.PendingCast{first}
	if got := session.enqueuePlayerCastLocked(first, []byte{1}); !bytes.Equal(got.castBody, []byte{1}) {
		t.Fatalf("first cast forward=%#v", got)
	}

	second := pendingForTest(686, 2)
	session.pendingCasts = append(session.pendingCasts, second)
	got := session.enqueuePlayerCastLocked(second, []byte{9})
	if !bytes.Equal(got.castBody, []byte{9}) {
		t.Fatalf("overlapping cast was not forwarded: %#v", got)
	}
	if session.spellQueue.active == nil || session.spellQueue.active.ClientCastID != second.ClientCastID {
		t.Fatal("active slot did not track the latest request")
	}
	if session.spellQueue.active.SpellID != 686 {
		t.Fatal("replaced slot kept the previous spell")
	}
	if len(session.pendingCasts) != 2 {
		t.Fatalf("competing unstarted pending was dropped: %#v", session.pendingCasts)
	}
	if session.pendingCasts[0].ClientCastID != first.ClientCastID || session.pendingCasts[1].ClientCastID != second.ClientCastID {
		t.Fatal("competing casts lost a ClientCastID")
	}
}

func TestSpellQueueKeepsStartedPendingOnCompetingCast(t *testing.T) {
	_, session, _, _ := queueSession(t)
	fireball := pendingForTest(143, 1)
	session.pendingCasts = []modernworld.PendingCast{fireball}
	session.enqueuePlayerCastLocked(fireball, []byte{1})
	session.pendingCasts[0].Started = true
	session.markSpellQueueStartedLocked(session.pendingCasts[0])

	iceArmor := pendingForTest(168, 2)
	session.pendingCasts = append(session.pendingCasts, iceArmor)
	session.enqueuePlayerCastLocked(iceArmor, []byte{2})
	if session.spellQueue.active == nil || session.spellQueue.active.ClientCastID != iceArmor.ClientCastID {
		t.Fatal("ice armor did not become the active attempt")
	}
	if len(session.pendingCasts) != 2 {
		t.Fatalf("started fireball pending was dropped: %#v", session.pendingCasts)
	}
	if session.pendingCasts[0].ClientCastID != fireball.ClientCastID || !session.pendingCasts[0].Started {
		t.Fatal("started fireball pending must survive ice armor")
	}

	pending, found := session.matchPlayerCastFailureLocked(143, false)
	if !found || pending.ClientCastID != fireball.ClientCastID {
		t.Fatal("fireball GO/INTERRUPTED cannot match the original CastID")
	}
	consumed, found := session.matchPlayerCastFailureLocked(143, true)
	if !found || consumed.ClientCastID != fireball.ClientCastID {
		t.Fatal("started fireball was not consumable after ice armor")
	}
	session.completeSpellQueueCastLocked(consumed)
	if session.spellQueue.active == nil || session.spellQueue.active.ClientCastID != iceArmor.ClientCastID {
		t.Fatal("fireball GO cleared the ice armor attempt")
	}
	if len(session.pendingCasts) != 1 || session.pendingCasts[0].ClientCastID != iceArmor.ClientCastID {
		t.Fatal("ice armor pending was consumed with fireball")
	}
}

func TestSpellQueueStartedCastPrefersGOOverQueuedRepeat(t *testing.T) {
	_, session, _, _ := queueSession(t)
	first := pendingForTest(143, 1)
	session.pendingCasts = []modernworld.PendingCast{first}
	session.enqueuePlayerCastLocked(first, []byte{1})
	session.pendingCasts[0].Started = true
	session.markSpellQueueStartedLocked(session.pendingCasts[0])

	queued := pendingForTest(143, 2)
	session.pendingCasts = append(session.pendingCasts, queued)
	session.enqueuePlayerCastLocked(queued, []byte{2})
	if session.spellQueue.held == nil || session.spellQueue.held.ClientCastID != queued.ClientCastID {
		t.Fatal("same-spell mash was not held")
	}
	if session.spellQueue.active == nil || session.spellQueue.active.ClientCastID != first.ClientCastID {
		t.Fatal("started fireball lost the exclusive slot")
	}
	if len(session.pendingCasts) != 2 {
		t.Fatal("queued next fireball dropped the started cast")
	}
	pending, found := session.matchPlayerCastFailureLocked(143, true)
	if !found || pending.ClientCastID != first.ClientCastID || !pending.Started {
		t.Fatal("SPELL_GO matched the queued repeat instead of the started cast")
	}
	if len(session.pendingCasts) != 1 || session.pendingCasts[0].ClientCastID != queued.ClientCastID {
		t.Fatal("queued fireball pending did not survive the previous GO")
	}
	if body := session.completeSpellQueueCastLocked(pending); !bytes.Equal(body, []byte{2}) {
		t.Fatalf("GO did not release the held fireball: %#v", body)
	}
	if session.spellQueue.active == nil || session.spellQueue.active.ClientCastID != queued.ClientCastID {
		t.Fatal("held fireball was not promoted after GO")
	}
}

func TestSpellQueueSwallowsNotReadyAndRetries(t *testing.T) {
	_, session, legacy, _ := queueSession(t)
	first := pendingForTest(133, 1)
	session.pendingCasts = []modernworld.PendingCast{first}
	session.enqueuePlayerCastLocked(first, []byte{1})
	if !session.swallowCastFailedLocked(133, spellFailedNotReady) {
		t.Fatal("NOT_READY should be swallowed in the queue window")
	}
	if len(session.pendingCasts) != 1 {
		t.Fatal("swallowed failure consumed the pending request")
	}
	if !session.retrySpellQueue(session.spellQueue.generation) {
		t.Fatal("retry stopped while the request is still unstarted")
	}
	write := legacy.nextWrite(t)
	if write.opcode != legacyworld.CMSGCastSpell || !bytes.Equal(write.body, []byte{1}) {
		t.Fatalf("retry did not resend CMSG_CAST_SPELL: %#v", write)
	}
	if len(legacy.writes) != 0 {
		t.Fatalf("retry wrote extra packets: %#v", <-legacy.writes)
	}
}

func TestSpellQueueDoesNotSwallowDontReport(t *testing.T) {
	_, session, _, _ := queueSession(t)
	first := pendingForTest(133, 1)
	session.pendingCasts = []modernworld.PendingCast{first}
	session.enqueuePlayerCastLocked(first, []byte{1})
	if session.swallowCastFailedLocked(133, spellFailedDontReport) {
		t.Fatal("DONT_REPORT should reach the 3.4.3 client")
	}
}

func TestSpellQueueDoesNotSwallowNoMana(t *testing.T) {
	_, session, _, _ := queueSession(t)
	first := pendingForTest(133, 1)
	session.pendingCasts = []modernworld.PendingCast{first}
	session.enqueuePlayerCastLocked(first, []byte{1})
	if session.swallowCastFailedLocked(133, spellFailedNoPower) {
		t.Fatal("NO_MANA should reach the 3.4.3 client immediately")
	}
	if session.spellQueueRetryBodyForceLocked(true) != nil {
		t.Fatal("non-retryable failure queued a retry")
	}
}

func TestSpellQueueDoesNotSwallowSpellInProgress(t *testing.T) {
	_, session, _, _ := queueSession(t)
	first := pendingForTest(133, 1)
	session.pendingCasts = []modernworld.PendingCast{first}
	session.enqueuePlayerCastLocked(first, []byte{1})
	if session.swallowCastFailedLocked(133, spellFailedSpellInProgress) {
		t.Fatal("SPELL_IN_PROGRESS should reach the 3.4.3 client")
	}
}

func TestSpellQueueForwardsInterruptAfterStart(t *testing.T) {
	_, session, _, _ := queueSession(t)
	first := pendingForTest(133, 1)
	session.pendingCasts = []modernworld.PendingCast{first}
	session.enqueuePlayerCastLocked(first, []byte{1})
	session.pendingCasts[0].Started = true
	session.markSpellQueueStartedLocked(session.pendingCasts[0])
	if session.swallowCastFailedLocked(133, spellFailedInterrupted) {
		t.Fatal("started interrupt should reach the 3.4.3 client")
	}
	pending, found := session.matchPlayerCastFailureLocked(133, true)
	if !found {
		t.Fatal("INTERRUPTED should consume the started pending request")
	}
	session.failSpellQueueLocked(pending)
	if session.spellQueue.active != nil {
		t.Fatal("terminal interrupt left the attempt active")
	}
}

func TestSpellQueueStartBarrierDropsStaleNotReady(t *testing.T) {
	_, session, _, _ := queueSession(t)
	first := pendingForTest(133, 1)
	session.pendingCasts = []modernworld.PendingCast{first}
	session.enqueuePlayerCastLocked(first, []byte{1})
	session.pendingCasts[0].Started = true
	session.markSpellQueueStartedLocked(session.pendingCasts[0])
	if session.spellQueue.active == nil || !session.spellQueue.active.Started {
		t.Fatal("START should keep the attempt as a success barrier")
	}
	if !session.swallowCastFailedLocked(133, spellFailedNotReady) {
		t.Fatal("stale NOT_READY after START should be swallowed")
	}
	if len(session.pendingCasts) != 1 {
		t.Fatal("stale NOT_READY consumed the started pending request")
	}
	if session.spellQueueRetryBodyForceLocked(true) != nil {
		t.Fatal("START barrier queued a retry")
	}
}

func TestSpellQueueCancelActive(t *testing.T) {
	_, session, _, _ := queueSession(t)
	first := pendingForTest(133, 1)
	session.pendingCasts = []modernworld.PendingCast{first}
	session.enqueuePlayerCastLocked(first, []byte{1})
	session.cancelSpellQueueLocked(first.ClientCastID, 133)
	if session.spellQueue.active != nil {
		t.Fatal("active slot survived cancel")
	}
	if len(session.pendingCasts) != 0 {
		t.Fatal("cancel left the pending request")
	}
}

func TestSpellQueueDuplicateNotReadyOneInFlight(t *testing.T) {
	_, session, legacy, _ := queueSession(t)
	first := pendingForTest(133, 1)
	session.pendingCasts = []modernworld.PendingCast{first}
	session.enqueuePlayerCastLocked(first, []byte{1})
	retries := 0
	delivered := 0
	for i := 0; i < 25; i++ {
		if session.swallowCastFailedLocked(133, spellFailedNotReady) {
			if session.retrySpellQueue(session.spellQueue.generation) {
				retries++
			}
			continue
		}
		delivered++
	}
	if retries != 1 {
		t.Fatalf("duplicate NOT_READY sent %d retries, want 1", retries)
	}
	if delivered != 1 {
		t.Fatalf("fast NOT_READY of the in-flight retry was swallowed %d extra times", delivered)
	}
	write := legacy.nextWrite(t)
	if write.opcode != legacyworld.CMSGCastSpell || !bytes.Equal(write.body, []byte{1}) {
		t.Fatalf("retry did not resend CMSG_CAST_SPELL: %#v", write)
	}
	if len(legacy.writes) != 0 {
		t.Fatalf("retry wrote extra packets: %#v", <-legacy.writes)
	}
	if session.spellQueue.active == nil || session.spellQueue.active.RetryInFlight {
		t.Fatal("the retry's NOT_READY left the attempt in flight")
	}
	pending, found := session.matchPlayerCastFailureLocked(133, true)
	if !found || pending.ClientCastID != first.ClientCastID {
		t.Fatal("fast NOT_READY could not fail the original CastID")
	}
	session.failSpellQueueLocked(pending)
	if session.spellQueue.active != nil || len(session.pendingCasts) != 0 {
		t.Fatal("fast NOT_READY left the CastID pending")
	}
}

func TestSpellQueueDeadlineKeepsInFlightRetry(t *testing.T) {
	_, session, legacy, now := queueSession(t)
	first := pendingForTest(133, 1)
	session.pendingCasts = []modernworld.PendingCast{first}
	session.enqueuePlayerCastLocked(first, []byte{1})
	*now = now.Add(1400 * time.Millisecond)
	if !session.swallowCastFailedLocked(133, spellFailedNotReady) {
		t.Fatal("NOT_READY before the deadline should be swallowed")
	}
	if !session.retrySpellQueue(session.spellQueue.generation) {
		t.Fatal("retry was not sent before the deadline")
	}
	write := legacy.nextWrite(t)
	if write.opcode != legacyworld.CMSGCastSpell {
		t.Fatalf("missing in-flight retry: %#v", write)
	}
	*now = now.Add(100 * time.Millisecond)
	if session.spellQueueRetryBodyForceLocked(true) != nil {
		t.Fatal("deadline must not start a new retry")
	}
	if session.spellQueue.active == nil || !session.spellQueue.active.RetryInFlight {
		t.Fatal("deadline cancelled the in-flight retry")
	}
	if len(session.pendingCasts) != 1 {
		t.Fatal("deadline consumed the pending request")
	}
	*now = now.Add(100 * time.Millisecond)
	session.pendingCasts[0].Started = true
	session.markSpellQueueStartedLocked(session.pendingCasts[0])
	if session.spellQueue.active == nil || session.spellQueue.active.ClientCastID != first.ClientCastID {
		t.Fatal("START after an in-flight retry did not match the attempt")
	}
}

func TestSpellQueueForwardsNotReadyAfterDeadlineWithoutInFlight(t *testing.T) {
	_, session, _, now := queueSession(t)
	first := pendingForTest(133, 1)
	session.pendingCasts = []modernworld.PendingCast{first}
	session.enqueuePlayerCastLocked(first, []byte{1})
	*now = now.Add(SpellQueueRetryLifetime + time.Millisecond)
	if session.swallowCastFailedLocked(133, spellFailedNotReady) {
		t.Fatal("real NOT_READY after the deadline should reach the client")
	}
	if session.spellQueue.active == nil || session.spellQueue.active.RetryInFlight {
		t.Fatal("late NOT_READY should settle the in-flight send")
	}
	pending, found := session.matchPlayerCastFailureLocked(133, true)
	if !found {
		t.Fatal("late NOT_READY should consume the pending request")
	}
	session.failSpellQueueLocked(pending)
	if session.spellQueue.active != nil {
		t.Fatal("late NOT_READY left the attempt active")
	}
}

func TestSpellQueueOldGenerationNotReadyDoesNotConsumeNew(t *testing.T) {
	_, session, _, _ := queueSession(t)
	first := pendingForTest(133, 1)
	session.pendingCasts = []modernworld.PendingCast{first}
	session.enqueuePlayerCastLocked(first, []byte{1})
	firstGeneration := session.spellQueue.active.Generation

	second := pendingForTest(686, 2)
	session.pendingCasts = append(session.pendingCasts, second)
	session.enqueuePlayerCastLocked(second, []byte{2})
	if session.spellQueue.active.Generation == firstGeneration {
		t.Fatal("competing cast did not open a new generation")
	}
	if session.swallowCastFailedLocked(133, spellFailedNotReady) {
		t.Fatal("unstarted competing CastID must receive CAST_FAILED")
	}
	if len(session.pendingCasts) != 2 {
		t.Fatalf("competing unstarted pending was dropped: %#v", session.pendingCasts)
	}
	pending, found := session.matchPlayerCastFailureLocked(133, true)
	if !found || pending.ClientCastID != first.ClientCastID {
		t.Fatal("first spell CAST_FAILED could not use the original CastID")
	}
	if body := session.failSpellQueueLocked(pending); body != nil {
		t.Fatal("old generation NOT_READY promoted a held cast")
	}
	if session.spellQueue.active == nil || session.spellQueue.active.ClientCastID != second.ClientCastID {
		t.Fatal("old generation NOT_READY replaced the new attempt")
	}
	if !session.spellQueue.active.RetryInFlight || session.spellQueue.active.SettledSeq != 0 {
		t.Fatal("old generation NOT_READY settled the new in-flight send")
	}
}

func TestSpellQueueRelaySwallowsNotReady(t *testing.T) {
	s, legacy := guildProxyTestServer(), newFakeLegacyWorldConnection()
	first := pendingForTest(133, 1)
	now := time.Unix(1_700_000_000, 0)
	nowPtr := &now
	session := &proxySession{
		legacy:           &legacyauth.Session{Username: "TEST"},
		legacyWorld:      legacy,
		currentCharacter: 7,
		currentMapID:     530,
		pendingCasts:     []modernworld.PendingCast{first},
		spellQueue: spellQueue{
			manualRetry: true,
			nowFn:       func() time.Time { return *nowPtr },
		},
	}
	session.enqueuePlayerCastLocked(first, []byte{1})
	done := make(chan struct{})
	go func() { defer close(done); s.relayLegacyWorld(session, nil, legacy) }()
	defer func() { legacy.Close(); <-done }()
	legacy.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGCastFailed, Body: []byte{1, 133, 0, 0, 0, byte(spellFailedNotReady)}}
	deadline := time.After(2 * time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		session.worldMu.Lock()
		swallowed := session.spellQueue.active != nil && !session.spellQueue.active.RetryInFlight && session.spellQueue.active.SettledSeq >= 1
		forwarded := len(session.pendingInstance)
		pending := len(session.pendingCasts)
		generation := session.spellQueue.generation
		session.worldMu.Unlock()
		if swallowed && forwarded == 0 && pending == 1 {
			if !session.retrySpellQueue(generation) {
				t.Fatal("retry stopped after swallowed NOT_READY")
			}
			write := legacy.nextWrite(t)
			if write.opcode != legacyworld.CMSGCastSpell || !bytes.Equal(write.body, []byte{1}) {
				t.Fatalf("retry did not resend CMSG_CAST_SPELL: %#v", write)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatalf("NOT_READY was not swallowed: forwarded=%d pending=%d", forwarded, pending)
		case <-tick.C:
		}
	}
}

func TestSpellQueueHoldsSameSpellUntilGo(t *testing.T) {
	_, session, _, _ := queueSession(t)
	first := pendingForTest(403, 1)
	session.pendingCasts = []modernworld.PendingCast{first}
	if got := session.enqueuePlayerCastLocked(first, []byte{1}); got.held || !bytes.Equal(got.castBody, []byte{1}) {
		t.Fatalf("first lightning bolt was not forwarded: %#v", got)
	}
	session.pendingCasts[0].Started = true
	session.markSpellQueueStartedLocked(session.pendingCasts[0])

	queued := pendingForTest(403, 2)
	session.pendingCasts = append(session.pendingCasts, queued)
	got := session.enqueuePlayerCastLocked(queued, []byte{2})
	if !got.held || got.castBody != nil {
		t.Fatalf("queued lightning bolt was forwarded during the read-cast: %#v", got)
	}
	if session.spellQueue.active.ClientCastID != first.ClientCastID || !session.spellQueue.active.Started {
		t.Fatal("exclusive slot released the started lightning bolt")
	}
	if session.swallowCastFailedLocked(403, spellFailedInterrupted) {
		t.Fatal("real interrupt of the started bolt must still reach the client")
	}
	if !session.swallowCastFailedLocked(403, spellFailedNotReady) {
		t.Fatal("NOT_READY during the started bolt should stay swallowed")
	}
	if session.spellQueueRetryBodyForceLocked(true) != nil {
		t.Fatal("started bolt queued a retry")
	}

	pending, found := session.matchPlayerCastFailureLocked(403, true)
	if !found || pending.ClientCastID != first.ClientCastID {
		t.Fatal("GO did not consume the started lightning bolt")
	}
	if body := session.completeSpellQueueCastLocked(pending); !bytes.Equal(body, []byte{2}) {
		t.Fatalf("GO did not release the held lightning bolt: %#v", body)
	}
	if session.spellQueue.active == nil || session.spellQueue.active.ClientCastID != queued.ClientCastID || session.spellQueue.active.Started {
		t.Fatal("held lightning bolt was not promoted after GO")
	}
}

func TestSpellQueueKeepsLatestHeldSameSpell(t *testing.T) {
	_, session, _, _ := queueSession(t)
	first := pendingForTest(403, 1)
	session.pendingCasts = []modernworld.PendingCast{first}
	session.enqueuePlayerCastLocked(first, []byte{1})

	second := pendingForTest(403, 2)
	session.pendingCasts = append(session.pendingCasts, second)
	if got := session.enqueuePlayerCastLocked(second, []byte{2}); !got.held {
		t.Fatal("second lightning bolt was not held")
	}

	third := pendingForTest(403, 3)
	session.pendingCasts = append(session.pendingCasts, third)
	if got := session.enqueuePlayerCastLocked(third, []byte{3}); !got.held {
		t.Fatal("third lightning bolt was not held")
	}
	if session.spellQueue.held == nil || session.spellQueue.held.ClientCastID != third.ClientCastID {
		t.Fatal("held slot did not keep the latest mash")
	}
	if len(session.pendingCasts) != 2 {
		t.Fatalf("replaced held pending leaked: %#v", session.pendingCasts)
	}
	if session.pendingCasts[0].ClientCastID != first.ClientCastID || session.pendingCasts[1].ClientCastID != third.ClientCastID {
		t.Fatal("active or latest held pending was dropped")
	}

	session.pendingCasts[0].Started = true
	session.markSpellQueueStartedLocked(session.pendingCasts[0])
	pending, found := session.matchPlayerCastFailureLocked(403, true)
	if !found {
		t.Fatal("started bolt was not completable")
	}
	if body := session.completeSpellQueueCastLocked(pending); !bytes.Equal(body, []byte{3}) {
		t.Fatalf("GO released a stale mash: %#v", body)
	}
}

func TestSpellQueueSameSpellNotReadyStaysOnActive(t *testing.T) {
	_, session, _, _ := queueSession(t)
	first := pendingForTest(403, 1)
	session.pendingCasts = []modernworld.PendingCast{first}
	session.enqueuePlayerCastLocked(first, []byte{1})

	queued := pendingForTest(403, 2)
	session.pendingCasts = append(session.pendingCasts, queued)
	session.enqueuePlayerCastLocked(queued, []byte{2})
	if !session.swallowCastFailedLocked(403, spellFailedNotReady) {
		t.Fatal("NOT_READY of the in-flight bolt leaked after a mash")
	}
	if len(session.pendingCasts) != 2 {
		t.Fatal("NOT_READY consumed a pending after mash")
	}
	if session.spellQueue.active == nil || session.spellQueue.active.ClientCastID != first.ClientCastID {
		t.Fatal("NOT_READY replaced the in-flight bolt with the mash")
	}
	if session.spellQueue.active.SettledSeq != 1 || session.spellQueue.active.RetryInFlight {
		t.Fatal("NOT_READY did not settle the in-flight send")
	}
}

func TestSpellQueueCancelHeldDoesNotInterruptActive(t *testing.T) {
	_, session, _, _ := queueSession(t)
	first := pendingForTest(403, 1)
	session.pendingCasts = []modernworld.PendingCast{first}
	session.enqueuePlayerCastLocked(first, []byte{1})
	session.pendingCasts[0].Started = true
	session.markSpellQueueStartedLocked(session.pendingCasts[0])

	queued := pendingForTest(403, 2)
	session.pendingCasts = append(session.pendingCasts, queued)
	session.enqueuePlayerCastLocked(queued, []byte{2})
	if session.cancelSpellQueueLocked(queued.ClientCastID, 403) {
		t.Fatal("cancel of the held mash was forwarded to 3.3.5")
	}
	if session.spellQueue.active == nil || session.spellQueue.active.ClientCastID != first.ClientCastID {
		t.Fatal("held cancel cleared the started lightning bolt")
	}
	if session.spellQueue.held != nil {
		t.Fatal("held mash survived cancel")
	}
	if len(session.pendingCasts) != 1 || session.pendingCasts[0].ClientCastID != first.ClientCastID {
		t.Fatal("held cancel dropped the started pending")
	}
}

func TestSpellQueuePromotesHeldAfterInterrupt(t *testing.T) {
	_, session, _, _ := queueSession(t)
	first := pendingForTest(403, 1)
	session.pendingCasts = []modernworld.PendingCast{first}
	session.enqueuePlayerCastLocked(first, []byte{1})
	session.pendingCasts[0].Started = true
	session.markSpellQueueStartedLocked(session.pendingCasts[0])

	queued := pendingForTest(403, 2)
	session.pendingCasts = append(session.pendingCasts, queued)
	session.enqueuePlayerCastLocked(queued, []byte{2})
	pending, found := session.matchPlayerCastFailureLocked(403, true)
	if !found || pending.ClientCastID != first.ClientCastID {
		t.Fatal("interrupt did not consume the started bolt")
	}
	if body := session.failSpellQueueLocked(pending); !bytes.Equal(body, []byte{2}) {
		t.Fatalf("interrupt did not release the held bolt: %#v", body)
	}
	if session.spellQueue.active == nil || session.spellQueue.active.ClientCastID != queued.ClientCastID {
		t.Fatal("held bolt was not promoted after interrupt")
	}
}

func TestHunterTripleMashKeepsCastIDs(t *testing.T) {
	server, session, _, _ := queueSession(t)
	sting := pendingForTest(1978, 1)
	arcane := pendingForTest(3044, 2)
	steady := pendingForTest(56641, 3)
	session.pendingCasts = []modernworld.PendingCast{sting}
	session.enqueuePlayerCastLocked(sting, []byte{1})
	session.pendingCasts = append(session.pendingCasts, arcane)
	session.enqueuePlayerCastLocked(arcane, []byte{2})
	session.pendingCasts = append(session.pendingCasts, steady)
	session.enqueuePlayerCastLocked(steady, []byte{3})
	if len(session.pendingCasts) != 3 {
		t.Fatalf("hunter mash dropped CastIDs: %#v", session.pendingCasts)
	}
	if session.spellQueue.active == nil || session.spellQueue.active.ClientCastID != steady.ClientCastID {
		t.Fatal("active slot is not the latest hunter shot")
	}
	if session.swallowCastFailedLocked(1978, spellFailedNotReady) || session.swallowCastFailedLocked(3044, spellFailedNotReady) {
		t.Fatal("retired hunter instants swallowed NOT_READY without clearing the yellow ring")
	}

	if err := server.forwardLegacySpell(session, modernworld.SpellCastData{CasterGUID: 7, CasterUnit: 7, SpellID: 1978}, false); err != nil {
		t.Fatal(err)
	}
	if len(session.pendingInstance) < 2 || session.pendingInstance[0].Opcode != modernworld.SMSGSpellPrepare || session.pendingInstance[1].Opcode != modernworld.SMSGSpellGo {
		t.Fatalf("sting GO packets=%#v", opcodesOf(session.pendingInstance))
	}
	if !bytes.Equal(session.pendingInstance[0].Body, modernworld.EncodeSpellPrepare(sting.ClientCastID, sting.ServerCastID)) {
		t.Fatal("sting GO did not return the original ClientCastID")
	}

	failed, found := session.matchPlayerCastFailureLocked(3044, true)
	if !found || failed.ClientCastID != arcane.ClientCastID {
		t.Fatal("arcane CAST_FAILED lost the original CastID")
	}
	if body := session.failSpellQueueLocked(failed); body != nil {
		t.Fatal("arcane failure promoted a held cast")
	}
	if session.spellQueue.active == nil || session.spellQueue.active.ClientCastID != steady.ClientCastID {
		t.Fatal("arcane failure cleared steady shot")
	}
	if len(session.pendingCasts) != 1 || session.pendingCasts[0].ClientCastID != steady.ClientCastID {
		t.Fatal("steady shot pending did not survive sting GO and arcane failure")
	}
}

func TestHunterMashNotReadySendsPrepareAndFailed(t *testing.T) {
	s, legacy := guildProxyTestServer(), newFakeLegacyWorldConnection()
	sting := pendingForTest(1978, 1)
	arcane := pendingForTest(3044, 2)
	now := time.Unix(1_700_000_000, 0)
	nowPtr := &now
	session := &proxySession{
		legacy:           &legacyauth.Session{Username: "TEST"},
		legacyWorld:      legacy,
		currentCharacter: 7,
		currentMapID:     530,
		pendingCasts:     []modernworld.PendingCast{sting},
		spellQueue: spellQueue{
			manualRetry: true,
			nowFn:       func() time.Time { return *nowPtr },
		},
	}
	session.enqueuePlayerCastLocked(sting, []byte{1})
	session.pendingCasts = append(session.pendingCasts, arcane)
	session.enqueuePlayerCastLocked(arcane, []byte{2})
	done := make(chan struct{})
	go func() { defer close(done); s.relayLegacyWorld(session, nil, legacy) }()
	defer func() { legacy.Close(); <-done }()

	body := []byte{1}
	body = binary.LittleEndian.AppendUint32(body, 1978)
	body = append(body, byte(spellFailedNotReady))
	legacy.reads <- legacyworld.Packet{Opcode: legacyworld.SMSGCastFailed, Body: body}
	deadline := time.After(2 * time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		session.worldMu.Lock()
		packets := append([]modernworld.Packet(nil), session.pendingInstance...)
		pending := append([]modernworld.PendingCast(nil), session.pendingCasts...)
		activeID := modernworld.GUID128{}
		if session.spellQueue.active != nil {
			activeID = session.spellQueue.active.ClientCastID
		}
		session.worldMu.Unlock()
		if len(packets) >= 2 {
			if packets[0].Opcode != modernworld.SMSGSpellPrepare || packets[1].Opcode != modernworld.SMSGCastFailed {
				t.Fatalf("NOT_READY packets=%v", opcodesOf(packets))
			}
			if !bytes.Equal(packets[0].Body, modernworld.EncodeSpellPrepare(sting.ClientCastID, sting.ServerCastID)) {
				t.Fatal("NOT_READY did not return sting ClientCastID")
			}
			info := modernworld.SpellFailureInfo{SpellID: 1978, Reason: spellFailedNotReady, Arg1: -1, Arg2: -1}
			want := modernworld.EncodeCastFailed(sting.ServerCastID, info, sting.VisualID)
			if !bytes.Equal(packets[1].Body, want) {
				t.Fatalf("cast failed=%x want=%x", packets[1].Body, want)
			}
			if len(pending) != 1 || pending[0].ClientCastID != arcane.ClientCastID || activeID != arcane.ClientCastID {
				t.Fatal("sting NOT_READY consumed arcane")
			}
			return
		}
		select {
		case <-deadline:
			t.Fatalf("sting NOT_READY was swallowed: packets=%d pending=%d", len(packets), len(pending))
		case <-tick.C:
		}
	}
}

func TestSameSpellHoldFastNotReadyFailsOriginalCastID(t *testing.T) {
	_, session, _, now := queueSession(t)
	first := pendingForTest(14287, 1)
	second := pendingForTest(14287, 2)
	session.pendingCasts = []modernworld.PendingCast{first}
	session.enqueuePlayerCastLocked(first, []byte{1})
	session.pendingCasts = append(session.pendingCasts, second)
	if got := session.enqueuePlayerCastLocked(second, []byte{2}); !got.held {
		t.Fatal("second arcane shot was forwarded during the first")
	}
	session.pendingCasts[0].Started = true
	session.markSpellQueueStartedLocked(session.pendingCasts[0])
	completed, found := session.matchPlayerCastFailureLocked(14287, true)
	if !found || completed.ClientCastID != first.ClientCastID {
		t.Fatal("first arcane GO did not use its own CastID")
	}
	if body := session.completeSpellQueueCastLocked(completed); !bytes.Equal(body, []byte{2}) {
		t.Fatalf("GO did not release the held arcane shot: %#v", body)
	}
	*now = now.Add(20 * time.Millisecond)
	if !session.swallowCastFailedLocked(14287, spellFailedNotReady) {
		t.Fatal("first NOT_READY of the released hold should still retry")
	}
	if !session.retrySpellQueue(session.spellQueue.generation) {
		t.Fatal("released hold did not retry")
	}
	*now = now.Add(16 * time.Millisecond)
	if session.swallowCastFailedLocked(14287, spellFailedNotReady) {
		t.Fatal("NOT_READY inside the fuse was swallowed")
	}
	failed, found := session.matchPlayerCastFailureLocked(14287, true)
	if !found || failed.ClientCastID != second.ClientCastID {
		t.Fatal("fast NOT_READY lost the held CastID")
	}
	if body := session.failSpellQueueLocked(failed); body != nil {
		t.Fatal("held failure promoted another cast")
	}
	if session.spellQueue.active != nil || len(session.pendingCasts) != 0 {
		t.Fatal("held arcane CastID stayed pending after NOT_READY")
	}
}

func TestReplacedHoldSendsPrepareAndCastFailed(t *testing.T) {
	_, session, _, _ := queueSession(t)
	first := pendingForTest(14287, 1)
	held := pendingForTest(14287, 2)
	steady := pendingForTest(56641, 3)
	session.pendingCasts = []modernworld.PendingCast{first}
	session.enqueuePlayerCastLocked(first, []byte{1})
	session.pendingCasts = append(session.pendingCasts, held)
	session.enqueuePlayerCastLocked(held, []byte{2})
	session.pendingCasts = append(session.pendingCasts, steady)
	session.enqueuePlayerCastLocked(steady, []byte{3})
	packets := session.takeAbandonedCastPacketsLocked()
	if len(packets) != 2 || packets[0].Opcode != modernworld.SMSGSpellPrepare || packets[1].Opcode != modernworld.SMSGCastFailed {
		t.Fatalf("replaced hold packets=%v", opcodesOf(packets))
	}
	if !bytes.Equal(packets[0].Body, modernworld.EncodeSpellPrepare(held.ClientCastID, held.ServerCastID)) {
		t.Fatal("replaced hold did not return its ClientCastID")
	}
	info := modernworld.SpellFailureInfo{SpellID: 14287, Reason: spellFailedSpellInProgress, Arg1: -1, Arg2: -1}
	if !bytes.Equal(packets[1].Body, modernworld.EncodeCastFailed(held.ServerCastID, info, held.VisualID)) {
		t.Fatal("replaced hold CAST_FAILED did not use the held CastID")
	}
	if len(session.pendingCasts) != 2 || session.pendingCasts[0].ClientCastID != first.ClientCastID || session.pendingCasts[1].ClientCastID != steady.ClientCastID {
		t.Fatal("replaced hold drop removed the in-flight casts")
	}
}

func TestStaleSameSpellDoesNotStealActiveStart(t *testing.T) {
	server, session, _, _ := queueSession(t)
	stale := pendingForTest(14287, 1)
	current := pendingForTest(14287, 2)
	session.pendingCasts = []modernworld.PendingCast{stale, current}
	session.enqueuePlayerCastLocked(current, []byte{2})
	cast := modernworld.SpellCastData{CasterGUID: 7, CasterUnit: 7, SpellID: 14287}
	if err := server.forwardLegacySpell(session, cast, true); err != nil {
		t.Fatal(err)
	}
	if len(session.pendingInstance) < 2 || !bytes.Equal(session.pendingInstance[0].Body, modernworld.EncodeSpellPrepare(current.ClientCastID, current.ServerCastID)) {
		t.Fatal("START bound the stale arcane CastID")
	}
	if session.pendingCasts[0].ClientCastID != stale.ClientCastID || session.pendingCasts[0].Started {
		t.Fatalf("START marked the stale pending: %#v", session.pendingCasts)
	}
	if !session.pendingCasts[1].Started || session.pendingCasts[1].ClientCastID != current.ClientCastID {
		t.Fatal("active arcane shot was not marked started")
	}
}

func TestTrimPendingCastFailsEvictedClientCast(t *testing.T) {
	_, session, _, _ := queueSession(t)
	oldest := pendingForTest(14287, 1)
	session.pendingCasts = []modernworld.PendingCast{oldest}
	session.enqueuePlayerCastLocked(oldest, []byte{1})
	for i := 2; i <= 8; i++ {
		next := pendingForTest(uint32(1000+i), uint64(i))
		session.pendingCasts = append(session.pendingCasts, next)
	}
	session.trimPendingCastsLocked(8)
	packets := session.takeAbandonedCastPacketsLocked()
	if len(packets) != 2 || !bytes.Equal(packets[0].Body, modernworld.EncodeSpellPrepare(oldest.ClientCastID, oldest.ServerCastID)) {
		t.Fatalf("evicted cast packets=%v", opcodesOf(packets))
	}
	if session.spellQueue.active != nil {
		t.Fatal("evicting the active cast left the attempt running")
	}
	found := false
	for _, pending := range session.pendingCasts {
		if pending.ClientCastID == oldest.ClientCastID {
			found = true
		}
	}
	if found {
		t.Fatal("evicted CastID stayed in pendingCasts")
	}
}

func opcodesOf(packets []modernworld.Packet) []uint16 {
	out := make([]uint16, len(packets))
	for i, packet := range packets {
		out[i] = packet.Opcode
	}
	return out
}
