package modernworld

import (
	"os"
	"sort"
	"strings"
	"time"
)

// TransportPathSync replays the realm's own MotionTransport path timer between
// the updates AzerothCore actually sends for it.
//
// The 3.4.3 client animates a transport hull from GAMEOBJECT_DYNAMIC path
// progress, and the realm only reports that word when the GameObject state
// changes. The ICC gunships therefore climb with no resync at all: proxy.log
// 22:03:10 carries progress 0x1064 with state 0 (moving), then nothing until
// 22:03:49 carries 0x90e1 with state 1 (parked). For those 39 seconds the
// client runs the hull on its own clock, which is where the flight drifts out
// of step with the encounter script.
//
// Over that window the realm's progress advances 1:1 with the wall clock,
// measured against GAMEOBJECT_LEVEL - the full path time in milliseconds:
// 32893 of 65535 in 39.3 s of a 78697 ms path. Replaying that same line pins
// the client to the script's timeline. Only the realm's progress word is
// extrapolated; no hull position is ever synthesized.
//
// legacy proxy does not replay it. Turning this on by default in the 21:21 run shoved
// both hulls along their paths every second and left the deck cannons as a
// gear cursor that never sent SpellClick. Leave it off unless
// REDSCARF_TRANSPORT_RESYNC=<duration> is set; "off" is also accepted.
type TransportPathSync struct {
	hulls map[uint64]*transportPathState
}

type transportPathState struct {
	dynamic  uint32
	bytes1   uint32
	period   uint32
	observed time.Time
	resent   time.Time
}

// TransportResync is a synthetic legacy Values delta for one hull, shaped like
// the realm's own so it runs through the ordinary translation path.
type TransportResync struct {
	GUID   uint64
	Fields map[int]uint32
}

var transportResyncInterval = parseTransportResyncInterval(os.Getenv("REDSCARF_TRANSPORT_RESYNC"))

func parseTransportResyncInterval(raw string) time.Duration {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "off", "no", "false", "0":
		return 0
	}
	interval, err := time.ParseDuration(raw)
	if err != nil || interval <= 0 {
		return 0
	}
	return interval
}

// TransportResyncEnabled reports whether the replay is switched on.
func TransportResyncEnabled() bool { return transportResyncInterval > 0 }

// Observe records the realm's path timer for a MotionTransport root. Map
// platforms and elevators (0xf120) are excluded: they carry a DB2 period and
// already animate correctly.
func (s *TransportPathSync) Observe(guid uint64, fields map[int]uint32, now time.Time) {
	if uint16(guid>>48) != 0x1fc0 {
		return
	}
	dynamic, hasDynamic := fields[legacyGameObjectDynamic]
	bytes1, hasState := fields[legacyGameObjectBytes1]
	period := fields[legacyGameObjectLevel]
	if !hasDynamic || !hasState || period == 0 {
		return
	}
	if s.hulls == nil {
		s.hulls = make(map[uint64]*transportPathState, 2)
	}
	s.hulls[guid] = &transportPathState{dynamic: dynamic, bytes1: bytes1, period: period, observed: now}
}

// Reset drops the tracked hulls, for map changes and character switches.
func (s *TransportPathSync) Reset() { s.hulls = nil }

// Due returns the hulls whose progress should be replayed now.
func (s *TransportPathSync) Due(now time.Time) []TransportResync {
	return s.due(now, transportResyncInterval)
}

func (s *TransportPathSync) due(now time.Time, interval time.Duration) []TransportResync {
	if interval <= 0 || len(s.hulls) == 0 {
		return nil
	}
	resyncs := make([]TransportResync, 0, len(s.hulls))
	for guid, hull := range s.hulls {
		// Legacy GO_STATE_ACTIVE is the moving transport; state 1 is parked and
		// the client must be left on the realm's last reported progress.
		if byte(hull.bytes1) != 0 {
			continue
		}
		last := hull.resent
		if last.Before(hull.observed) {
			last = hull.observed
		}
		if now.Sub(last) < interval {
			continue
		}
		hull.resent = now
		resyncs = append(resyncs, TransportResync{GUID: guid, Fields: map[int]uint32{
			legacyGameObjectDynamic: hull.progressAt(now),
			legacyGameObjectBytes1:  hull.bytes1,
		}})
	}
	if len(resyncs) == 0 {
		return nil
	}
	sort.Slice(resyncs, func(i, j int) bool { return resyncs[i].GUID < resyncs[j].GUID })
	return resyncs
}

// progressAt advances the realm's progress word the way AzerothCore does:
// pathProgress grows with the clock and is reported as a fraction of the path
// period scaled to 65535. The realm's own value stays the baseline, so a replay
// cannot accumulate error.
func (hull *transportPathState) progressAt(now time.Time) uint32 {
	elapsed := now.Sub(hull.observed).Milliseconds()
	if elapsed < 0 {
		elapsed = 0
	}
	const scale = uint64(65535)
	advance := uint64(elapsed) * scale / uint64(hull.period)
	progress := (uint64(hull.dynamic>>16) + advance) % scale
	return uint32(progress)<<16 | hull.dynamic&0xffff
}
