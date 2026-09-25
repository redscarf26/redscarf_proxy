package proxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"redscarf/internal/bnet"
	"redscarf/internal/legacyauth"
	"redscarf/internal/legacyworld"
	"redscarf/internal/modernworld"
	"redscarf/internal/opcodes"
	"redscarf/internal/realm"
)

type legacyLoginFunc func(context.Context, string, legacyauth.Credentials) (*legacyauth.Session, error)

type legacyWorldConnection interface {
	ReadPacket() (legacyworld.Packet, error)
	WritePacket(uint32, []byte) error
	Close() error
}

type legacyWorldFunc func(context.Context, legacyauth.Realm, *legacyauth.Session) (legacyWorldConnection, error)

type addonWhisperTarget struct {
	expires time.Time
	pending uint32
}

type lootRollReference struct {
	legacyGUID uint64
	modernGUID modernworld.GUID128
	itemKey    modernworld.LootRollKey
}

// pendingObjectSpell holds a server-originated cast until every caster object
// referenced by it has actually been put on the client stream.  A legacy
// realm can emit SPELL_GO between object-update batches during login; sending
// a creature GUID before that creature's create makes the 3.4.3 combat-log
// path dereference a missing object.
type pendingObjectSpell struct {
	cast  modernworld.SpellCastData
	start bool
}

const (
	serviceConnection                    = 0x65446991
	serviceAuthentication                = 0x0decfc01
	serviceAccount                       = 0x62da0891
	serviceGameUtilities                 = 0x3fc1274d
	listenerChallenge                    = 0xbbda171f
	listenerAuthentication               = 0x71240e35
	rpcOK                         uint32 = 0
	rpcDenied                     uint32 = 3
	rpcBadVersion                 uint32 = 0x1c
	rpcBadProgram                 uint32 = 0x4d
	rpcBadLocale                  uint32 = 0x4e
	rpcBadPlatform                uint32 = 0x4f
	maxCompletedQuestLoginPackets        = 4096
	maxPendingObjectSpells               = 128
)

type proxySession struct {
	guild                       modernworld.GuildState
	encounterFrames             modernworld.EncounterFrames
	encounterInProgress         bool
	channelObjects              map[uint64]uint64
	worldMu                     sync.Mutex
	legacy                      *legacyauth.Session
	gameAccountID               uint64
	modernKey                   [64]byte
	clientSecret                [32]byte
	hasClientSecret             bool
	joinSecret                  [32]byte
	worldKey                    [64]byte
	selectedRealm               legacyauth.Realm
	hasRealm                    bool
	modernSessionKey            [40]byte
	legacyWorld                 legacyWorldConnection
	realmWorld                  *modernworld.PacketConn
	instanceWorld               *modernworld.PacketConn
	instanceKey                 uint64
	pendingInstance             []modernworld.Packet
	knownCharacters             map[uint64]struct{}
	knownCharacterInfo          map[uint64]modernworld.LegacyCharacter
	currentCharacter            uint64
	currentChanneledSpell       uint32
	currentMapID                uint16
	mapDifficulty               modernworld.MapDifficulty
	mapReady                    bool
	hasMapDifficulty            bool
	currentZoneID               uint32
	currentTaxiNode             uint32
	usableTaxiNodes             []byte
	taxiReplyPending            bool
	taxiLoginGraceUntil         time.Time
	actionButtons               []int32
	actionButtonReason          byte
	hasActionButtons            bool
	knownSpells                 []uint32
	spellHistory                []modernworld.SpellHistoryEntry
	hasKnownSpells              bool
	knownSpellsSent             bool
	unlearnSpells               []uint32
	activePlayerCreated         bool
	objectGUIDs                 map[uint64]modernworld.GUID128
	visibleObjectGUIDs          map[uint64]struct{}
	objectTypes                 map[uint64]uint8
	objectFields                map[uint64]map[int]uint32
	currencyQty                 map[uint32]uint32
	inspectArenaTeams           map[uint64][modernworld.ArenaSlotCount]modernworld.ArenaTeamInspect
	arenaTeams                  map[uint32]arenaTeamCache
	battlegroundQueues          map[uint32]modernworld.BattlegroundQueue
	objectPositions             map[uint64][3]float32
	transportSync               modernworld.TransportPathSync
	initWorldStates             []byte
	goQueryGUIDs                map[uint32]modernworld.GUID128
	queriedCreatures            map[uint32]struct{}
	creatureDisplay             map[uint32]uint32
	queriedMirrorImages         map[uint64]struct{}
	corpseQueryPlayer           modernworld.GUID128
	pendingCorpseLocation       *modernworld.CorpseLocation
	playerAuras                 []modernworld.AuraInfo
	playerAuraBySlot            map[uint8]uint32
	spellVisuals                map[uint32]uint32
	pendingCasts                []modernworld.PendingCast
	spellQueue                  spellQueue
	completedCastIDs            map[uint32]modernworld.GUID128
	unmatchedSpellGoSeq         uint64
	unmatchedSpellStarts        map[unmatchedSpellKey]modernworld.GUID128
	unmatchedSpellGoes          map[unmatchedSpellKey]struct{}
	runeCooldownStarted         [6]time.Time
	runeState                   modernworld.LegacyRuneState
	hasRuneState                bool
	pendingObjectSpells         []pendingObjectSpell
	accountData                 [modernworld.AccountDataCount]*modernworld.AccountData
	pendingCreateName           string
	pendingCreateCode           byte
	awaitingCreateEnum          bool
	waitingForNewWorld          bool
	waitingForWorldPortAck      bool
	lootLegacyGUID              uint64
	lastLootTargetLegacy        uint64
	lootObjModern               modernworld.GUID128
	lootRolls                   map[lootRollIdentity]lootRollReference
	completedLootRolls          []lootRollIdentity
	masterLootCandidates        []uint64
	masterLootListPending       bool
	lastMasterLootSentLegacy    uint64
	questRewardChoices          map[uint32][6]uint32
	questItemObjectives         map[uint32][]modernworld.QuestItemObjectiveInfo
	questStateOnly              map[uint32]struct{}
	questItemSyncPending        bool
	completedQuestBlocks        []uint64
	completedQuestSyncPending   bool
	completedQuestSyncRequested bool
	broadcastTexts              map[uint32]modernworld.BroadcastTextRecord
	vendorBuyCounts             map[uint32]uint32
	currentInteractedGO         uint64
	addonPrefixes               map[string]struct{}
	addonWhisperTargets         map[string]addonWhisperTarget
	playerNames                 map[uint64]string
	playerIdentities            map[uint64]modernworld.LegacyNameIdentity
	pendingPlayerNameQueries    map[uint64]bool
	pendingPlayerNameModern     map[uint64]modernworld.GUID128
	pendingPetNameGUIDs         map[uint32][]modernworld.GUID128
	pendingNamedChats           map[uint64][]modernworld.LegacyChatMessage
	lastWhisperTarget           string
	lastWhoRequestID            uint32
	partySequence               int32
	partyLegacyGUID             uint64
	partyGUID                   modernworld.GUID128
	partyIndex                  byte
	partyMembers                map[uint64]modernworld.LegacyPartyMember
	partyRoles                  map[uint64]byte
	lfgQueueMode                byte
	lfgRoles                    byte
	partyLootMethod             byte
	partyLootMaster             uint64
	partyLootThreshold          byte
	tradeID                     uint32
	tradeClientState            uint32
	tradeServerState            uint32
	tradeActive                 bool
	readyCheckGeneration        uint64
	lastStableMaster            uint64
}

type unmatchedSpellKey struct {
	caster uint64
	spell  uint32
}

func normalizePlayerNameKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func (session *proxySession) rememberAddonWhisperTargetLocked(name string, now time.Time) {
	key := normalizePlayerNameKey(name)
	if key == "" {
		return
	}
	if session.addonWhisperTargets == nil {
		session.addonWhisperTargets = make(map[string]addonWhisperTarget)
	}
	target := session.addonWhisperTargets[key]
	target.expires = now.Add(15 * time.Second)
	if target.pending < ^uint32(0) {
		target.pending++
	}
	session.addonWhisperTargets[key] = target
}

func (session *proxySession) consumeAddonWhisperFailureLocked(name string, now time.Time) bool {
	key := normalizePlayerNameKey(name)
	target, ok := session.addonWhisperTargets[key]
	if !ok {
		return false
	}
	if now.After(target.expires) {
		delete(session.addonWhisperTargets, key)
		return false
	}
	if target.pending <= 1 {
		delete(session.addonWhisperTargets, key)
	} else {
		target.pending--
		session.addonWhisperTargets[key] = target
	}
	return true
}

type arenaTeamCache struct {
	emblem    modernworld.ArenaTeamEmblem
	hasEmblem bool
	stats     modernworld.ArenaTeamStats
	hasStats  bool
}

func (session *proxySession) rememberPlayerNameLocked(guid uint64, name string) {
	name = strings.TrimSpace(name)
	if guid == 0 || name == "" {
		return
	}
	if session.playerNames == nil {
		session.playerNames = make(map[uint64]string)
	}
	session.playerNames[guid] = name
}

func (session *proxySession) storeArenaEmblemLocked(emblem modernworld.ArenaTeamEmblem) {
	if session.arenaTeams == nil {
		session.arenaTeams = make(map[uint32]arenaTeamCache)
	}
	cache := session.arenaTeams[emblem.TeamID]
	cache.emblem = emblem
	cache.hasEmblem = true
	session.arenaTeams[emblem.TeamID] = cache
}

func (session *proxySession) storeArenaStatsLocked(teamID uint32, stats modernworld.ArenaTeamStats) {
	if session.arenaTeams == nil {
		session.arenaTeams = make(map[uint32]arenaTeamCache)
	}
	cache := session.arenaTeams[teamID]
	cache.stats = stats
	cache.hasStats = true
	session.arenaTeams[teamID] = cache
}

func (session *proxySession) arenaStatsLocked(teamID uint32) modernworld.ArenaTeamStats {
	if session.arenaTeams == nil {
		return modernworld.ArenaTeamStats{}
	}
	return session.arenaTeams[teamID].stats
}

func (session *proxySession) playerGUIDByNameLocked(name string) modernworld.GUID128 {
	name = strings.TrimSpace(name)
	if name == "" {
		return modernworld.GUID128{}
	}
	for guid, stored := range session.playerNames {
		if strings.EqualFold(stored, name) {
			return session.modernGUIDForLegacyLocked(guid)
		}
	}
	for guid, character := range session.knownCharacterInfo {
		if strings.EqualFold(character.Name, name) {
			return session.modernGUIDForLegacyLocked(guid)
		}
	}
	for guid, identity := range session.playerIdentities {
		if strings.EqualFold(identity.Name, name) {
			return session.modernGUIDForLegacyLocked(guid)
		}
	}
	return modernworld.GUID128{}
}

func (session *proxySession) playerNameByModernLocked(guid modernworld.GUID128) string {
	legacy, known := session.legacyGUIDForModernLocked(guid)
	if !known || legacy == 0 {
		var ok bool
		legacy, ok = modernworld.LegacyPlayerGUIDFromModern(guid)
		if !ok || legacy == 0 {
			return ""
		}
	}
	if name := strings.TrimSpace(session.playerNames[legacy]); name != "" {
		return name
	}
	if identity, _, ok := session.playerNameIdentityLocked(legacy); ok {
		return identity.Name
	}
	return ""
}

func (session *proxySession) queueNamedChatLocked(message modernworld.LegacyChatMessage) bool {
	guid := message.Sender
	if guid == 0 {
		return false
	}
	if session.pendingNamedChats == nil {
		session.pendingNamedChats = make(map[uint64][]modernworld.LegacyChatMessage)
	}
	// A name response normally arrives immediately. Keep a bounded queue so a
	// broken legacy peer cannot grow the session indefinitely.
	if len(session.pendingNamedChats[guid]) < 32 {
		session.pendingNamedChats[guid] = append(session.pendingNamedChats[guid], message)
	}
	return session.queuePlayerNameQueryLocked(guid)
}

// queuePlayerNameQueryLocked deduplicates legacy name queries shared by chat
// and party-cache warmups. PartyUpdate carries a display name, but build 54261
// does not reliably issue CMSG_QUERY_PLAYER_NAMES for an out-of-range member.
// Without a QueryPlayerNamesResponse in its cache, selecting that member falls
// back to "Unknown Target" even though the party portrait has a GUID.
func (session *proxySession) queuePlayerNameQueryLocked(guid uint64) bool {
	if guid == 0 {
		return false
	}
	if _, known := session.playerIdentities[guid]; known {
		return false
	}
	if session.pendingPlayerNameQueries == nil {
		session.pendingPlayerNameQueries = make(map[uint64]bool)
	}
	if session.pendingPlayerNameQueries[guid] {
		return false
	}
	session.pendingPlayerNameQueries[guid] = true
	return true
}

func (session *proxySession) playerNameIdentityLocked(legacyGUID uint64) (modernworld.LegacyNameIdentity, byte, bool) {
	if character, ok := session.knownCharacterInfo[legacyGUID]; ok && character.Name != "" {
		return modernworld.LegacyNameIdentity{
			GUID:  character.GUID,
			Name:  character.Name,
			Race:  character.Race,
			Sex:   character.Sex,
			Class: character.Class,
		}, character.Level, true
	}
	identity, ok := session.playerIdentities[legacyGUID]
	if !ok || identity.Name == "" {
		return modernworld.LegacyNameIdentity{}, 0, false
	}
	level := byte(1)
	if character, known := session.knownCharacterInfo[legacyGUID]; known && character.Level != 0 {
		level = character.Level
	}
	return identity, level, true
}

func (session *proxySession) rememberPendingNameQueryLocked(legacyGUID uint64, modern modernworld.GUID128) {
	if legacyGUID == 0 {
		return
	}
	if session.pendingPlayerNameModern == nil {
		session.pendingPlayerNameModern = make(map[uint64]modernworld.GUID128)
	}
	session.pendingPlayerNameModern[legacyGUID] = modern
}

func (session *proxySession) takeNamedChatsLocked(guid uint64) []modernworld.LegacyChatMessage {
	pending := session.pendingNamedChats[guid]
	delete(session.pendingNamedChats, guid)
	delete(session.pendingPlayerNameQueries, guid)
	return pending
}

func (session *proxySession) legacyGUIDForModernLocked(guid modernworld.GUID128) (uint64, bool) {
	if guid.Low == 0 && guid.High == 0 {
		return 0, true
	}
	for legacyGUID, modernGUID := range session.objectGUIDs {
		if modernGUID == guid {
			return legacyGUID, true
		}
	}
	if session.currentCharacter != 0 && guid.Low == uint64(uint32(session.currentCharacter)) {
		return session.currentCharacter, true
	}
	return 0, false
}

// resolveSpellCastTargetLocked maps a modern cast target GUID128 back to a legacy
// guid for the legacy spell-cast targets, returning 0 for an empty or unknown one.
func resolveSpellCastTargetLocked(session *proxySession, guid modernworld.GUID128) uint64 {
	if guid.Low == 0 && guid.High == 0 {
		return 0
	}
	legacy, _ := session.legacyGUIDForModernLocked(guid)
	return legacy
}

// resolveSpellCastTransportLocked maps the modern spell-target transport value to
// a legacy guid, mirroring the transport resolution used by the cast path.
func resolveSpellCastTransportLocked(session *proxySession, location *modernworld.SpellTargetLocation) uint64 {
	if location == nil {
		return 0
	}
	legacy, _ := session.legacyGUIDForModernLocked(location.Transport)
	return legacy
}

// remapSpellTargetTransportLocked converts a legacy SPELL_START/GO location's
// packed guid64 (stored in Transport.Low) into the session's modern GUID128.
// MO_TRANSPORT hulls keep their identity in High, so leaving the guid64 in Low
// made rocket-pack dests resolve as world coordinates.
func remapSpellTargetTransportLocked(session *proxySession, location *modernworld.SpellTargetLocation) {
	if location == nil || (location.Transport.Low == 0 && location.Transport.High == 0) {
		return
	}
	if location.Transport.High != 0 {
		return
	}
	location.Transport = session.modernGUIDForLegacyLocked(location.Transport.Low)
}

func formatSpellTargetTransport(location *modernworld.SpellTargetLocation) string {
	if location == nil {
		return ""
	}
	return fmt.Sprintf("%016x:%016x", location.Transport.High, location.Transport.Low)
}

func formatSpellTargetLocation(location *modernworld.SpellTargetLocation) string {
	if location == nil {
		return ""
	}
	return fmt.Sprintf("%g,%g,%g", location.Location.X, location.Location.Y, location.Location.Z)
}

func (session *proxySession) legacyPartyTargetLocked(guid modernworld.GUID128) (uint64, bool) {
	legacyGUID, known := session.legacyGUIDForModernLocked(guid)
	if !known {
		legacyGUID, known = modernworld.LegacyPlayerGUIDFromModern(guid)
	}
	return legacyGUID, known
}

func (session *proxySession) legacySocialTargetLocked(guid modernworld.GUID128) (uint64, bool) {
	if guid.Low == 0 && guid.High == 0 {
		return 0, false
	}
	legacyGUID, known := session.legacyGUIDForModernLocked(guid)
	if known && legacyGUID != 0 {
		return legacyGUID, true
	}
	return modernworld.LegacyPlayerGUIDFromModern(guid)
}

// legacyMailboxLocked resolves the modern mailbox GUID to its 3.3.5 GUID64,
// falling back to the most recently used GameObject. Delete and return-to-sender
// do not carry the mailbox GUID on the modern wire at all, so the fallback is
// the only value those commands can use.
func (session *proxySession) legacyMailboxLocked(mailbox modernworld.GUID128) uint64 {
	if mailbox.Low != 0 || mailbox.High != 0 {
		if legacy, known := session.legacyGUIDForModernLocked(mailbox); known {
			return legacy
		}
	}
	return session.currentInteractedGO
}

type bnetConnState struct {
	session   *proxySession
	nextToken uint32
	header    bnet.Header
}

type Server struct {
	cufMu  sync.Mutex
	config Config
	log    *slog.Logger
	tls    *tls.Config

	mu        sync.Mutex
	listeners []net.Listener
	addresses map[string]string
	http      *http.Server

	legacyLogin      legacyLoginFunc
	legacyWorld      legacyWorldFunc
	sessionsMu       sync.RWMutex
	sessions         map[string]*proxySession
	worldSessions    map[string]*proxySession
	instanceSessions map[uint64]*proxySession
	partyOrdersMu    sync.Mutex
	partyOrders      map[partyOrderKey][]uint64
}

func New(config Config, logger *slog.Logger) (*Server, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if err := ensureTLSFilesReadable(config); err != nil {
		return nil, fmt.Errorf("TLS files: %w", err)
	}
	if logger == nil {
		logger = slog.Default()
	}
	tlsConfig, development, err := loadTLSConfig(config.TLSCertFile, config.TLSKeyFile)
	if err != nil {
		return nil, err
	}
	if development {
		logger.Warn("using an ephemeral development certificate; configure the Arctium-compatible certificate for live login")
	}
	return &Server{
		config:      config,
		log:         logger,
		tls:         tlsConfig,
		addresses:   make(map[string]string),
		legacyLogin: legacyauth.Login,
		legacyWorld: func(ctx context.Context, selected legacyauth.Realm, session *legacyauth.Session) (legacyWorldConnection, error) {
			return legacyworld.Authenticate(ctx, selected, session)
		},
		sessions:         make(map[string]*proxySession),
		worldSessions:    make(map[string]*proxySession),
		instanceSessions: make(map[uint64]*proxySession),
	}, nil
}

func (s *Server) Run(ctx context.Context) error {
	if err := s.openListeners(); err != nil {
		s.closeListeners()
		return err
	}

	errCh := make(chan error, 3)
	var wg sync.WaitGroup
	start := func(name string, serve func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := serve(); err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, http.ErrServerClosed) {
				select {
				case errCh <- fmt.Errorf("%s: %w", name, err):
				default:
				}
			}
		}()
	}

	start("BNet", func() error { return s.serveBNet(s.listeners[0]) })
	start("REST", func() error { return s.serveREST(s.listeners[1]) })
	start("World", func() error { return s.serveWorld(s.listeners[2]) })

	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-errCh:
	}
	s.closeListeners()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if s.http != nil {
		_ = s.http.Shutdown(shutdownCtx)
	}
	wg.Wait()
	return runErr
}

func (s *Server) Addresses() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := make(map[string]string, len(s.addresses))
	for name, address := range s.addresses {
		copy[name] = address
	}
	return copy
}

func (s *Server) openListeners() error {
	specs := []struct {
		name    string
		address string
		tls     bool
	}{
		{name: "bnet", address: s.config.BNetAddress, tls: true},
		{name: "rest", address: s.config.RESTAddress, tls: true},
		{name: "world", address: s.config.WorldAddress},
	}
	for _, spec := range specs {
		listener, err := net.Listen("tcp", spec.address)
		if err != nil {
			return fmt.Errorf("listen %s on %s: %w", spec.name, spec.address, err)
		}
		actual := listener.Addr().String()
		if spec.tls {
			tlsConfig := s.tls.Clone()
			if spec.name == "rest" {
				tlsConfig = restTLSConfig(s.tls)
			} else if spec.name == "bnet" {
				tlsConfig = bnetTLSConfig(s.tls)
			}
			listener = tls.NewListener(listener, tlsConfig)
		}
		s.listeners = append(s.listeners, listener)
		s.mu.Lock()
		s.addresses[spec.name] = actual
		s.mu.Unlock()
		s.log.Info("listener ready", "service", spec.name, "address", actual, "tls", spec.tls)
	}
	return nil
}

func (s *Server) closeListeners() {
	for _, listener := range s.listeners {
		_ = listener.Close()
	}
}

func (s *Server) serveBNet(listener net.Listener) error {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go s.handleBNet(conn)
	}
}

var errBNetDisconnect = errors.New("BNet disconnect requested")

func (s *Server) handleBNet(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Minute))
	if tlsConn, ok := conn.(*tls.Conn); ok {
		if err := tlsConn.Handshake(); err != nil {
			s.log.Warn("BNet TLS handshake failed", "remote", conn.RemoteAddr(), "error", err)
			return
		}
	}
	s.log.Info("BNet connection accepted", "remote", conn.RemoteAddr())
	peeked := &peekConn{Conn: conn}
	state := &bnetConnState{}
	loggedPrefix := false
	for {
		frame, err := bnet.ReadFrame(peeked)
		if !loggedPrefix && peeked.peek.Len() > 0 {
			s.log.Info("BNet first bytes", "hex", fmt.Sprintf("%x", peeked.peek.Bytes()), "count", peeked.peek.Len())
			loggedPrefix = true
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				s.log.Warn("BNet connection ended", "remote", conn.RemoteAddr(), "error", err)
			}
			return
		}
		s.log.Info("BNet RPC received",
			"service_id", frame.Header.ServiceID,
			"service_hash", fmt.Sprintf("0x%08X", frame.Header.ServiceHash),
			"method_id", frame.Header.MethodID,
			"token", frame.Header.Token,
			"payload_bytes", len(frame.Payload))
		if err := s.dispatchBNet(conn, state, frame); err != nil {
			if errors.Is(err, errBNetDisconnect) {
				s.log.Info("BNet disconnect completed", "remote", conn.RemoteAddr())
				return
			}
			s.log.Warn("BNet RPC failed", "service_hash", fmt.Sprintf("0x%08X", frame.Header.ServiceHash), "method_id", frame.Header.MethodID, "error", err)
			return
		}
	}
}

type peekConn struct {
	net.Conn
	peek  bytes.Buffer
	limit int
}

func (c *peekConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 && c.limit < 512 {
		remain := 512 - c.limit
		if n < remain {
			remain = n
		}
		c.peek.Write(p[:remain])
		c.limit += remain
	}
	return n, err
}

func (s *Server) dispatchBNet(conn net.Conn, state *bnetConnState, frame bnet.Frame) error {
	serviceHash := frame.Header.ServiceHash
	if serviceHash == 0 && frame.Header.ServiceID == 0 {
		serviceHash = serviceConnection
	}
	switch serviceHash {
	case serviceConnection:
		switch frame.Header.MethodID {
		case 1:
			request, err := bnet.DecodeConnectRequest(frame.Payload)
			if err != nil {
				return err
			}
			now := time.Now()
			payload := bnet.EncodeConnectResponse(request, uint32(os.Getpid()), uint32(now.Unix()), uint64(now.UnixMilli()))
			return sendRPCResponse(conn, frame.Header, rpcOK, payload)
		case 5:
			return sendRPCResponse(conn, frame.Header, rpcOK, nil)
		case 7:
			code, err := bnet.DecodeDisconnectRequest(frame.Payload)
			if err != nil {
				return err
			}
			// The client drops the BNet front connection after joining a realm.
			// A plain RPC OK leaves it disconnecting when returning to login.
			// Match Hermes: send ForceDisconnect (method 4), then close only
			// this socket. The world connection still needs the shared session.
			state.nextToken++
			if err := sendRPCRequest(conn, frame.Header, serviceConnection, 4, state.nextToken, bnet.EncodeDisconnectNotification(code)); err != nil {
				return err
			}
			s.log.Info("BNet disconnect notification sent", "error_code", code)
			return errBNetDisconnect
		}
	case serviceAuthentication:
		switch frame.Header.MethodID {
		case 1:
			request, err := bnet.DecodeLogonRequest(frame.Payload)
			if err != nil {
				return err
			}
			s.log.Info("BNet Logon",
				"program", request.Program,
				"platform", request.Platform,
				"locale", request.Locale,
				"build", request.ApplicationVersion,
				"payload_hex", fmt.Sprintf("%x", frame.Payload))
			if status := validateLogonRequest(request); status != rpcOK {
				s.log.Warn("BNet Logon rejected", "status", fmt.Sprintf("0x%X", status))
				return sendRPCResponse(conn, frame.Header, status, nil)
			}
			state.header = frame.Header
			if state.nextToken < 100 {
				state.nextToken = 100
			}
			state.nextToken++
			challengeURL, err := s.loginURL(request.Platform, request.ApplicationVersion, request.Locale)
			if err != nil {
				return err
			}
			s.log.Info("BNet login challenge", "url", challengeURL, "token", state.nextToken)
			challengePayload := bnet.EncodeExternalChallenge(challengeURL)
			if err := sendRPCRequest(conn, state.header, listenerChallenge, 3, state.nextToken, challengePayload); err != nil {
				return err
			}
			s.log.Info("BNet RPC sent",
				"kind", "OnExternalChallenge",
				"header_hex", fmt.Sprintf("%x", bnet.MarshalHeader(bnet.Header{
					MethodID:    3,
					Token:       state.nextToken,
					Size:        uint32(len(challengePayload)),
					ServiceHash: listenerChallenge,
				})))
			return sendRPCResponse(conn, frame.Header, rpcOK, nil)
		case 7:
			ticket, err := bnet.DecodeWebCredentials(frame.Payload)
			if err != nil {
				return err
			}
			s.sessionsMu.RLock()
			session := s.sessions[ticket]
			s.sessionsMu.RUnlock()
			if session == nil {
				return sendRPCResponse(conn, frame.Header, rpcDenied, nil)
			}
			if _, err := rand.Read(session.modernKey[:]); err != nil {
				return err
			}
			state.session = session
			state.nextToken++
			if err := sendRPCRequest(conn, state.header, listenerAuthentication, 5, state.nextToken, bnet.EncodeLogonResult(session.modernKey[:], session.gameAccountID)); err != nil {
				return err
			}
			return sendRPCResponse(conn, frame.Header, rpcOK, nil)
		}
	case serviceGameUtilities:
		if state.session == nil {
			return sendRPCResponse(conn, frame.Header, rpcDenied, nil)
		}
		switch frame.Header.MethodID {
		case 1:
			return s.handleGameUtilitiesRequest(conn, state, frame)
		case 10:
			key, err := bnet.DecodeGetAllValuesRequest(frame.Payload)
			if err != nil {
				return err
			}
			if key == "Command_RealmListRequest_v1_wotlk1" {
				payload := bnet.EncodeGetAllValuesResponse([]bnet.Variant{bnet.StringVariant(realm.SubRegion)})
				return sendRPCResponse(conn, frame.Header, rpcOK, payload)
			}
			return sendRPCResponse(conn, frame.Header, rpcOK, nil)
		}
	case serviceAccount:
		if state.session == nil {
			return sendRPCResponse(conn, frame.Header, rpcDenied, nil)
		}
		switch frame.Header.MethodID {
		case 30:
			return sendRPCResponse(conn, frame.Header, rpcOK, bnet.EncodeAccountStateResponse())
		case 31:
			return sendRPCResponse(conn, frame.Header, rpcOK, bnet.EncodeGameAccountStateResponse(state.session.legacy.Username))
		}
	}
	// WotLK Classic requests several optional services during glue-screen
	// startup. Hermes intentionally returns an empty OK for unknown services.
	return sendRPCResponse(conn, frame.Header, rpcOK, nil)
}

func (s *Server) handleGameUtilitiesRequest(conn net.Conn, state *bnetConnState, frame bnet.Frame) error {
	attributes, err := bnet.DecodeClientRequest(frame.Payload)
	if err != nil {
		return err
	}
	var command string
	var commandValue bnet.Variant
	for _, attribute := range attributes {
		if strings.HasPrefix(attribute.Name, "Command_") {
			command = attribute.Name
			commandValue = attribute.Value
		}
	}
	var response []bnet.Attribute
	switch command {
	case "Command_RealmListTicketRequest_v1_wotlk1":
		if clientInfo, ok := bnet.FindAttribute(attributes, "Param_ClientInfo"); ok && clientInfo.HasBlob {
			secret, err := parseClientSecret(clientInfo.BlobValue)
			if err != nil {
				return err
			}
			state.session.clientSecret = secret
			state.session.hasClientSecret = true
		}
		response = append(response, bnet.Attribute{
			Name:  "Param_RealmListTicket",
			Value: bnet.BlobVariant([]byte("AuthRealmListTicket")),
		})
	case "Command_LastCharPlayedRequest_v1_wotlk1":
		// An empty successful response means there is no last-played character.
	case "Command_RealmListRequest_v1_wotlk1":
		subRegion := realm.SubRegion
		if commandValue.StringValue != nil {
			subRegion = *commandValue.StringValue
		}
		realmList, err := realm.EncodeList(state.session.legacy.Realms, subRegion, s.config.ClientBuild)
		if err != nil {
			return err
		}
		characterCounts, err := realm.EncodeCharacterCounts(state.session.legacy.Realms)
		if err != nil {
			return err
		}
		response = append(response,
			bnet.Attribute{Name: "Param_RealmList", Value: bnet.BlobVariant(realmList)},
			bnet.Attribute{Name: "Param_CharacterCountList", Value: bnet.BlobVariant(characterCounts)},
		)
	case "Command_RealmJoinRequest_v1_wotlk1":
		address, ok := bnet.FindAttribute(attributes, "Param_RealmAddress")
		if !ok || address.UintValue == nil || !state.session.hasClientSecret {
			return sendRPCResponse(conn, frame.Header, rpcDenied, nil)
		}
		var selected legacyauth.Realm
		for _, candidate := range state.session.legacy.Realms {
			if uint64(realm.Address(candidate.ID)) == *address.UintValue {
				selected = candidate
				state.session.hasRealm = true
				break
			}
		}
		if !state.session.hasRealm {
			return sendRPCResponse(conn, frame.Header, rpcDenied, nil)
		}
		state.session.selectedRealm = selected
		if _, err := rand.Read(state.session.joinSecret[:]); err != nil {
			return err
		}
		copy(state.session.worldKey[:32], state.session.clientSecret[:])
		copy(state.session.worldKey[32:], state.session.joinSecret[:])
		host, port, err := s.worldEndpoint()
		if err != nil {
			return err
		}
		serverAddresses, err := realm.EncodeServerAddresses(host, port)
		if err != nil {
			return err
		}
		response = append(response,
			bnet.Attribute{Name: "Param_RealmJoinTicket", Value: bnet.BlobVariant([]byte(state.session.legacy.Username))},
			bnet.Attribute{Name: "Param_ServerAddresses", Value: bnet.BlobVariant(serverAddresses)},
			bnet.Attribute{Name: "Param_JoinSecret", Value: bnet.BlobVariant(state.session.joinSecret[:])},
		)
		s.sessionsMu.Lock()
		s.worldSessions[strings.ToUpper(state.session.legacy.Username)] = state.session
		s.sessionsMu.Unlock()
	default:
		// Unknown glue-screen commands are acknowledged with an empty response;
		// several 3.4.3 client paths treat a nonzero RPC status as fatal.
	}
	return sendRPCResponse(conn, frame.Header, rpcOK, bnet.EncodeClientResponse(response))
}

func parseClientSecret(blob []byte) ([32]byte, error) {
	var result [32]byte
	text := strings.TrimSpace(strings.TrimRight(string(blob), "\x00"))
	if !strings.HasPrefix(text, "{") {
		_, remainder, ok := strings.Cut(text, ":")
		if !ok {
			return result, fmt.Errorf("client information lacks JSON prefix")
		}
		text = remainder
	}
	var document struct {
		Info struct {
			Secret []int `json:"secret"`
		} `json:"info"`
	}
	if err := json.Unmarshal([]byte(text), &document); err != nil {
		return result, fmt.Errorf("decode client secret: %w", err)
	}
	if len(document.Info.Secret) < len(result) {
		return result, fmt.Errorf("client secret has %d bytes, want %d", len(document.Info.Secret), len(result))
	}
	for index := range result {
		value := document.Info.Secret[index]
		if value < 0 || value > 255 {
			return result, fmt.Errorf("client secret byte %d out of range", index)
		}
		result[index] = byte(value)
	}
	return result, nil
}

func (s *Server) worldEndpoint() (string, int, error) {
	s.mu.Lock()
	address := s.addresses["world"]
	s.mu.Unlock()
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return "", 0, err
	}
	if host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return "", 0, err
	}
	return host, port, nil
}

func validateLogonRequest(request bnet.LogonRequest) uint32 {
	if request.Program != "WoW" {
		return rpcBadProgram
	}
	if request.ApplicationVersion != SupportedClientBuild {
		return rpcBadVersion
	}
	switch request.Platform {
	case "Win", "Wn64", "Mc64", "MacA":
	default:
		return rpcBadPlatform
	}
	switch request.Locale {
	case "enUS", "koKR", "frFR", "deDE", "zhCN", "zhTW", "esES", "esMX", "ruRU", "ptBR", "itIT":
	default:
		return rpcBadLocale
	}
	return rpcOK
}

func (s *Server) loginURL(platform string, build uint32, locale string) (string, error) {
	s.mu.Lock()
	address := s.addresses["rest"]
	s.mu.Unlock()
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", err
	}
	if host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	// legacy proxy embeds this exact form, including Wn64/build/locale, on RestPort 8081.
	return fmt.Sprintf("https://%s/bnetserver/login/%s/%d/%s/", net.JoinHostPort(host, port), platform, build, locale), nil
}

func sendRPCResponse(w io.Writer, request bnet.Header, status uint32, payload []byte) error {
	header := request
	header.ServiceID = 0xfe
	header.IsResponse = true
	header.Status = status
	return bnet.WriteFrame(w, bnet.Frame{
		Header:  header,
		Payload: payload,
	})
}

func sendRPCRequest(w io.Writer, from bnet.Header, serviceHash, methodID, token uint32, payload []byte) error {
	return bnet.WriteFrame(w, bnet.Frame{
		Header: bnet.Header{
			ServiceHash: serviceHash,
			MethodID:    methodID,
			Token:       token,
			ObjectID:    from.ObjectID,
			Remainder:   append([]byte(nil), from.Remainder...),
		},
		Payload: payload,
	})
}

func (s *Server) serveREST(listener net.Listener) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/bnetserver/login/", s.handleLogin)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "build": s.config.ClientBuild})
	})
	s.http = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
		ErrorLog:          slog.NewLogLogger(s.log.Handler(), slog.LevelWarn),
	}
	return s.http.Serve(&acceptLogger{Listener: listener, log: s.log, name: "rest"})
}

type acceptLogger struct {
	net.Listener
	log  *slog.Logger
	name string
}

func (l *acceptLogger) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	l.log.Info("connection accepted", "service", l.name, "remote", conn.RemoteAddr())
	return conn, nil
}

func (s *Server) handleLogin(w http.ResponseWriter, request *http.Request) {
	s.log.Info("REST login", "method", request.Method, "path", request.URL.Path, "remote", request.RemoteAddr)
	switch request.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, loginFormResponse{
			Type: "LOGIN_FORM",
			Inputs: []loginFormInput{
				{InputID: "account_name", Type: "text", Label: "E-mail", MaxLength: 320},
				{InputID: "password", Type: "password", Label: "Password", MaxLength: 128},
				{InputID: "log_in_submit", Type: "submit", Label: "Log In"},
			},
		})
	case http.MethodPost:
		s.handleLoginPOST(w, request)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{})
	}
}

type loginFormResponse struct {
	Type   string           `json:"type"`
	Inputs []loginFormInput `json:"inputs"`
}

type loginFormInput struct {
	InputID   string `json:"input_id"`
	Type      string `json:"type"`
	Label     string `json:"label"`
	MaxLength int    `json:"max_length,omitempty"`
}

type loginRequest struct {
	Version  string `json:"version"`
	Program  string `json:"program_id"`
	Platform string `json:"platform_id"`
	Inputs   []struct {
		ID    string `json:"input_id"`
		Value string `json:"value"`
	} `json:"inputs"`
}

func (s *Server) handleLoginPOST(w http.ResponseWriter, request *http.Request) {
	platform, build, locale, err := parseLoginPath(request.URL.Path)
	if err != nil || build != s.config.ClientBuild {
		writeAuthError(w, http.StatusBadRequest, "UNSUPPORTED_CLIENT", "Only WoW 3.4.3.54261 is supported.")
		return
	}
	var form loginRequest
	decoder := json.NewDecoder(io.LimitReader(request.Body, 64<<10))
	if err := decoder.Decode(&form); err != nil {
		writeAuthError(w, http.StatusBadRequest, "INVALID_LOGIN_FORM", "The login form could not be decoded.")
		return
	}
	var username, password string
	for _, input := range form.Inputs {
		switch input.ID {
		case "account_name":
			username = input.Value
		case "password":
			password = input.Value
		}
	}
	if strings.TrimSpace(username) == "" || password == "" {
		writeAuthError(w, http.StatusBadRequest, "INVALID_LOGIN_FORM", "Account name and password are required.")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 10*time.Second)
	defer cancel()
	session, err := s.legacyLogin(ctx, s.config.LegacyAuth, legacyauth.Credentials{
		Username: username,
		Password: password,
		Locale:   locale,
	})
	if err != nil {
		var authErr *legacyauth.AuthError
		if errors.As(err, &authErr) {
			status, code, message := legacyAuthError(authErr.Code)
			writeAuthError(w, status, code, message)
			return
		}
		s.log.Error("legacy SRP bridge failed", "platform", platform, "locale", locale, "error", err)
		writeAuthError(w, http.StatusBadGateway, "AUTH_SERVER_UNAVAILABLE", "The legacy authentication server could not complete login.")
		return
	}
	ticket, err := newLoginTicket()
	if err != nil {
		writeAuthError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Could not create a login session.")
		return
	}
	s.sessionsMu.Lock()
	s.sessions[ticket] = &proxySession{
		legacy:          session,
		gameAccountID:   proxyGameAccountID(session.Username),
		knownCharacters: make(map[uint64]struct{}),
	}
	s.sessionsMu.Unlock()
	s.log.Info("legacy SRP login succeeded", "account", session.Username, "realms", len(session.Realms))
	writeJSON(w, http.StatusOK, map[string]any{
		"authentication_state": "DONE",
		"login_ticket":         ticket,
	})
}

func parseLoginPath(path string) (platform string, build uint32, locale string, err error) {
	remainder := strings.TrimPrefix(path, "/bnetserver/login/")
	parts := strings.Split(strings.Trim(remainder, "/"), "/")
	if len(parts) != 3 {
		return "", 0, "", fmt.Errorf("invalid login path")
	}
	parsed, parseErr := strconv.ParseUint(parts[1], 10, 32)
	if parseErr != nil || len(parts[2]) != 4 {
		return "", 0, "", fmt.Errorf("invalid login path")
	}
	return parts[0], uint32(parsed), parts[2], nil
}

func newLoginTicket() (string, error) {
	random := make([]byte, 20)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "RS-" + strings.ToUpper(hex.EncodeToString(random)), nil
}

func proxyGameAccountID(username string) uint64 {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(strings.ToUpper(strings.TrimSpace(username))))
	id := uint64(hash.Sum32())
	if id == 0 {
		return 1
	}
	return id
}

func legacyAuthError(code legacyauth.ResultCode) (int, string, string) {
	switch code {
	case legacyauth.ResultUnknownAccount:
		return http.StatusBadRequest, "UNABLE_TO_DECODE", "Invalid username or password."
	case legacyauth.ResultIncorrectPassword:
		return http.StatusBadRequest, "UNABLE_TO_DECODE", "Invalid password."
	case legacyauth.ResultBanned:
		return http.StatusForbidden, "ACCOUNT_BANNED", "This account is closed."
	case legacyauth.ResultSuspended:
		return http.StatusForbidden, "ACCOUNT_SUSPENDED", "This account is temporarily suspended."
	case legacyauth.ResultVersionInvalid:
		return http.StatusBadRequest, "VERSION_MISMATCH", "The AzerothCore server rejected the 3.3.5a protocol version."
	default:
		return http.StatusBadGateway, "AUTH_FAILED", fmt.Sprintf("Legacy authentication failed (0x%02X).", byte(code))
	}
}

func writeAuthError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"authentication_state": "LOGIN",
		"error_code":           code,
		"error_message":        message,
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) registerInstanceSession(session *proxySession) (uint64, error) {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(strings.ToUpper(session.legacy.Username)))
	accountID := hash.Sum32()
	for range 16 {
		var random [4]byte
		if _, err := rand.Read(random[:]); err != nil {
			return 0, err
		}
		randomKey := uint64(binary.LittleEndian.Uint32(random[:]) & 0x7fffffff)
		key := uint64(accountID) | uint64(1)<<32 | randomKey<<33
		s.sessionsMu.Lock()
		if _, exists := s.instanceSessions[key]; exists {
			s.sessionsMu.Unlock()
			continue
		}
		if session.instanceKey != 0 {
			delete(s.instanceSessions, session.instanceKey)
		}
		session.instanceKey = key
		s.instanceSessions[key] = session
		s.sessionsMu.Unlock()
		return key, nil
	}
	return 0, fmt.Errorf("could not allocate a unique instance connection key")
}

func (s *Server) lookupInstanceSession(key uint64) *proxySession {
	s.sessionsMu.RLock()
	session := s.instanceSessions[key]
	s.sessionsMu.RUnlock()
	return session
}

func (s *Server) claimInstanceSession(key uint64, session *proxySession) bool {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	if s.instanceSessions[key] != session {
		return false
	}
	delete(s.instanceSessions, key)
	return true
}

// worldWireDump (temporary diagnostic) records every outbound modern world
// packet to a JSONL file shaped like the legacy proxy reference capture
// ({"sequence","time","opcode","bodyLength"} + optional "hex"), so a live
// redscarf session can be diffed against a reference packets.jsonl.
// Enabled with REDSCARF_DUMP_FILE=<path>; bodies are included for opcodes in
// REDSCARF_DUMP_OPCODES (comma hex/decimal; default 9928,10187).
var worldWireDump struct {
	file    string
	opcodes map[uint16]bool
	seq     atomic.Uint64
}

func init() {
	if path := os.Getenv("REDSCARF_DUMP_FILE"); path != "" {
		worldWireDump.file = path
		worldWireDump.opcodes = map[uint16]bool{}
		for _, raw := range strings.Split(os.Getenv("REDSCARF_DUMP_OPCODES"), ",") {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			value, err := strconv.ParseUint(raw, 0, 16)
			if err == nil {
				worldWireDump.opcodes[uint16(value)] = true
			}
		}
		if len(worldWireDump.opcodes) == 0 {
			worldWireDump.opcodes[9928] = true
			worldWireDump.opcodes[10187] = true
		}
	}
}

func (session *proxySession) sendInstance(packet modernworld.Packet) error {
	if worldWireDump.file != "" {
		record := fmt.Sprintf("{\"sequence\":%d,\"opcode\":%d,\"bodyLength\":%d",
			worldWireDump.seq.Add(1), packet.Opcode, len(packet.Body))
		if worldWireDump.opcodes[packet.Opcode] {
			record += fmt.Sprintf(",\"hex\":\"%s\"", hex.EncodeToString(packet.Body))
		}
		record += "}\n"
		if handle, openErr := os.OpenFile(worldWireDump.file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); openErr == nil {
			_, _ = handle.WriteString(record)
			_ = handle.Close()
		}
	}
	session.worldMu.Lock()
	defer session.worldMu.Unlock()
	if session.instanceWorld == nil {
		packet.Body = append([]byte(nil), packet.Body...)
		session.pendingInstance = append(session.pendingInstance, packet)
		return nil
	}
	return session.instanceWorld.WritePacket(packet.Opcode, packet.Body)
}

func (s *Server) partySessions(source *proxySession) []*proxySession {
	source.worldMu.Lock()
	partyGUID := source.partyLegacyGUID
	realmID := source.selectedRealm.ID
	source.worldMu.Unlock()
	if partyGUID == 0 {
		return []*proxySession{source}
	}
	s.sessionsMu.RLock()
	candidates := make([]*proxySession, 0, len(s.worldSessions)+len(s.instanceSessions)+1)
	candidates = append(candidates, source)
	seen := map[*proxySession]struct{}{source: {}}
	for _, candidate := range s.worldSessions {
		if _, duplicate := seen[candidate]; duplicate {
			continue
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
	}
	for _, candidate := range s.instanceSessions {
		if _, duplicate := seen[candidate]; duplicate {
			continue
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
	}
	s.sessionsMu.RUnlock()
	result := candidates[:0]
	for _, candidate := range candidates {
		candidate.worldMu.Lock()
		sameParty := candidate.partyLegacyGUID == partyGUID && candidate.selectedRealm.ID == realmID
		candidate.worldMu.Unlock()
		if sameParty {
			result = append(result, candidate)
		}
	}
	return result
}

func (s *Server) broadcastPartyPacket(source *proxySession, packet modernworld.Packet) error {
	for _, target := range s.partySessions(source) {
		if err := target.sendInstance(packet); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) socialWowAccountGUID(source *proxySession, legacyGUID uint64) modernworld.GUID128 {
	if legacyGUID == 0 {
		return modernworld.GUID128{}
	}
	source.worldMu.Lock()
	_, ownCharacter := source.knownCharacters[legacyGUID]
	gameAccountID := source.gameAccountID
	source.worldMu.Unlock()
	if ownCharacter && gameAccountID != 0 {
		return modernworld.ModernWowAccountGUID(gameAccountID)
	}
	// Other players must use the same stable player-derived account GUID as
	// their visible world objects. Resolving an online proxy session's real
	// account ID creates a second identity, so the client cannot attach the
	// online FriendStatus row to that player and continues to show them offline.
	return modernworld.ModernWowAccountGUIDForLegacy(legacyGUID)
}

func (s *Server) socialBNetAccountGUID(source *proxySession, legacyGUID uint64) modernworld.GUID128 {
	if legacyGUID == 0 {
		return modernworld.GUID128{}
	}
	source.worldMu.Lock()
	_, ownCharacter := source.knownCharacters[legacyGUID]
	accountID := uint64(0)
	if source.legacy != nil {
		accountID = proxyGameAccountID(source.legacy.Username)
	}
	source.worldMu.Unlock()
	if ownCharacter && accountID != 0 {
		return modernworld.ModernBNetAccountGUID(accountID)
	}
	return modernworld.ModernBNetAccountGUIDForLegacy(legacyGUID)
}

func (s *Server) scheduleLegacyReadyCheckFinish(session *proxySession, generation uint64) {
	time.AfterFunc(30*time.Second, func() {
		session.worldMu.Lock()
		if session.readyCheckGeneration != generation {
			session.worldMu.Unlock()
			return
		}
		session.readyCheckGeneration++
		legacyConn := session.legacyWorld
		account := ""
		if session.legacy != nil {
			account = session.legacy.Username
		}
		session.worldMu.Unlock()
		if legacyConn == nil {
			return
		}
		if err := legacyConn.WritePacket(legacyworld.MSGRaidReadyCheckFinished, nil); err != nil {
			s.log.Warn("finish ready check failed", "account", account, "error", err)
		}
	})
}

func appendKnownSpell(spells []uint32, spellID uint32) []uint32 {
	for _, existing := range spells {
		if existing == spellID {
			return spells
		}
	}
	return append(spells, spellID)
}

func removeKnownSpell(spells []uint32, spellID uint32) []uint32 {
	kept := spells[:0]
	for _, existing := range spells {
		if existing != spellID {
			kept = append(kept, existing)
		}
	}
	return kept
}

func (session *proxySession) forwardSupercededSpells(body []byte) error {
	oldSpell, newSpell, err := modernworld.ParseLegacySupercededSpells(body)
	if err != nil {
		return err
	}
	// Legacy rank upgrades can send only SUPERCEDED, without LEARNED.
	// Register the new spell before replacing action buttons on the modern
	// client, and keep the cached active spell list consistent with login.
	session.worldMu.Lock()
	session.knownSpells = appendKnownSpell(removeKnownSpell(session.knownSpells, oldSpell), newSpell)
	session.worldMu.Unlock()
	if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGLearnedSpells, Body: modernworld.EncodeLearnedSpells([]uint32{newSpell}, true)}); err != nil {
		return err
	}
	return session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSupercededSpells, Body: modernworld.EncodeSupercededSpells(newSpell, oldSpell)})
}

func sendModernKnownSpells(session *proxySession, spells []uint32, history []modernworld.SpellHistoryEntry, initialLogin bool) error {
	if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSendKnownSpells, Body: modernworld.EncodeSendKnownSpells(spells, initialLogin)}); err != nil {
		return err
	}
	if len(history) == 0 {
		return nil
	}
	return session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSendSpellHistory, Body: modernworld.EncodeSendSpellHistory(history)})
}

func (session *proxySession) fillMultiCastButtonsLocked() {
	if !session.hasActionButtons || !session.hasKnownSpells {
		return
	}
	session.actionButtons = modernworld.FillMultiCastTotemButtons(session.actionButtons, session.knownSpells)
}

// refreshMultiCastActionBar re-sends known spells then the 180-slot action
// table so 3.4.3 fires UPDATE_MULTI_CAST_ACTIONBAR after GetTotemInfo is true.
// MultiCastActionBarFrame (the extra shaman totem bar beside stealth/stance)
// only listens to that event and PLAYER_ENTERING_WORLD.
func refreshMultiCastActionBar(session *proxySession) error {
	session.worldMu.Lock()
	session.fillMultiCastButtonsLocked()
	buttons := append([]int32(nil), session.actionButtons...)
	hasButtons := session.hasActionButtons
	session.worldMu.Unlock()
	if !hasButtons {
		return nil
	}
	return sendModernActionButtons(session, buttons)
}

func sendModernActionButtons(session *proxySession, buttons []int32) error {
	session.worldMu.Lock()
	reason := session.actionButtonReason
	session.worldMu.Unlock()
	// legacy proxy sends this table before the ActivePlayer create, with the legacy
	// reason left unchanged (1 at login) and each packed uint32 zero-extended.
	// A reason-0 table after the bar frames exist clears showgrid, so empty
	// main-bar slots stay hidden.
	body := modernworld.EncodeUpdateActionButtons(buttons, reason)
	return session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGUpdateActionButtons, Body: body})
}

func (s *Server) sendQuestItemProgressForItem(session *proxySession, itemID, count uint32) error {
	session.worldMu.Lock()
	active := modernworld.ActiveQuestIDsFromLegacyFields(session.objectFields[session.currentCharacter])
	objectives := make([]modernworld.QuestItemObjectiveInfo, 0, 2)
	for questID := range active {
		for _, objective := range session.questItemObjectives[questID] {
			if objective.ItemID == itemID {
				objectives = append(objectives, objective)
			}
		}
	}
	session.worldMu.Unlock()
	if count > uint32(^uint16(0)) {
		count = uint32(^uint16(0))
	}
	for _, objective := range objectives {
		body := modernworld.EncodeQuestItemProgress(objective.QuestID, objective.ItemID, uint16(count), objective.Required)
		if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGQuestUpdateAddCredit, Body: body}); err != nil {
			return err
		}
		s.log.Debug("quest item progress", "account", session.legacy.Username, "quest", objective.QuestID, "item", itemID, "count", count, "required", objective.Required)
	}
	return nil
}

func (s *Server) flushQuestItemProgress(session *proxySession) error {
	session.worldMu.Lock()
	if !session.questItemSyncPending {
		session.worldMu.Unlock()
		return nil
	}
	playerFields := session.objectFields[session.currentCharacter]
	if len(playerFields) == 0 {
		session.worldMu.Unlock()
		return nil
	}
	session.questItemSyncPending = false
	active := modernworld.ActiveQuestIDsFromLegacyFields(playerFields)
	counts := make(map[uint32]uint32)
	for guid, fields := range session.objectFields {
		objectType := session.objectTypes[guid]
		if objectType != 1 && objectType != 2 {
			continue
		}
		entry, count := modernworld.LegacyItemEntryAndCount(fields)
		counts[entry] += count
	}
	items := make(map[uint32]struct{})
	for questID := range active {
		for _, objective := range session.questItemObjectives[questID] {
			items[objective.ItemID] = struct{}{}
		}
	}
	session.worldMu.Unlock()
	for itemID := range items {
		if err := s.sendQuestItemProgressForItem(session, itemID, counts[itemID]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) replaceCompletedQuests(session *proxySession, quests []uint32) error {
	blocks := modernworld.QuestCompletedBlocks(quests)
	changed := make(map[int]uint64)
	session.worldMu.Lock()
	for index, value := range blocks {
		var old uint64
		if index < len(session.completedQuestBlocks) {
			old = session.completedQuestBlocks[index]
		}
		if old != value {
			changed[index] = value
		}
	}
	session.completedQuestBlocks = blocks
	created := session.activePlayerCreated
	mapID := session.currentMapID
	player := session.modernGUIDForLegacyLocked(session.currentCharacter)
	session.worldMu.Unlock()
	if !created || len(changed) == 0 {
		return nil
	}
	body, err := modernworld.EncodeQuestCompletedUpdate(mapID, player, changed)
	if err != nil {
		return err
	}
	return session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGUpdateObject, Body: body})
}

func (s *Server) syncCompletedQuests(session *proxySession, body []byte) (int, error) {
	quests, err := modernworld.ParseLegacyQueryQuestsCompletedResponse(body)
	if err != nil {
		return 0, err
	}
	s.log.Debug("completed quest IDs decoded", "account", session.legacy.Username, "quest_ids", quests)
	if err := s.replaceCompletedQuests(session, quests); err != nil {
		return 0, err
	}
	return len(quests), nil
}

func (s *Server) markQuestCompleted(session *proxySession, questID uint32) error {
	session.worldMu.Lock()
	if len(session.completedQuestBlocks) != modernworld.QuestCompletedBlockCount {
		session.completedQuestBlocks = make([]uint64, modernworld.QuestCompletedBlockCount)
	}
	index, value, changed := modernworld.MarkQuestCompleted(session.completedQuestBlocks, questID)
	created := session.activePlayerCreated
	mapID := session.currentMapID
	player := session.modernGUIDForLegacyLocked(session.currentCharacter)
	session.worldMu.Unlock()
	if !created || !changed {
		return nil
	}
	body, err := modernworld.EncodeQuestCompletedUpdate(mapID, player, map[int]uint64{index: value})
	if err != nil {
		return err
	}
	return session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGUpdateObject, Body: body})
}

// repushQuestLogObjectiveFlag re-emits one quest-log slot once its quest is
// first known to be state-only. The ActivePlayer create is sent before the
// modern client's quest-template queries arrive, so a quest that was already
// COMPLETE when the character logged in never received the StateFlags objective
// bit on the initial descriptor. The client queries the template right after
// login (it has to, to render the log row), and this follows that query with a
// corrected slot update so the synthesized objective checks itself.
func (s *Server) repushQuestLogObjectiveFlag(session *proxySession, questID uint32) error {
	session.worldMu.Lock()
	if session.currentCharacter == 0 || !session.activePlayerCreated {
		session.worldMu.Unlock()
		return nil
	}
	playerFields := session.objectFields[session.currentCharacter]
	base := modernworld.FindLegacyQuestLogSlot(playerFields, questID)
	if base < 0 {
		session.worldMu.Unlock()
		return nil
	}
	state := playerFields[base+1]
	if state&0x1 == 0 { // legacy QUEST_STATE_COMPLETE not set: nothing to tick
		session.worldMu.Unlock()
		return nil
	}
	merged := make(map[int]uint32, len(playerFields))
	for field, value := range playerFields {
		merged[field] = value
	}
	modernGUID, knownGUID := session.objectGUIDs[session.currentCharacter]
	mapID := session.currentMapID
	stateOnlyQuests := make(map[uint32]struct{}, len(session.questStateOnly))
	for stateQuest := range session.questStateOnly {
		stateOnlyQuests[stateQuest] = struct{}{}
	}
	session.worldMu.Unlock()
	if !knownGUID {
		return nil
	}
	changed := map[int]uint32{base + 1: state}
	body, _, err := modernworld.EncodeValuesUpdate(modernworld.LegacyObjectUpdate{
		Type: modernworld.LegacyUpdateValues, GUID: session.currentCharacter,
		Values: modernworld.LegacyUpdateValuesBlock{Fields: changed},
	}, modernworld.ValuesUpdateOptions{
		MapID: mapID, ObjectType: 4, GUID: modernGUID, Active: true,
		Fields: merged, StateOnlyQuests: stateOnlyQuests,
	})
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return nil
	}
	s.log.Debug("repush quest-log objective flag", "account", session.legacy.Username, "quest", questID, "state", fmt.Sprintf("0x%x", state))
	return session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGUpdateObject, Body: body})
}

func (session *proxySession) sendRealm(packet modernworld.Packet) error {
	session.worldMu.Lock()
	defer session.worldMu.Unlock()
	if session.realmWorld == nil {
		return net.ErrClosed
	}
	return session.realmWorld.WritePacket(packet.Opcode, packet.Body)
}

func (s *Server) handleModernSpeedAck(session *proxySession, packet modernworld.Packet) (bool, error) {
	legacyOpcode, ok := modernworld.LegacyOpcodeForModernSpeedAck(packet.Opcode)
	if !ok {
		return false, nil
	}
	ack, err := modernworld.ParseModernSpeedAck(packet.Body)
	if err != nil {
		return true, err
	}
	session.worldMu.Lock()
	moverGUID, moverKnown := session.legacyGUIDForModernLocked(ack.Move.Mover)
	transportGUID, transportKnown := session.legacyGUIDForModernLocked(ack.Move.Transport)
	legacyConn := session.legacyWorld
	session.worldMu.Unlock()
	if !moverKnown || (!transportKnown && (ack.Move.Transport.Low != 0 || ack.Move.Transport.High != 0)) {
		return true, fmt.Errorf("speed-ack references an unknown object")
	}
	if legacyConn == nil {
		return true, nil
	}
	body, err := modernworld.EncodeLegacyForceSpeedAck(moverGUID, transportGUID, ack)
	if err != nil {
		return true, err
	}
	return true, legacyConn.WritePacket(legacyOpcode, body)
}

func (s *Server) handleModernMovementFlagAck(session *proxySession, packet modernworld.Packet) (bool, error) {
	legacyOpcode, ok := modernworld.LegacyOpcodeForModernMovementFlagAck(packet.Opcode)
	if !ok {
		return false, nil
	}
	ack, err := modernworld.ParseModernMovementFlagAck(packet.Body)
	if packet.Opcode == modernworld.CMSGMoveKnockBackAck {
		ack, err = modernworld.ParseModernKnockBackAck(packet.Body)
	}
	if err != nil {
		return true, err
	}
	session.worldMu.Lock()
	moverGUID, moverKnown := session.legacyGUIDForModernLocked(ack.Move.Mover)
	transportGUID, transportKnown := session.legacyGUIDForModernLocked(ack.Move.Transport)
	legacyConn := session.legacyWorld
	session.worldMu.Unlock()
	if !moverKnown || (!transportKnown && (ack.Move.Transport.Low != 0 || ack.Move.Transport.High != 0)) {
		return true, fmt.Errorf("movement-flag ack references an unknown object")
	}
	if legacyConn == nil {
		return true, nil
	}
	body, err := modernworld.EncodeLegacyMovementFlagAck(legacyOpcode, moverGUID, transportGUID, ack)
	if err != nil {
		return true, err
	}
	return true, legacyConn.WritePacket(legacyOpcode, body)
}

func (s *Server) handleModernCollisionHeightAck(session *proxySession, packet modernworld.Packet) (bool, error) {
	if packet.Opcode != modernworld.CMSGMoveSetCollisionHeightAck {
		return false, nil
	}
	ack, err := modernworld.ParseModernCollisionHeightAck(packet.Body)
	if err != nil {
		return true, err
	}
	session.worldMu.Lock()
	moverGUID, moverKnown := session.legacyGUIDForModernLocked(ack.Move.Mover)
	transportGUID, transportKnown := session.legacyGUIDForModernLocked(ack.Move.Transport)
	legacyConn := session.legacyWorld
	session.worldMu.Unlock()
	if !moverKnown || (!transportKnown && (ack.Move.Transport.Low != 0 || ack.Move.Transport.High != 0)) {
		return true, fmt.Errorf("collision-height ack references an unknown object")
	}
	if legacyConn == nil {
		return true, nil
	}
	body, err := modernworld.EncodeLegacyCollisionHeightAck(moverGUID, transportGUID, ack)
	if err != nil {
		return true, err
	}
	return true, legacyConn.WritePacket(legacyworld.CMSGMoveSetCollisionHeightAck, body)
}

func (session *proxySession) sendCorpseLocation(loc modernworld.CorpseLocation) error {
	session.worldMu.Lock()
	copied := loc
	session.pendingCorpseLocation = &copied
	player := session.corpseQueryPlayer
	if player.Low == 0 && player.High == 0 && session.currentCharacter != 0 {
		player = session.modernGUIDForLegacyLocked(session.currentCharacter)
	}
	created := session.activePlayerCreated
	session.worldMu.Unlock()
	if !created {
		return nil
	}
	// Hermes 54261 CorpseLocation uses the default (realm) connection, not instance.
	return session.sendRealm(modernworld.Packet{Opcode: modernworld.SMSGCorpseLocation, Body: modernworld.EncodeCorpseLocation(player, loc)})
}

func (session *proxySession) resetWorldObjectsLocked(mapID uint16) {
	session.currentMapID = mapID
	session.encounterFrames = nil
	session.encounterInProgress = false
	session.channelObjects = nil
	session.mapDifficulty = modernworld.MapDifficulty{}
	session.hasMapDifficulty = false
	session.mapReady = true
	session.activePlayerCreated = false
	session.pendingObjectSpells = nil
	session.taxiLoginGraceUntil = time.Time{}
	session.objectGUIDs = make(map[uint64]modernworld.GUID128)
	session.visibleObjectGUIDs = make(map[uint64]struct{})
	session.objectTypes = make(map[uint64]uint8)
	session.objectFields = make(map[uint64]map[int]uint32)
	session.objectPositions = make(map[uint64][3]float32)
	session.transportSync.Reset()
	session.goQueryGUIDs = make(map[uint32]modernworld.GUID128)
	session.queriedCreatures = make(map[uint32]struct{})
	session.creatureDisplay = make(map[uint32]uint32)
	session.queriedMirrorImages = make(map[uint64]struct{})
	session.corpseQueryPlayer = modernworld.GUID128{}
	session.pendingCorpseLocation = nil
	session.initWorldStates = nil
	session.lootLegacyGUID = 0
	session.lastLootTargetLegacy = 0
	session.lootObjModern = modernworld.GUID128{}
	session.masterLootCandidates = nil
	session.masterLootListPending = false
	session.lastMasterLootSentLegacy = 0
}

func (session *proxySession) resetCharacterSessionLocked() {
	session.currentCharacter = 0
	session.guild = modernworld.GuildState{}
	session.encounterFrames = nil
	session.encounterInProgress = false
	session.channelObjects = nil
	session.mapReady = false
	session.hasMapDifficulty = false
	session.mapDifficulty = modernworld.MapDifficulty{}
	session.partySequence = 0
	session.partyLegacyGUID = 0
	session.partyGUID = modernworld.GUID128{}
	session.partyIndex = 0
	session.partyMembers = nil
	session.partyRoles = nil
	session.lfgQueueMode = 0
	session.lfgRoles = 0
	session.partyLootMethod = 0
	session.partyLootMaster = 0
	session.partyLootThreshold = 2
	session.tradeID = 0
	session.tradeClientState = 0
	session.tradeServerState = 0
	session.tradeActive = false
	session.lootRolls = nil
	session.completedLootRolls = nil
	session.readyCheckGeneration++
	session.pendingPlayerNameQueries = nil
	session.pendingPlayerNameModern = nil
	session.pendingPetNameGUIDs = nil
	session.pendingNamedChats = nil
	session.playerIdentities = nil
	session.arenaTeams = nil
	session.battlegroundQueues = nil
	session.lastWhisperTarget = ""
	session.currentTaxiNode = 0
	session.usableTaxiNodes = nil
	session.taxiReplyPending = false
	session.taxiLoginGraceUntil = time.Time{}
	session.actionButtons = nil
	session.actionButtonReason = 0
	session.hasActionButtons = false
	session.knownSpells = nil
	session.spellHistory = nil
	session.hasKnownSpells = false
	session.knownSpellsSent = false
	session.unlearnSpells = nil
	session.playerAuras = nil
	session.playerAuraBySlot = nil
	session.pendingCasts = nil
	session.resetSpellQueueLocked()
	session.lastStableMaster = 0
	session.completedCastIDs = nil
	session.unmatchedSpellGoSeq = 0
	session.unmatchedSpellStarts = nil
	session.unmatchedSpellGoes = nil
	session.runeCooldownStarted = [6]time.Time{}
	session.runeState = modernworld.LegacyRuneState{}
	session.hasRuneState = false
	session.questItemSyncPending = false
	session.completedQuestBlocks = nil
	session.completedQuestSyncPending = false
	session.completedQuestSyncRequested = false
	session.waitingForNewWorld = false
	session.waitingForWorldPortAck = false
	session.currentInteractedGO = 0
	session.resetWorldObjectsLocked(0)
	session.mapReady = false
}

func (s *Server) queryCreatureTemplate(session *proxySession, entry uint32) error {
	if entry == 0 {
		return nil
	}
	session.worldMu.Lock()
	if session.queriedCreatures == nil {
		session.queriedCreatures = make(map[uint32]struct{})
	}
	if _, seen := session.queriedCreatures[entry]; seen {
		session.worldMu.Unlock()
		return nil
	}
	session.queriedCreatures[entry] = struct{}{}
	legacyConn := session.legacyWorld
	session.worldMu.Unlock()
	if legacyConn == nil {
		return nil
	}
	return legacyConn.WritePacket(legacyworld.CMSGCreatureQuery, modernworld.EncodeLegacyCreatureQuery(entry))
}

func (session *proxySession) rememberStableMasterLocked(legacyGUID uint64) {
	if legacyGUID != 0 {
		session.lastStableMaster = legacyGUID
	}
}

func (s *Server) sendStabledPets(session *proxySession, list modernworld.LegacyStabledPets) error {
	session.worldMu.Lock()
	session.rememberStableMasterLocked(list.Master)
	player := session.modernGUIDForLegacyLocked(session.currentCharacter)
	master := session.modernGUIDForLegacyLocked(list.Master)
	mapID := session.currentMapID
	created := session.activePlayerCreated
	displays := make(map[uint32]uint32, len(session.creatureDisplay))
	for entry, display := range session.creatureDisplay {
		displays[entry] = display
	}
	var currentDisplay uint32
	if summon := modernworld.LegacySummonGUID(session.objectFields[session.currentCharacter]); summon != 0 {
		currentDisplay = modernworld.LegacyUnitDisplayID(session.objectFields[summon])
	}
	session.worldMu.Unlock()

	missing := modernworld.AssignStablePetDisplays(&list, displays, currentDisplay)
	for _, entry := range missing {
		if err := s.queryCreatureTemplate(session, entry); err != nil {
			return err
		}
	}

	// 54261 dropped SMSG_PET_STABLE_LIST. legacy proxy opens the frame with
	// SMSG_PET_GUIDS plus an ActivePlayer HasPetStable VALUES update
	// (reference capture seq 562/565). It does not send
	// NPC_INTERACTION type 22.
	var guids []modernworld.GUID128
	session.worldMu.Lock()
	if summon := modernworld.LegacySummonGUID(session.objectFields[session.currentCharacter]); summon != 0 {
		guids = append(guids, session.modernGUIDForLegacyLocked(summon))
	}
	session.worldMu.Unlock()
	if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGPetGuids, Body: modernworld.EncodePetGuids(guids)}); err != nil {
		return err
	}
	if !created {
		s.log.Debug("stabled pets skipped values update", "account", session.legacy.Username, "pets", len(list.Pets), "slots", list.NumStableSlots)
		return nil
	}
	body, err := modernworld.EncodePetStableValuesUpdate(mapID, player, master, list)
	if err != nil {
		s.log.Warn("encode pet-stable values failed", "account", session.legacy.Username, "error", err)
		return nil
	}
	s.log.Debug("stabled pets", "account", session.legacy.Username, "pets", len(list.Pets), "slots", list.NumStableSlots, "missing_display", len(missing))
	if len(list.Pets) > 0 {
		pet := list.Pets[0]
		s.log.Debug("stabled pet", "account", session.legacy.Username, "number", pet.PetNumber, "creature", pet.CreatureID, "display", pet.DisplayID, "flags", pet.Flags, "slot", pet.PetSlot, "name", pet.Name)
	}
	return session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGUpdateObject, Body: body})
}

func (s *Server) requestMirrorImageData(session *proxySession, legacyGUID uint64) error {
	if legacyGUID == 0 {
		return nil
	}
	session.worldMu.Lock()
	if session.queriedMirrorImages == nil {
		session.queriedMirrorImages = make(map[uint64]struct{})
	}
	if _, seen := session.queriedMirrorImages[legacyGUID]; seen {
		session.worldMu.Unlock()
		return nil
	}
	session.queriedMirrorImages[legacyGUID] = struct{}{}
	legacyConn := session.legacyWorld
	session.worldMu.Unlock()
	if legacyConn == nil {
		return nil
	}
	s.log.Debug("request mirror-image data", "account", session.legacy.Username, "legacy", fmt.Sprintf("0x%x", legacyGUID))
	return legacyConn.WritePacket(legacyworld.CMSGGetMirrorImageData, modernworld.EncodeLegacyGetMirrorImageData(legacyGUID))
}

// transportProgressReplays re-sends the realm's MotionTransport path progress,
// extrapolated on the realm's own timer, while a hull is moving. AzerothCore
// reports that word only on state changes, so the client otherwise animates the
// ICC gunships on its own clock for the whole 39-second climb. Disabled unless
// REDSCARF_TRANSPORT_RESYNC is set.
func (s *Server) transportProgressReplays(session *proxySession, mapID uint16) [][]byte {
	if !modernworld.TransportResyncEnabled() {
		return nil
	}
	type replay struct {
		modern modernworld.GUID128
		legacy uint64
		merged map[int]uint32
		delta  map[int]uint32
	}
	session.worldMu.Lock()
	resyncs := session.transportSync.Due(time.Now())
	replays := make([]replay, 0, len(resyncs))
	for _, resync := range resyncs {
		modernGUID, known := session.objectGUIDs[resync.GUID]
		if !known {
			continue
		}
		cached := session.objectFields[resync.GUID]
		merged := make(map[int]uint32, len(cached)+len(resync.Fields))
		for field, value := range cached {
			merged[field] = value
		}
		for field, value := range resync.Fields {
			merged[field] = value
		}
		replays = append(replays, replay{modernGUID, resync.GUID, merged, resync.Fields})
	}
	session.worldMu.Unlock()
	bodies := make([][]byte, 0, len(replays))
	for _, entry := range replays {
		body, _, err := modernworld.EncodeValuesUpdate(modernworld.LegacyObjectUpdate{
			Type: modernworld.LegacyUpdateValues, GUID: entry.legacy, ObjectType: 5,
			Values: modernworld.LegacyUpdateValuesBlock{Fields: entry.delta},
		}, modernworld.ValuesUpdateOptions{
			MapID: mapID, ObjectType: 5, GUID: entry.modern, Fields: entry.merged,
		})
		if err != nil {
			s.log.Warn("replay transport path progress failed", "account", session.legacy.Username,
				"guid", fmt.Sprintf("0x%x", entry.legacy), "error", err)
			continue
		}
		if len(body) == 0 {
			continue
		}
		s.log.Debug("replay transport path progress",
			"account", session.legacy.Username,
			"guid", fmt.Sprintf("0x%x", entry.legacy),
			"progress", fmt.Sprintf("0x%x", entry.delta[14]>>16),
			"bytes", len(body))
		bodies = append(bodies, body)
	}
	return bodies
}

// rememberObjectCreateLocked updates the object cache before the current
// legacy update batch is emitted. Movement/values packets can arrive between
// legacy batches, so the proxy must know the modern GUID immediately while
// keeping client visibility atomic at the batch boundary.
func (session *proxySession) rememberObjectCreateLocked(update modernworld.LegacyObjectUpdate, mapID uint16) {
	modernGUID := modernworld.ModernGUIDForCreate(update, mapID)
	if session.objectGUIDs == nil {
		session.objectGUIDs = make(map[uint64]modernworld.GUID128)
	}
	if session.objectTypes == nil {
		session.objectTypes = make(map[uint64]uint8)
	}
	if session.objectFields == nil {
		session.objectFields = make(map[uint64]map[int]uint32)
	}
	if session.objectPositions == nil {
		session.objectPositions = make(map[uint64][3]float32)
	}
	session.objectGUIDs[update.GUID] = modernGUID
	session.objectTypes[update.GUID] = update.ObjectType
	if update.Movement != nil {
		session.objectPositions[update.GUID] = [3]float32{update.Movement.X, update.Movement.Y, update.Movement.Z}
	}
	fields := make(map[int]uint32, len(update.Values.Fields))
	for field, value := range update.Values.Fields {
		fields[field] = value
	}
	session.objectFields[update.GUID] = fields
}

func (session *proxySession) markObjectVisibleLocked(legacyGUID uint64) {
	if session.visibleObjectGUIDs == nil {
		session.visibleObjectGUIDs = make(map[uint64]struct{})
	}
	session.visibleObjectGUIDs[legacyGUID] = struct{}{}
}

func (session *proxySession) forgetObjectVisibleLocked(legacyGUID uint64) {
	delete(session.visibleObjectGUIDs, legacyGUID)
	session.encounterFrames.Hide(legacyGUID)
}

// unknownSpellObjectLocked reports a caster object that has not yet been
// written to the modern client. Player-initiated casts are deliberately
// excluded: their client-side cast state must continue to match immediately.
func (session *proxySession) unknownSpellObjectLocked(cast modernworld.SpellCastData) (uint64, bool) {
	if session.currentCharacter != 0 &&
		(cast.CasterGUID == session.currentCharacter || cast.CasterUnit == session.currentCharacter) {
		return 0, false
	}
	seen := make(map[uint64]struct{}, 2)
	for _, guid := range []uint64{cast.CasterGUID, cast.CasterUnit} {
		if guid == 0 || guid == session.currentCharacter {
			continue
		}
		if _, duplicate := seen[guid]; duplicate {
			continue
		}
		seen[guid] = struct{}{}
		if _, visible := session.visibleObjectGUIDs[guid]; !visible {
			return guid, true
		}
	}
	return 0, false
}

func cloneSpellCastData(cast modernworld.SpellCastData) modernworld.SpellCastData {
	clone := cast
	clone.HitTargets = append([]uint64(nil), cast.HitTargets...)
	clone.MissTargets = append([]modernworld.SpellMiss(nil), cast.MissTargets...)
	if cast.Target.Src != nil {
		value := *cast.Target.Src
		clone.Target.Src = &value
	}
	if cast.Target.Dst != nil {
		value := *cast.Target.Dst
		clone.Target.Dst = &value
	}
	if cast.Target.Orientation != nil {
		value := *cast.Target.Orientation
		clone.Target.Orientation = &value
	}
	if cast.Target.MapID != nil {
		value := *cast.Target.MapID
		clone.Target.MapID = &value
	}
	return clone
}

func (session *proxySession) queueObjectSpellLocked(cast modernworld.SpellCastData, start bool) bool {
	if len(session.pendingObjectSpells) >= maxPendingObjectSpells {
		return false
	}
	session.pendingObjectSpells = append(session.pendingObjectSpells, pendingObjectSpell{
		cast: cloneSpellCastData(cast), start: start,
	})
	return true
}

func (session *proxySession) takeReadyObjectSpellsLocked() []pendingObjectSpell {
	if len(session.pendingObjectSpells) == 0 {
		return nil
	}
	ready := make([]pendingObjectSpell, 0, len(session.pendingObjectSpells))
	remaining := session.pendingObjectSpells[:0]
	for _, pending := range session.pendingObjectSpells {
		if _, waiting := session.unknownSpellObjectLocked(pending.cast); waiting {
			remaining = append(remaining, pending)
		} else {
			ready = append(ready, pending)
		}
	}
	if len(remaining) == 0 {
		session.pendingObjectSpells = nil
	} else {
		session.pendingObjectSpells = remaining
	}
	return ready
}

func (s *Server) finishActivePlayerLogin(session *proxySession, mapID uint16, currentCharacter uint64, initWorldStates []byte) error {
	session.worldMu.Lock()
	session.activePlayerCreated = true
	session.taxiLoginGraceUntil = time.Now().Add(time.Second)
	session.fillMultiCastButtonsLocked()
	// Re-send known spells after the player object exists. AC emits
	// SMSG_INITIAL_SPELLS before AddToMap. The action-button table was already
	// forwarded when the legacy packet arrived, before this create. Do not
	// send another copy here: a reason-0 table after the bar frames exist
	// clears showgrid, so empty main-bar slots stay hidden.
	sendKnown := session.hasKnownSpells
	knownSpells := append([]uint32(nil), session.knownSpells...)
	var spellHistory []modernworld.SpellHistoryEntry
	playerAuras := append([]modernworld.AuraInfo(nil), session.playerAuras...)
	if sendKnown {
		session.knownSpellsSent = true
		spellHistory = append([]modernworld.SpellHistoryEntry(nil), session.spellHistory...)
	}
	if len(playerAuras) > 0 {
		modernworld.MarkLocalPlayerAuraFlags(playerAuras)
		modernworld.RemapMountSpeedAuras(playerAuras)
		for index := range playerAuras {
			if playerAuras[index].CastUnit.Low != 0 {
				playerAuras[index].CastUnit = session.modernGUIDForLegacyLocked(playerAuras[index].CastUnit.Low)
			}
			if playerAuras[index].HasData && playerAuras[index].VisualID == 0 {
				if visual := session.visualIDForSpellLocked(playerAuras[index].SpellID); visual != 0 {
					playerAuras[index].VisualID = visual
				}
			}
		}
	}
	playerGUID := session.modernGUIDForLegacyLocked(currentCharacter)
	session.worldMu.Unlock()
	worldReady := []modernworld.Packet{
		{Opcode: modernworld.SMSGUpdateObject, Body: modernworld.EncodeEmptyUpdateObject(mapID)},
	}
	if len(playerAuras) > 0 {
		worldReady = append(worldReady, modernworld.Packet{Opcode: modernworld.SMSGAuraUpdate, Body: modernworld.EncodeAuraUpdate(playerGUID, mapID, true, playerAuras)})
	}
	worldReady = append(worldReady, modernworld.Packet{Opcode: modernworld.SMSGPhaseShiftChange, Body: modernworld.EncodeDefaultPhaseShift(currentCharacter)})
	if len(initWorldStates) == 0 {
		initWorldStates = modernworld.EncodeInitWorldStates(modernworld.InitWorldStates{MapID: uint32(mapID)})
	}
	worldReady = append(worldReady, modernworld.Packet{Opcode: modernworld.SMSGInitWorldStates, Body: initWorldStates})
	for _, readyPacket := range worldReady {
		if err := session.sendInstance(readyPacket); err != nil {
			return err
		}
	}
	if sendKnown {
		if err := sendModernKnownSpells(session, knownSpells, spellHistory, true); err != nil {
			return err
		}
		s.log.Debug("send known spells after active player", "account", session.legacy.Username, "spells", len(knownSpells), "history", len(spellHistory))
	}
	session.worldMu.Lock()
	hasRunes := session.hasRuneState
	runeState := session.runeState
	session.worldMu.Unlock()
	if hasRunes {
		if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGResyncRunes, Body: modernworld.EncodeRuneResync(runeState)}); err != nil {
			return err
		}
	}
	if err := s.seedLocalPlayerName(session, currentCharacter, playerGUID); err != nil {
		return err
	}
	return nil
}

func (s *Server) seedLocalPlayerName(session *proxySession, currentCharacter uint64, playerGUID modernworld.GUID128) error {
	session.worldMu.Lock()
	identity, level, ok := session.playerNameIdentityLocked(currentCharacter)
	realmAddress := realm.Address(session.selectedRealm.ID)
	if ok {
		session.rememberPlayerNameLocked(identity.GUID, identity.Name)
		if session.playerIdentities == nil {
			session.playerIdentities = make(map[uint64]modernworld.LegacyNameIdentity)
		}
		session.playerIdentities[identity.GUID] = identity
	}
	session.worldMu.Unlock()
	if !ok {
		return nil
	}
	body, err := modernworld.EncodeQueryPlayerNameLookup(playerGUID, identity, level, realmAddress,
		s.socialWowAccountGUID(session, identity.GUID), s.socialBNetAccountGUID(session, identity.GUID))
	if err != nil {
		return err
	}
	s.log.Debug("seed local player name", "account", session.legacy.Username, "name", identity.Name)
	return session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGQueryPlayerNames, Body: body})
}

func (s *Server) handleModernTeleportPacket(session *proxySession, packet modernworld.Packet) (bool, error) {
	switch packet.Opcode {
	case modernworld.CMSGWorldPortResponse:
		if err := modernworld.ParseWorldPortResponse(packet.Body); err != nil {
			return true, err
		}
		session.worldMu.Lock()
		session.waitingForWorldPortAck = false
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.MSGMoveWorldportAck, nil)
	case modernworld.CMSGMoveTeleportAck:
		ack, err := modernworld.ParseMoveTeleportAck(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacyGUIDForModernLocked(ack.Mover)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known || legacyConn == nil {
			return true, fmt.Errorf("move-teleport-ack references an unknown object")
		}
		return true, legacyConn.WritePacket(legacyworld.MSGMoveTeleportAck, modernworld.EncodeLegacyMoveTeleportAck(legacyGUID, ack))
	case modernworld.CMSGLoadingScreenNotify:
		_, err := modernworld.ParseLoadingScreenNotify(packet.Body)
		return true, err
	case modernworld.CMSGSuspendTokenResponse:
		_, err := modernworld.ParseSuspendTokenResponse(packet.Body)
		return true, err
	default:
		return false, nil
	}
}

func (s *Server) handleAccountDataPacket(session *proxySession, packet modernworld.Packet) (bool, error) {
	switch packet.Opcode {
	case modernworld.CMSGRequestAccountData:
		request, err := modernworld.ParseAccountDataRequest(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		stored := session.accountData[request.DataType]
		var response modernworld.AccountData
		if stored != nil {
			response = *stored
			response.CompressedData = append([]byte(nil), stored.CompressedData...)
		} else {
			response = modernworld.AccountData{Time: time.Now().Unix(), DataType: request.DataType}
		}
		session.worldMu.Unlock()
		response.PlayerGUID = request.PlayerGUID
		body, err := modernworld.EncodeAccountDataUpdate(response)
		if err != nil {
			return true, err
		}
		return true, session.sendRealm(modernworld.Packet{Opcode: modernworld.SMSGUpdateAccountData, Body: body})
	case modernworld.CMSGUpdateAccountData:
		update, err := modernworld.ParseAccountDataUpdate(packet.Body)
		if err != nil {
			return true, err
		}
		copy := update
		copy.CompressedData = append([]byte(nil), update.CompressedData...)
		session.worldMu.Lock()
		session.accountData[update.DataType] = &copy
		session.worldMu.Unlock()
		return true, nil
	default:
		return false, nil
	}
}

func (s *Server) handleSimpleModernSystemPacket(session *proxySession, packet modernworld.Packet) (bool, error) {
	if session.legacyWorld == nil {
		return false, nil
	}
	switch packet.Opcode {
	case modernworld.CMSGTimeSyncResponse:
		if len(packet.Body) != 8 {
			return true, fmt.Errorf("time-sync response has %d bytes, want 8", len(packet.Body))
		}
		return true, session.legacyWorld.WritePacket(legacyworld.CMSGTimeSyncResponse, packet.Body)
	case modernworld.CMSGTutorialFlag:
		action, err := modernworld.ParseTutorialAction(packet.Body)
		if err != nil {
			return true, err
		}
		switch action.Action {
		case modernworld.TutorialUpdate:
			return true, session.legacyWorld.WritePacket(legacyworld.CMSGTutorialFlag, binary.LittleEndian.AppendUint32(nil, action.Bit))
		case modernworld.TutorialClear:
			return true, session.legacyWorld.WritePacket(legacyworld.CMSGTutorialClear, nil)
		case modernworld.TutorialReset:
			return true, session.legacyWorld.WritePacket(legacyworld.CMSGTutorialReset, nil)
		}
	}
	return false, nil
}

func (session *proxySession) activateInstance(world *modernworld.PacketConn) error {
	session.worldMu.Lock()
	defer session.worldMu.Unlock()
	session.instanceWorld = world
	for _, packet := range session.pendingInstance {
		if err := world.WritePacket(packet.Opcode, packet.Body); err != nil {
			return err
		}
	}
	session.pendingInstance = nil
	return nil
}

func (session *proxySession) clearInstance(world *modernworld.PacketConn) {
	session.worldMu.Lock()
	if session.instanceWorld == world {
		session.instanceWorld = nil
	}
	session.worldMu.Unlock()
}

func (s *Server) serveWorld(listener net.Listener) error {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		s.log.Info("modern world connection accepted", "remote", conn.RemoteAddr())
		go s.handleWorld(conn)
	}
}

func (s *Server) handleWorld(conn net.Conn) {
	defer conn.Close()
	world := modernworld.NewServerConn(conn)
	challenge, err := world.Accept()
	if err != nil {
		s.log.Debug("modern world initializer failed", "remote", conn.RemoteAddr(), "error", err)
		return
	}
	packet, err := world.ReadPacket()
	if err != nil {
		s.log.Warn("modern world authentication packet missing", "remote", conn.RemoteAddr(), "error", err)
		return
	}
	switch packet.Opcode {
	case modernworld.CMSGAuthSession:
		s.handleRealmWorld(world, challenge, packet, conn.RemoteAddr())
	case modernworld.CMSGAuthContinuedSession:
		s.handleInstanceWorld(world, challenge, packet, conn.RemoteAddr())
	default:
		s.log.Warn("unexpected first modern world opcode", "remote", conn.RemoteAddr(), "opcode", packet.Opcode)
	}
}

func (s *Server) handleRealmWorld(world *modernworld.PacketConn, challenge modernworld.AuthChallenge, packet modernworld.Packet, remote net.Addr) {
	auth, err := modernworld.ParseAuthSession(packet.Body)
	if err != nil {
		s.log.Warn("modern world auth session malformed", "remote", remote, "error", err)
		return
	}
	s.sessionsMu.RLock()
	session := s.worldSessions[strings.ToUpper(auth.RealmJoinTicket)]
	s.sessionsMu.RUnlock()
	if session == nil || !session.hasRealm || auth.RegionID != 1 || auth.BattlegroupID != 1 || auth.RealmID != session.selectedRealm.ID {
		s.log.Warn("modern world join ticket rejected", "remote", remote, "ticket", auth.RealmJoinTicket, "region", auth.RegionID, "battlegroup", auth.BattlegroupID, "realm", auth.RealmID)
		return
	}
	sessionKey, encryptionKey := modernworld.DeriveKeys(session.worldKey, challenge.ServerChallenge, auth.LocalChallenge)
	session.modernSessionKey = sessionKey
	session.worldMu.Lock()
	session.realmWorld = world
	session.worldMu.Unlock()
	defer func() {
		session.worldMu.Lock()
		if session.realmWorld == world {
			session.realmWorld = nil
		}
		session.worldMu.Unlock()
	}()

	legacyCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	legacyConn, err := s.legacyWorld(legacyCtx, session.selectedRealm, session.legacy)
	cancel()
	if err != nil {
		s.log.Error("legacy world authentication failed", "account", session.legacy.Username, "realm", session.selectedRealm.Name, "error", err)
		return
	}
	session.legacyWorld = legacyConn
	if legacyConn != nil {
		defer legacyConn.Close()
	}

	enterEncrypted, err := modernworld.EncodeEnterEncryptedMode(encryptionKey)
	if err != nil {
		s.log.Warn("build enter-encrypted-mode failed", "account", session.legacy.Username, "error", err)
		return
	}
	if err := world.WritePacket(modernworld.SMSGEnterEncryptedMode, enterEncrypted); err != nil {
		s.log.Warn("send enter-encrypted-mode failed", "account", session.legacy.Username, "error", err)
		return
	}
	ack, err := world.ReadPacket()
	if err != nil || ack.Opcode != modernworld.CMSGEnterEncryptedModeAck {
		s.log.Warn("enter-encrypted-mode acknowledgement missing", "account", session.legacy.Username, "opcode", ack.Opcode, "error", err)
		return
	}
	if err := world.EnableEncryption(encryptionKey[:]); err != nil {
		s.log.Warn("enable modern world encryption failed", "account", session.legacy.Username, "error", err)
		return
	}
	authResponse := modernworld.EncodeAuthResponse(realm.Address(session.selectedRealm.ID), session.selectedRealm.Name, time.Now())
	if err := world.WritePacket(modernworld.SMSGAuthResponse, authResponse); err != nil {
		s.log.Warn("send modern world auth response failed", "account", session.legacy.Username, "error", err)
		return
	}
	if err := sendWorldGlue(world, realm.Address(session.selectedRealm.ID)); err != nil {
		s.log.Warn("send modern world glue packets failed", "account", session.legacy.Username, "error", err)
		return
	}
	s.log.Info("modern and legacy world authentication succeeded", "account", session.legacy.Username, "realm", session.selectedRealm.Name)

	if legacyConn != nil {
		go s.relayLegacyWorld(session, world, legacyConn)
	}

	for {
		packet, err := world.ReadPacket()
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				s.log.Debug("modern world connection ended", "account", session.legacy.Username, "error", err)
			}
			return
		}
		if handled, ackErr := s.handleModernSpeedAck(session, packet); handled {
			if ackErr != nil {
				s.log.Warn("modern player speed-ack failed", "account", session.legacy.Username, "opcode", packet.Opcode, "error", ackErr)
			}
			continue
		}
		if handled, ackErr := s.handleModernMovementFlagAck(session, packet); handled {
			if ackErr != nil {
				s.log.Warn("modern movement-flag ack failed", "account", session.legacy.Username, "opcode", packet.Opcode, "error", ackErr)
			}
			continue
		}
		if handled, ackErr := s.handleModernCollisionHeightAck(session, packet); handled {
			if ackErr != nil {
				s.log.Warn("modern collision-height ack failed", "account", session.legacy.Username, "error", ackErr)
			}
			continue
		}
		if legacyOpcode, movementPacket := modernworld.LegacyOpcodeForModernMovement(packet.Opcode); movementPacket {
			movement, parseErr := modernworld.ParseModernPlayerMovement(packet.Body)
			if parseErr != nil {
				s.log.Warn("modern player movement malformed", "account", session.legacy.Username, "opcode", packet.Opcode, "error", parseErr)
				continue
			}
			session.worldMu.Lock()
			moverGUID, moverKnown := session.legacyGUIDForModernLocked(movement.Mover)
			transportGUID, transportKnown := session.legacyGUIDForModernLocked(movement.Transport)
			if moverKnown {
				if session.objectPositions == nil {
					session.objectPositions = make(map[uint64][3]float32)
				}
				session.objectPositions[moverGUID] = [3]float32{movement.Move.X, movement.Move.Y, movement.Move.Z}
			}
			legacyConn := session.legacyWorld
			session.worldMu.Unlock()
			if !moverKnown || (!transportKnown && (movement.Transport.Low != 0 || movement.Transport.High != 0)) {
				s.log.Warn("modern player movement references an unknown object", "account", session.legacy.Username, "opcode", packet.Opcode,
					"mover_low", movement.Mover.Low, "transport_low", movement.Transport.Low)
				continue
			}
			legacyBody, encodeErr := modernworld.EncodeLegacyPlayerMovement(movement, moverGUID, transportGUID)
			if encodeErr != nil {
				s.log.Warn("encode legacy player movement failed", "account", session.legacy.Username, "opcode", packet.Opcode, "error", encodeErr)
				continue
			}
			if legacyConn == nil {
				continue
			}
			if err := legacyConn.WritePacket(legacyOpcode, legacyBody); err != nil {
				return
			}
			if packet.Opcode == modernworld.CMSGMoveDismissVehicle {
				s.log.Info("forward controlled vehicle dismiss", "modern_opcode", packet.Opcode, "legacy_opcode", legacyOpcode, "mover", fmt.Sprintf("0x%x", moverGUID), "bytes", len(legacyBody))
			}
			continue
		}
		switch packet.Opcode {
		case modernworld.CMsgPing:
			if len(packet.Body) >= 4 {
				serial := binary.LittleEndian.Uint32(packet.Body[:4])
				pong := binary.LittleEndian.AppendUint32(nil, serial)
				_ = world.WritePacket(modernworld.SMsgPong, pong)
			}
		case modernworld.CMSGEnumCharacters:
			if legacyConn == nil {
				s.log.Warn("character enumeration requested without a legacy world connection", "account", session.legacy.Username)
				continue
			}
			if err := legacyConn.WritePacket(legacyworld.CMSGEnumCharacters, nil); err != nil {
				s.log.Warn("forward character enumeration to legacy world failed", "account", session.legacy.Username, "error", err)
				return
			}
		case modernworld.CMSGGenerateRandomCharacterName:
			if len(packet.Body) != 2 {
				s.log.Warn("random character name request malformed", "account", session.legacy.Username, "bytes", len(packet.Body))
				continue
			}
			if err := world.WritePacket(modernworld.SMSGGenerateRandomCharacterName, modernworld.EncodeRandomCharacterNameUnavailable()); err != nil {
				return
			}
		case modernworld.CMSGCreateCharacter:
			if legacyConn == nil {
				s.log.Warn("character creation requested without a legacy world connection", "account", session.legacy.Username)
				continue
			}
			request, err := modernworld.ParseCreateCharacter(packet.Body)
			if err != nil {
				s.log.Warn("modern create-character request malformed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			session.pendingCreateName = request.Name
			session.pendingCreateCode = 0
			session.awaitingCreateEnum = false
			session.worldMu.Unlock()
			if err := legacyConn.WritePacket(legacyworld.CMSGCreateCharacter, modernworld.EncodeLegacyCreateCharacter(request)); err != nil {
				s.log.Warn("forward character creation to legacy world failed", "account", session.legacy.Username, "error", err)
				return
			}
		case modernworld.CMSGCharDelete:
			if legacyConn == nil {
				s.log.Warn("character deletion requested without a legacy world connection", "account", session.legacy.Username)
				continue
			}
			guid, err := modernworld.ParseCharacterGUID(packet.Body)
			if err != nil {
				s.log.Warn("modern character-delete request malformed", "account", session.legacy.Username, "error", err)
				continue
			}
			legacyBody := binary.LittleEndian.AppendUint64(nil, guid)
			if err := legacyConn.WritePacket(legacyworld.CMSGCharDelete, legacyBody); err != nil {
				s.log.Warn("forward character deletion to legacy world failed", "account", session.legacy.Username, "error", err)
				return
			}
		case modernworld.CMSGCharacterRenameRequest:
			if legacyConn == nil {
				s.log.Warn("character rename requested without a legacy world connection", "account", session.legacy.Username)
				continue
			}
			request, err := modernworld.ParseCharacterRenameRequest(packet.Body)
			if err != nil {
				s.log.Warn("modern character-rename request malformed", "account", session.legacy.Username, "error", err)
				continue
			}
			legacyGUID, known := uint64(0), false
			session.worldMu.Lock()
			if mapped, ok := session.legacyGUIDForModernLocked(request.GUID); ok {
				legacyGUID, known = mapped, true
			} else {
				for candidate := range session.knownCharacterInfo {
					if modernworld.ModernGUIDForLegacy(candidate, 0) == request.GUID {
						legacyGUID, known = candidate, true
						break
					}
				}
			}
			session.worldMu.Unlock()
			if !known || legacyGUID == 0 {
				s.log.Warn("rename request references an unknown character", "account", session.legacy.Username, "guid", fmt.Sprintf("0x%x", request.GUID.Low))
				continue
			}
			if err := legacyConn.WritePacket(legacyworld.CMSGCharacterRenameRequest, modernworld.EncodeLegacyCharacterRename(legacyGUID, request.Name)); err != nil {
				s.log.Warn("forward character rename to legacy world failed", "account", session.legacy.Username, "error", err)
				return
			}
		case modernworld.CMSGPlayerLogin:
			if legacyConn == nil {
				s.log.Warn("player login requested without a legacy world connection", "account", session.legacy.Username)
				continue
			}
			login, err := modernworld.ParsePlayerLogin(packet.Body)
			if err != nil {
				s.log.Warn("modern player-login request malformed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			_, knownCharacter := session.knownCharacters[login.GUID]
			session.pendingInstance = nil
			if knownCharacter {
				session.currentCharacter = login.GUID
				session.guild = modernworld.GuildState{ID: session.knownCharacterInfo[login.GUID].GuildID}
				session.currentMapID = 0
				session.currentZoneID = 0
				session.currentTaxiNode = 0
				session.usableTaxiNodes = nil
				session.taxiReplyPending = false
				session.taxiLoginGraceUntil = time.Time{}
				session.actionButtons = nil
				session.actionButtonReason = 0
				session.hasActionButtons = false
				session.knownSpells = nil
				session.spellHistory = nil
				session.hasKnownSpells = false
				session.knownSpellsSent = false
				session.unlearnSpells = nil
				session.activePlayerCreated = false
				session.pendingObjectSpells = nil
				session.objectGUIDs = make(map[uint64]modernworld.GUID128)
				session.visibleObjectGUIDs = make(map[uint64]struct{})
				session.objectTypes = make(map[uint64]uint8)
				session.objectFields = make(map[uint64]map[int]uint32)
				session.objectPositions = make(map[uint64][3]float32)
				session.transportSync.Reset()
				session.goQueryGUIDs = make(map[uint32]modernworld.GUID128)
				session.queriedCreatures = make(map[uint32]struct{})
				session.creatureDisplay = make(map[uint32]uint32)
				session.queriedMirrorImages = make(map[uint64]struct{})
				session.corpseQueryPlayer = modernworld.GUID128{}
				session.pendingCorpseLocation = nil
				session.playerAuras = nil
				session.playerAuraBySlot = nil
				session.pendingCasts = nil
				session.resetSpellQueueLocked()
				session.lastStableMaster = 0
				session.completedCastIDs = nil
				session.unmatchedSpellGoSeq = 0
				session.unmatchedSpellStarts = nil
				session.unmatchedSpellGoes = nil
				session.initWorldStates = nil
				session.completedQuestBlocks = make([]uint64, modernworld.QuestCompletedBlockCount)
				session.completedQuestSyncPending = true
				session.completedQuestSyncRequested = false
				session.waitingForNewWorld = false
				session.waitingForWorldPortAck = false
			}
			session.worldMu.Unlock()
			if !knownCharacter {
				_ = world.WritePacket(modernworld.SMSGCharacterLoginFailed, []byte{5})
				s.log.Warn("player login requested for an unknown character", "account", session.legacy.Username, "guid", login.GUID)
				continue
			}
			connectKey, err := s.registerInstanceSession(session)
			if err != nil {
				s.log.Warn("allocate instance connection key failed", "account", session.legacy.Username, "error", err)
				_ = world.WritePacket(modernworld.SMSGCharacterLoginFailed, []byte{1})
				continue
			}
			host, port, err := s.worldEndpoint()
			if err != nil || port < 1 || port > 65535 {
				s.log.Warn("resolve instance endpoint failed", "account", session.legacy.Username, "error", err, "port", port)
				_ = world.WritePacket(modernworld.SMSGCharacterLoginFailed, []byte{3})
				continue
			}
			connectBody, err := modernworld.EncodeConnectTo(host, uint16(port), modernworld.ConnectToWorldAttempt1, connectKey)
			if err != nil {
				s.log.Warn("build connect-to-instance packet failed", "account", session.legacy.Username, "error", err)
				_ = world.WritePacket(modernworld.SMSGCharacterLoginFailed, []byte{3})
				continue
			}
			if err := world.WritePacket(modernworld.SMSGConnectTo, connectBody); err != nil {
				return
			}
			legacyBody := binary.LittleEndian.AppendUint64(nil, login.GUID)
			if err := legacyConn.WritePacket(legacyworld.CMSGPlayerLogin, legacyBody); err != nil {
				s.log.Warn("forward player login to legacy world failed", "account", session.legacy.Username, "error", err)
				return
			}
			s.log.Info("instance connection requested", "account", session.legacy.Username, "guid", login.GUID, "address", host, "port", port)
		case modernworld.CMSGHotfixRequest:
			ids, err := modernworld.ParseHotfixRequest(packet.Body)
			if err != nil {
				s.log.Warn("modern hotfix request malformed", "account", session.legacy.Username, "error", err)
				continue
			}
			response, matched, err := modernworld.EncodeHotfixConnect(ids)
			if err != nil {
				s.log.Warn("build modern hotfix response failed", "account", session.legacy.Username, "error", err)
				return
			}
			s.log.Debug("modern customization hotfix request", "account", session.legacy.Username, "requested", len(ids), "matched", matched)
			if err := world.WritePacket(modernworld.SMSGHotfixConnect, response); err != nil {
				return
			}
		case modernworld.CMSGDBQueryBulk:
			query, err := modernworld.ParseDBQueryBulk(packet.Body)
			if err != nil {
				s.log.Warn("modern DB query bulk malformed", "account", session.legacy.Username, "error", err)
				continue
			}
			matched := 0
			for _, recordID := range query.RecordIDs {
				response := modernworld.EncodeDBReply(query.TableHash, recordID, time.Now().Unix())
				if query.TableHash == modernworld.BroadcastTextTableHash {
					session.worldMu.Lock()
					record, ok := session.broadcastTexts[recordID]
					session.worldMu.Unlock()
					if ok {
						response = modernworld.EncodeBroadcastTextDBReply(record, time.Now().Unix())
						matched++
					}
				}
				if err := world.WritePacket(modernworld.SMSGDBReply, response); err != nil {
					return
				}
			}
			s.log.Debug("modern DB query bulk", "account", session.legacy.Username, "table", fmt.Sprintf("0x%08x", query.TableHash), "requested", len(query.RecordIDs), "matched", matched)
		case modernworld.CMSGRequestAccountData, modernworld.CMSGUpdateAccountData:
			_, err := s.handleAccountDataPacket(session, packet)
			if err != nil {
				s.log.Warn("modern account-data request failed", "account", session.legacy.Username, "opcode", packet.Opcode, "error", err)
			}
		case modernworld.CMSGTimeSyncResponse, modernworld.CMSGTutorialFlag:
			_, err := s.handleSimpleModernSystemPacket(session, packet)
			if err != nil {
				s.log.Warn("modern system request failed", "account", session.legacy.Username, "opcode", packet.Opcode, "error", err)
			}
		case modernworld.CMSGWorldPortResponse, modernworld.CMSGMoveTeleportAck, modernworld.CMSGLoadingScreenNotify, modernworld.CMSGSuspendTokenResponse:
			_, err := s.handleModernTeleportPacket(session, packet)
			if err != nil {
				s.log.Warn("modern teleport request failed", "account", session.legacy.Username, "opcode", packet.Opcode, "error", err)
			}
		default:
			handled, err := s.handleModernPlayPacket(session, world, packet)
			if err != nil {
				s.log.Warn("modern play request failed", "account", session.legacy.Username, "opcode", packet.Opcode, "error", err)
				continue
			}
			if handled {
				continue
			}
			if packet.Opcode == modernworld.CMSGLogDisconnect {
				reason := uint32(0)
				if len(packet.Body) >= 4 {
					reason = binary.LittleEndian.Uint32(packet.Body)
				}
				s.log.Warn("client logged disconnect", "account", session.legacy.Username, "source", "realm", "reason", reason)
				s.saveDisconnectPackets(world, "realm")
				continue
			}
			name := opcodes.Modern54261[packet.Opcode]
			if name == "" {
				name = "UNKNOWN"
			}
			s.log.Debug("modern world opcode pending translation", "account", session.legacy.Username, "opcode", packet.Opcode, "name", name, "bytes", len(packet.Body))
		}
	}
}

func (s *Server) handleInstanceWorld(world *modernworld.PacketConn, challenge modernworld.AuthChallenge, packet modernworld.Packet, remote net.Addr) {
	auth, err := modernworld.ParseAuthContinuedSession(packet.Body)
	if err != nil {
		s.log.Warn("modern continued world session malformed", "remote", remote, "error", err)
		return
	}
	if (auth.Key>>32)&1 != 1 {
		s.log.Warn("continued world session is not an instance connection", "remote", remote, "key", auth.Key)
		return
	}
	session := s.lookupInstanceSession(auth.Key)
	if session == nil {
		s.log.Warn("continued world session key rejected", "remote", remote, "key", auth.Key)
		return
	}
	if !modernworld.VerifyContinuedDigest(session.modernSessionKey, challenge.ServerChallenge, auth) {
		s.log.Warn("continued world session digest rejected", "remote", remote, "account", session.legacy.Username)
		return
	}
	if !s.claimInstanceSession(auth.Key, session) {
		s.log.Warn("continued world session key was already claimed", "remote", remote, "account", session.legacy.Username)
		return
	}
	_, encryptionKey := modernworld.DeriveContinuedKeys(session.modernSessionKey, auth.Key, challenge.ServerChallenge, auth.LocalChallenge)
	enterEncrypted, err := modernworld.EncodeEnterEncryptedMode(encryptionKey)
	if err != nil {
		s.log.Warn("build instance enter-encrypted-mode failed", "account", session.legacy.Username, "error", err)
		return
	}
	if err := world.WritePacket(modernworld.SMSGEnterEncryptedMode, enterEncrypted); err != nil {
		return
	}
	ack, err := world.ReadPacket()
	if err != nil || ack.Opcode != modernworld.CMSGEnterEncryptedModeAck {
		s.log.Warn("instance enter-encrypted-mode acknowledgement missing", "account", session.legacy.Username, "opcode", ack.Opcode, "error", err)
		return
	}
	if err := world.EnableEncryption(encryptionKey[:]); err != nil {
		s.log.Warn("enable instance world encryption failed", "account", session.legacy.Username, "error", err)
		return
	}
	if err := world.WritePacket(modernworld.SMSGResumeComms, nil); err != nil {
		return
	}
	if err := session.activateInstance(world); err != nil {
		session.clearInstance(world)
		s.log.Warn("flush queued instance packets failed", "account", session.legacy.Username, "error", err)
		return
	}
	defer session.clearInstance(world)
	s.log.Info("modern instance world authentication succeeded", "account", session.legacy.Username, "remote", remote)

	for {
		packet, err := world.ReadPacket()
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				s.log.Debug("modern instance world connection ended", "account", session.legacy.Username, "error", err)
			}
			return
		}
		if handled, ackErr := s.handleModernSpeedAck(session, packet); handled {
			if ackErr != nil {
				s.log.Warn("modern instance speed-ack failed", "account", session.legacy.Username, "opcode", packet.Opcode, "error", ackErr)
			}
			continue
		}
		if handled, ackErr := s.handleModernMovementFlagAck(session, packet); handled {
			if ackErr != nil {
				s.log.Warn("modern instance movement-flag ack failed", "account", session.legacy.Username, "opcode", packet.Opcode, "error", ackErr)
			}
			continue
		}
		if handled, ackErr := s.handleModernCollisionHeightAck(session, packet); handled {
			if ackErr != nil {
				s.log.Warn("modern instance collision-height ack failed", "account", session.legacy.Username, "error", ackErr)
			}
			continue
		}
		if legacyOpcode, movementPacket := modernworld.LegacyOpcodeForModernMovement(packet.Opcode); movementPacket {
			movement, parseErr := modernworld.ParseModernPlayerMovement(packet.Body)
			if parseErr != nil {
				s.log.Warn("modern instance player movement malformed", "account", session.legacy.Username, "opcode", packet.Opcode, "error", parseErr)
				continue
			}
			session.worldMu.Lock()
			moverGUID, moverKnown := session.legacyGUIDForModernLocked(movement.Mover)
			transportGUID, transportKnown := session.legacyGUIDForModernLocked(movement.Transport)
			if moverKnown {
				if session.objectPositions == nil {
					session.objectPositions = make(map[uint64][3]float32)
				}
				session.objectPositions[moverGUID] = [3]float32{movement.Move.X, movement.Move.Y, movement.Move.Z}
			}
			legacyConn := session.legacyWorld
			session.worldMu.Unlock()
			if !moverKnown || (!transportKnown && (movement.Transport.Low != 0 || movement.Transport.High != 0)) {
				s.log.Warn("modern instance player movement references an unknown object", "account", session.legacy.Username, "opcode", packet.Opcode,
					"mover_low", movement.Mover.Low, "transport_low", movement.Transport.Low)
				continue
			}
			legacyBody, encodeErr := modernworld.EncodeLegacyPlayerMovement(movement, moverGUID, transportGUID)
			if encodeErr != nil {
				s.log.Warn("encode legacy instance player movement failed", "account", session.legacy.Username, "opcode", packet.Opcode, "error", encodeErr)
				continue
			}
			if legacyConn == nil {
				continue
			}
			if err := legacyConn.WritePacket(legacyOpcode, legacyBody); err != nil {
				return
			}
			if packet.Opcode == modernworld.CMSGMoveDismissVehicle {
				s.log.Info("forward controlled vehicle dismiss", "modern_opcode", packet.Opcode, "legacy_opcode", legacyOpcode, "mover", fmt.Sprintf("0x%x", moverGUID), "bytes", len(legacyBody))
			}
			continue
		}
		switch packet.Opcode {
		case modernworld.CMsgPing:
			if len(packet.Body) >= 4 {
				serial := binary.LittleEndian.Uint32(packet.Body[:4])
				_ = world.WritePacket(modernworld.SMsgPong, binary.LittleEndian.AppendUint32(nil, serial))
			}
		case modernworld.CMSGRequestAccountData, modernworld.CMSGUpdateAccountData:
			_, err := s.handleAccountDataPacket(session, packet)
			if err != nil {
				s.log.Warn("modern instance account-data request failed", "account", session.legacy.Username, "opcode", packet.Opcode, "error", err)
			}
		case modernworld.CMSGTimeSyncResponse, modernworld.CMSGTutorialFlag:
			_, err := s.handleSimpleModernSystemPacket(session, packet)
			if err != nil {
				s.log.Warn("modern instance system request failed", "account", session.legacy.Username, "opcode", packet.Opcode, "error", err)
			}
		case modernworld.CMSGMoveInitActiveComplete:
			if _, err := modernworld.ParseInitActiveMoverComplete(packet.Body); err != nil {
				s.log.Warn("modern init-active-mover-complete malformed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			guid := session.currentCharacter
			legacyConn := session.legacyWorld
			session.worldMu.Unlock()
			if legacyConn == nil || guid == 0 {
				s.log.Warn("init-active-mover-complete has no legacy player", "account", session.legacy.Username)
				continue
			}
			if err := legacyConn.WritePacket(legacyworld.CMSGSetActiveMover, binary.LittleEndian.AppendUint64(nil, guid)); err != nil {
				return
			}
		case modernworld.CMSGSetActiveMover:
			modernGUID, err := modernworld.ParseSetActiveMover(packet.Body)
			if err != nil {
				s.log.Warn("modern set-active-mover malformed", "account", session.legacy.Username, "error", err)
				continue
			}
			// An empty modern mover is a control-transition notification. WotLK
			// validates this packet against its current mover and cannot accept zero.
			if modernGUID == (modernworld.GUID128{}) {
				continue
			}
			session.worldMu.Lock()
			legacyGUID, known := session.legacyGUIDForModernLocked(modernGUID)
			legacyConn := session.legacyWorld
			session.worldMu.Unlock()
			if !known || legacyConn == nil {
				s.log.Warn("modern set-active-mover references an unknown object", "account", session.legacy.Username, "guid_low", modernGUID.Low)
				continue
			}
			if err := legacyConn.WritePacket(legacyworld.CMSGSetActiveMover, binary.LittleEndian.AppendUint64(nil, legacyGUID)); err != nil {
				return
			}
		case modernworld.CMSGRequestPlayedTime:
			trigger, err := modernworld.ParseRequestPlayedTime(packet.Body)
			if err != nil {
				s.log.Warn("modern request-played-time malformed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			legacyConn := session.legacyWorld
			session.worldMu.Unlock()
			if legacyConn == nil {
				continue
			}
			legacyBody := []byte{0}
			if trigger {
				legacyBody[0] = 1
			}
			if err := legacyConn.WritePacket(legacyworld.CMSGRequestPlayedTime, legacyBody); err != nil {
				return
			}
		case modernworld.CMSGWorldPortResponse, modernworld.CMSGMoveTeleportAck, modernworld.CMSGLoadingScreenNotify, modernworld.CMSGSuspendTokenResponse:
			_, err := s.handleModernTeleportPacket(session, packet)
			if err != nil {
				s.log.Warn("modern instance teleport request failed", "account", session.legacy.Username, "opcode", packet.Opcode, "error", err)
			}
		default:
			handled, err := s.handleModernPlayPacket(session, world, packet)
			if err != nil {
				s.log.Warn("modern instance play request failed", "account", session.legacy.Username, "opcode", packet.Opcode, "error", err)
				continue
			}
			if handled {
				continue
			}
			if packet.Opcode == modernworld.CMSGLogDisconnect {
				reason := uint32(0)
				if len(packet.Body) >= 4 {
					reason = binary.LittleEndian.Uint32(packet.Body)
				}
				s.log.Warn("client logged disconnect", "account", session.legacy.Username, "source", "instance", "reason", reason)
				s.saveDisconnectPackets(world, "instance")
				continue
			}
			name := opcodes.Modern54261[packet.Opcode]
			if name == "" {
				name = "UNKNOWN"
			}
			s.log.Debug("modern instance opcode pending translation", "account", session.legacy.Username, "opcode", packet.Opcode, "name", name, "bytes", len(packet.Body))
		}
	}
}

func (session *proxySession) modernGUIDForLegacyLocked(legacyGUID uint64) modernworld.GUID128 {
	if guid, ok := session.objectGUIDs[legacyGUID]; ok {
		return guid
	}
	return modernworld.ModernGUIDForLegacy(legacyGUID, session.currentMapID)
}

func (session *proxySession) battlegroundContextLocked() modernworld.BattlegroundContext {
	if session.battlegroundQueues == nil {
		session.battlegroundQueues = make(map[uint32]modernworld.BattlegroundQueue)
	}
	return modernworld.BattlegroundContext{
		Requester: session.modernGUIDForLegacyLocked(session.currentCharacter),
		Now:       time.Now().Unix(),
		Queues:    session.battlegroundQueues,
		GUID:      session.modernGUIDForLegacyLocked,
		Player: func(guid uint64) modernworld.BattlegroundPlayer {
			character, known := session.knownCharacterInfo[guid]
			return modernworld.BattlegroundPlayerFromFields(session.objectFields[guid], character, known)
		},
	}
}

func (session *proxySession) lfgContextLocked() modernworld.LFGContext {
	return modernworld.LFGContext{
		Ticket:         modernworld.NewLFGTicket(session.modernGUIDForLegacyLocked(session.currentCharacter), time.Now().Unix()),
		RequestedRoles: session.lfgRoles,
		GUID:           session.modernGUIDForLegacyLocked,
	}
}

// legacyItemForModernLocked recovers the legacy item GUID64 behind a modern item
// GUID128. World objects are looked up in objectGUIDs; anything else is assumed
// to be an inventory item, whose modern identity is the deterministic inverse of
// modernLegacyGUID (Low = item counter) so bag items resolve without a create.
func (session *proxySession) legacyItemForModernLocked(item modernworld.GUID128) uint64 {
	if legacy, known := session.legacyGUIDForModernLocked(item); known {
		return legacy
	}
	return modernworld.LegacyItemGUIDFromModern(item)
}

func (session *proxySession) rememberLootRollLocked(key modernworld.LootRollKey, legacyGUID uint64) lootRollReference {
	reference := lootRollReference{
		legacyGUID: legacyGUID,
		modernGUID: modernworld.ModernLootGUID(legacyGUID, session.currentMapID),
		itemKey:    key,
	}
	if session.lootRolls == nil {
		session.lootRolls = make(map[lootRollIdentity]lootRollReference)
	}
	session.lootRolls[lootRollIdentity{reference.modernGUID, key.Slot}] = reference
	return reference
}

func (session *proxySession) resolveLootRollLocked(key modernworld.LootRollKey, legacyGUID uint64) (lootRollReference, bool) {
	if legacyGUID != 0 {
		return session.rememberLootRollLocked(key, legacyGUID), true
	}
	var result lootRollReference
	found := false
	for _, reference := range session.lootRolls {
		if reference.itemKey == key {
			// Legacy notifications can omit the roll GUID. Never guess when
			// simultaneous identical drops make the notification ambiguous.
			if found {
				return lootRollReference{}, false
			}
			result, found = reference, true
		}
	}
	return result, found
}

func (session *proxySession) rememberCompletedCastLocked(spellID uint32, castID modernworld.GUID128) {
	if spellID == 0 || (castID.Low == 0 && castID.High == 0) {
		return
	}
	if session.completedCastIDs == nil {
		session.completedCastIDs = make(map[uint32]modernworld.GUID128)
	}
	session.completedCastIDs[spellID] = castID
}

func (session *proxySession) spellFailureCastLocked(spellID uint32, caster modernworld.GUID128) (modernworld.GUID128, uint32) {
	// Pending/completed CastIDs are the local player's. Instant interrupts
	// (Mind Freeze 47528) send SPELL_FAILED_OTHER for the target between the
	// kick's START and GO; findPendingCastLocked's rank fallback would then
	// stamp the kick CastID onto the creature and leave its 3.4.3 cast bar up.
	// Hermes only reuses a pending when caster == player/pet and SpellId matches.
	player := session.modernGUIDForLegacyLocked(session.currentCharacter)
	if session.currentCharacter != 0 && caster == player {
		if pending, found := session.matchPlayerCastFailureLocked(spellID, false); found {
			return pending.ServerCastID, pending.VisualID
		}
		if id, ok := session.completedCastIDs[spellID]; ok {
			return id, session.spellVisuals[spellID]
		}
	}
	return modernworld.ModernCastGUID(session.currentMapID, spellID, uint64(spellID)+caster.Low), session.exactVisualIDForSpellLocked(spellID)
}

func (session *proxySession) playerCombatLogCastIDLocked(spellID uint32, fallback modernworld.GUID128) modernworld.GUID128 {
	if id, ok := session.completedCastIDs[spellID]; ok {
		return id
	}
	if pending, found := session.matchPendingCastLocked(spellID, false, false); found {
		return pending.ServerCastID
	}
	return fallback
}

func (session *proxySession) findPendingCastLocked(spellID uint32, start bool) (int, bool) {
	return session.findPendingCastForLocked(spellID, start, playerPending)
}

func (session *proxySession) matchPendingCastLocked(spellID uint32, start, consume bool) (modernworld.PendingCast, bool) {
	index, ok := session.findPendingCastLocked(spellID, start)
	if !ok {
		return modernworld.PendingCast{}, false
	}
	return session.usePendingCastLocked(index, spellID, start, consume), true
}

func (session *proxySession) rememberSpellVisualLocked(spellID, visualID uint32) {
	if spellID == 0 || visualID == 0 {
		return
	}
	if session.spellVisuals == nil {
		session.spellVisuals = make(map[uint32]uint32)
	}
	session.spellVisuals[spellID] = visualID
}

func (session *proxySession) exactVisualIDForSpellLocked(spellID uint32) uint32 {
	for _, pending := range session.pendingCasts {
		if pending.VisualID != 0 && pending.SpellID == spellID {
			return pending.VisualID
		}
	}
	if visual := session.spellVisuals[spellID]; visual != 0 {
		return visual
	}
	if visual := modernworld.KnownSpellVisual(spellID); visual != 0 {
		return visual
	}
	return 0
}

func (session *proxySession) visualIDForSpellLocked(spellID uint32) uint32 {
	return session.exactVisualIDForSpellLocked(spellID)
}

func (session *proxySession) noteAuraSlotsLocked(auras []modernworld.AuraInfo, updateAll bool) []uint32 {
	if session.playerAuraBySlot == nil {
		session.playerAuraBySlot = make(map[uint8]uint32)
	}
	var removed []uint32
	if updateAll {
		next := make(map[uint8]uint32, len(auras))
		for _, aura := range auras {
			if aura.HasData && aura.SpellID != 0 {
				next[aura.Slot] = aura.SpellID
			}
		}
		for slot, spellID := range session.playerAuraBySlot {
			if _, keep := next[slot]; !keep && modernworld.IsStealthSpell(spellID) {
				removed = append(removed, spellID)
			}
		}
		session.playerAuraBySlot = next
		return removed
	}
	for _, aura := range auras {
		if aura.HasData && aura.SpellID != 0 {
			session.playerAuraBySlot[aura.Slot] = aura.SpellID
			continue
		}
		spellID := session.playerAuraBySlot[aura.Slot]
		delete(session.playerAuraBySlot, aura.Slot)
		if modernworld.IsStealthSpell(spellID) {
			removed = append(removed, spellID)
		}
	}
	return removed
}

func (session *proxySession) resolveSpellCastLocked(cast *modernworld.SpellCastData, start bool) (caster, unit, castID modernworld.GUID128, hits, misses []modernworld.GUID128, pending modernworld.PendingCast, found bool) {
	legacyTarget := cast.Target.Unit.Low
	caster = session.modernGUIDForLegacyLocked(cast.CasterGUID)
	unit = session.modernGUIDForLegacyLocked(cast.CasterUnit)
	if cast.Target.Unit.Low != 0 {
		cast.Target.Unit = session.modernGUIDForLegacyLocked(cast.Target.Unit.Low)
	}
	if cast.Target.Item.Low != 0 {
		cast.Target.Item = session.modernGUIDForLegacyLocked(cast.Target.Item.Low)
	}
	remapSpellTargetTransportLocked(session, cast.Target.Src)
	remapSpellTargetTransportLocked(session, cast.Target.Dst)
	hits = make([]modernworld.GUID128, len(cast.HitTargets))
	for index, guid := range cast.HitTargets {
		hits[index] = session.modernGUIDForLegacyLocked(guid)
	}
	misses = make([]modernworld.GUID128, len(cast.MissTargets))
	for index, miss := range cast.MissTargets {
		misses[index] = session.modernGUIDForLegacyLocked(miss.Target)
	}
	playerCast := session.currentCharacter != 0 && (cast.CasterUnit == session.currentCharacter || cast.CasterGUID == session.currentCharacter)
	// Requests are partitioned by caster and by the item actually used.
	// Unsolicited pet, creature and item effects cannot borrow player casts.
	itemCaster := caster.High>>58 == 3
	accepts := func(p modernworld.PendingCast) bool {
		if cast.PetLoadCooldown {
			return false
		}
		if playerCast {
			if p.PetGUID != 0 {
				return false
			}
			if itemCaster {
				return p.CastItemGUID == cast.CasterGUID
			}
			// TakeReagents may clear the cast item between START and GO.
			// Only an already-started item cast may complete as its owner.
			return p.CastItemGUID == 0 || (!start && p.Started)
		}
		return p.PetGUID != 0 && p.PetGUID == cast.CasterUnit && p.PetGUID == cast.CasterGUID
	}
	index, matches := session.findPendingCastForLocked(cast.SpellID, start, accepts)
	if !matches && start && playerCast && !itemCaster && cast.CastFlags&legacyCastFlagPending == 0 {
		index, matches = session.findDownrankCastLocked(cast.SpellID, &legacyTarget)
	}
	if matches {
		pending, found = session.usePendingCastLocked(index, cast.SpellID, start, !start), true
	}

	if found {
		castID = pending.ServerCastID
		cast.VisualID = pending.VisualID
		return
	}
	if playerCast {
		cast.VisualID = session.visualIDForSpellLocked(cast.SpellID)
	} else {
		cast.VisualID = session.exactVisualIDForSpellLocked(cast.SpellID)
	}
	// legacy proxy still gives an unmatched server-originated SPELL_START/GO a normal
	// Cast GUID. Its counter is the caster-unit counter plus the translated spell
	// ID; ModernCastGUID supplies legacy proxy's SpellCastSource.Normal (3) and masks the
	// counter to the 40 bits carried by MapSpecificCreate.
	castID = modernworld.ModernCastGUID(session.currentMapID, cast.SpellID, unit.Low+uint64(cast.SpellID))
	if cast.PetLoadCooldown {
		return
	}
	if playerCast && !start {
		if _, ok := session.completedCastIDs[cast.SpellID]; ok {
			session.unmatchedSpellGoSeq++
			castID = modernworld.ModernCastGUID(session.currentMapID, cast.SpellID, unit.Low+uint64(cast.SpellID)+session.unmatchedSpellGoSeq)
		}
		session.rememberCompletedCastLocked(cast.SpellID, castID)
		return
	}
	if playerCast {
		return
	}
	castID = session.unmatchedCreatureCastIDLocked(*cast, unit, start, castID)
	if !start && modernworld.NeedsFallingMissileTrajectory(cast.SpellID) {
		session.applyUnmatchedMissileTrajectoryLocked(cast, legacyTarget)
	}
	return
}

func unmatchedSpellCastKey(cast modernworld.SpellCastData) unmatchedSpellKey {
	caster := cast.CasterUnit
	if caster == 0 {
		caster = cast.CasterGUID
	}
	return unmatchedSpellKey{caster: caster, spell: cast.SpellID}
}

func (session *proxySession) unmatchedCreatureCastIDLocked(cast modernworld.SpellCastData, unit modernworld.GUID128, start bool, base modernworld.GUID128) modernworld.GUID128 {
	key := unmatchedSpellCastKey(cast)
	if start {
		if session.unmatchedSpellStarts == nil {
			session.unmatchedSpellStarts = make(map[unmatchedSpellKey]modernworld.GUID128)
		}
		session.unmatchedSpellStarts[key] = base
		return base
	}
	if id, ok := session.unmatchedSpellStarts[key]; ok {
		delete(session.unmatchedSpellStarts, key)
		return id
	}
	if session.unmatchedSpellGoes == nil {
		session.unmatchedSpellGoes = make(map[unmatchedSpellKey]struct{})
	}
	if _, used := session.unmatchedSpellGoes[key]; used {
		session.unmatchedSpellGoSeq++
		base = modernworld.ModernCastGUID(session.currentMapID, cast.SpellID, unit.Low+uint64(cast.SpellID)+session.unmatchedSpellGoSeq)
	}
	session.unmatchedSpellGoes[key] = struct{}{}
	return base
}

func (session *proxySession) applyUnmatchedMissileTrajectoryLocked(cast *modernworld.SpellCastData, legacyTarget uint64) {
	var casterPos, impactPos *[3]float32
	if pos, ok := session.objectPositions[cast.CasterGUID]; ok {
		value := pos
		casterPos = &value
	} else if pos, ok := session.objectPositions[cast.CasterUnit]; ok {
		value := pos
		casterPos = &value
	}
	if legacyTarget != 0 && legacyTarget != cast.CasterGUID && legacyTarget != cast.CasterUnit {
		if pos, ok := session.objectPositions[legacyTarget]; ok {
			value := pos
			impactPos = &value
		}
	}
	// Rotface 69832 and Sindragosa 69846 reach this helper. Always fall onto
	// the dest rather than skating from the caster: Rotface is on the floor,
	// and the last frost bomb is in flight while she starts to land.
	modernworld.ApplyTriggeredMissileTrajectory(cast, casterPos, impactPos, true)
}

func (s *Server) forwardLegacySpell(session *proxySession, cast modernworld.SpellCastData, start bool) error {
	session.worldMu.Lock()
	caster, unit, castID, hits, misses, pending, found := session.resolveSpellCastLocked(&cast, start)
	if object := modernworld.SpellChannelObject(cast); object != 0 {
		if session.channelObjects == nil {
			session.channelObjects = make(map[uint64]uint64)
		}
		session.channelObjects[cast.CasterGUID] = object
		if cast.CasterUnit != 0 && cast.CasterUnit != cast.CasterGUID {
			session.channelObjects[cast.CasterUnit] = object
		}
	}
	var heldBody []byte
	if found {
		if start {
			session.markSpellQueueStartedLocked(pending)
		} else {
			heldBody = session.completeSpellQueueCastLocked(pending)
		}
	}
	legacyConn := session.legacyWorld
	session.worldMu.Unlock()
	if start {
		if found {
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSpellPrepare, Body: modernworld.EncodeSpellPrepare(pending.ClientCastID, pending.ServerCastID)}); err != nil {
				return err
			}
		}
	} else if found && !pending.Started {
		if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSpellPrepare, Body: modernworld.EncodeSpellPrepare(pending.ClientCastID, pending.ServerCastID)}); err != nil {
			return err
		}
	}
	opcode := modernworld.SMSGSpellGo
	if start {
		opcode = modernworld.SMSGSpellStart
	}
	var body []byte
	if start {
		body = modernworld.EncodeSpellStart(cast, caster, unit, castID, hits, misses)
	} else {
		body = modernworld.EncodeSpellGo(cast, caster, unit, castID, hits, misses)
	}
	if err := session.sendInstance(modernworld.Packet{Opcode: opcode, Body: body}); err != nil {
		return err
	}
	s.log.Debug("forward spell",
		"account", session.legacy.Username,
		"kind", map[bool]string{true: "start", false: "go"}[start],
		"spell", cast.SpellID,
		"caster", fmt.Sprintf("0x%x", cast.CasterGUID),
		"caster_unit", fmt.Sprintf("0x%x", cast.CasterUnit),
		"visual", cast.VisualID,
		"travel_time", cast.TravelTime,
		"matched", found,
		"pet_load_cooldown", cast.PetLoadCooldown,
		"request_spell", pending.SpellID,
		"client_cast_id", fmt.Sprintf("%016x:%016x", pending.ClientCastID.High, pending.ClientCastID.Low),
		"server_cast_id", fmt.Sprintf("%016x:%016x", castID.High, castID.Low),
		"released_held", len(heldBody) > 0)
	if !start && len(heldBody) > 0 && legacyConn != nil {
		return legacyConn.WritePacket(legacyworld.CMSGCastSpell, heldBody)
	}
	return nil
}

func (s *Server) flushReadyObjectSpells(session *proxySession) error {
	session.worldMu.Lock()
	ready := session.takeReadyObjectSpellsLocked()
	session.worldMu.Unlock()
	for _, pending := range ready {
		if err := s.forwardLegacySpell(session, pending.cast, pending.start); err != nil {
			return err
		}
		s.log.Debug("forward queued spell after object create",
			"account", session.legacy.Username,
			"spell", pending.cast.SpellID,
			"caster", fmt.Sprintf("0x%x", pending.cast.CasterGUID),
			"kind", map[bool]string{true: "start", false: "go"}[pending.start])
	}
	return nil
}

func (s *Server) handleLegacyArena(session *proxySession, packet legacyworld.Packet) error {
	switch packet.Opcode {
	case legacyworld.SMSGArenaTeamQueryResponse:
		emblem, err := modernworld.ParseLegacyArenaTeamQuery(packet.Body)
		if err != nil {
			return err
		}
		body, err := modernworld.EncodeArenaTeamQueryResponse(emblem)
		if err != nil {
			return err
		}
		session.worldMu.Lock()
		session.storeArenaEmblemLocked(emblem)
		session.worldMu.Unlock()
		return session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGQueryArenaTeamResponse, Body: body})
	case legacyworld.SMSGArenaTeamStats:
		teamID, stats, err := modernworld.ParseLegacyArenaTeamStats(packet.Body)
		if err != nil {
			return err
		}
		session.worldMu.Lock()
		session.storeArenaStatsLocked(teamID, stats)
		session.worldMu.Unlock()
		return nil
	case legacyworld.SMSGArenaTeamRoster:
		roster, err := modernworld.ParseLegacyArenaTeamRoster(packet.Body)
		if err != nil {
			return err
		}
		session.worldMu.Lock()
		for _, member := range roster.Members {
			session.rememberPlayerNameLocked(member.GUID, member.Name)
		}
		stats := session.arenaStatsLocked(roster.TeamID)
		body, err := modernworld.EncodeArenaTeamRoster(roster, stats, session.modernGUIDForLegacyLocked)
		session.worldMu.Unlock()
		if err != nil {
			return err
		}
		return session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGArenaTeamRoster, Body: body})
	case legacyworld.SMSGArenaTeamCommandResult:
		command, err := modernworld.ParseLegacyArenaTeamCommand(packet.Body)
		if err != nil {
			return err
		}
		body, err := modernworld.EncodeArenaTeamCommand(command)
		if err != nil {
			return err
		}
		return session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGArenaTeamCommandResult, Body: body})
	case legacyworld.SMSGArenaTeamEvent:
		notice, err := modernworld.ParseLegacyArenaTeamEvent(packet.Body)
		if err != nil {
			return err
		}
		body, err := modernworld.EncodeArenaTeamEvent(notice)
		if err != nil {
			return err
		}
		return session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGArenaTeamEvent, Body: body})
	case legacyworld.SMSGArenaTeamInvite:
		names, err := modernworld.ParseLegacyArenaTeamInvite(packet.Body)
		if err != nil {
			return err
		}
		session.worldMu.Lock()
		player := session.playerGUIDByNameLocked(names.PlayerName)
		realmAddress := realm.Address(session.selectedRealm.ID)
		session.worldMu.Unlock()
		body, err := modernworld.EncodeArenaTeamInvite(player, modernworld.ModernArenaTeamGUID(1), realmAddress, names)
		if err != nil {
			return err
		}
		return session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGArenaTeamInvite, Body: body})
	default:
		return nil
	}
}

func (s *Server) handleArenaClient(session *proxySession, dest *modernworld.PacketConn, packet modernworld.Packet) (bool, error) {
	switch packet.Opcode {
	case modernworld.CMSGArenaTeamRoster:
		index, err := modernworld.ParseArenaTeamRoster(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		teamID := modernworld.ArenaTeamIDFromFields(session.objectFields[session.currentCharacter], index)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if teamID == 0 {
			if dest == nil {
				return true, nil
			}
			return true, dest.WritePacket(modernworld.SMSGArenaTeamRoster, modernworld.EncodeEmptyArenaTeamRoster(index))
		}
		if legacyConn == nil {
			return true, nil
		}
		body := modernworld.EncodeLegacyArenaTeamID(teamID)
		if err := legacyConn.WritePacket(legacyworld.CMSGArenaTeamQuery, body); err != nil {
			return true, err
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGArenaTeamRoster, body)
	case modernworld.CMSGArenaTeamAccept:
		if err := modernworld.ParseArenaTeamAccept(packet.Body); err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGArenaTeamAccept, nil)
	case modernworld.CMSGArenaTeamDisband:
		teamID, err := modernworld.ParseArenaTeamDisband(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGArenaTeamDisband, modernworld.EncodeLegacyArenaTeamID(teamID))
	case modernworld.CMSGArenaTeamRemove:
		teamID, player, err := modernworld.ParseArenaTeamRemove(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		name := session.playerNameByModernLocked(player)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if name == "" {
			return true, fmt.Errorf("arena-team remove player name is unknown")
		}
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGArenaTeamRemove, modernworld.EncodeLegacyArenaTeamRemove(teamID, name))
	case modernworld.CMSGBattlemasterJoinArena, modernworld.CMSGBattlemasterJoinSkirmish:
		var join modernworld.BattlemasterJoin
		var err error
		rated := byte(0)
		if packet.Opcode == modernworld.CMSGBattlemasterJoinArena {
			join, err = modernworld.ParseBattlemasterJoinArena(packet.Body)
			rated = 1
		} else {
			join, err = modernworld.ParseBattlemasterJoinSkirmish(packet.Body)
		}
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacyGUIDForModernLocked(join.GUID)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known || legacyGUID == 0 {
			return true, fmt.Errorf("arena battlemaster is unknown")
		}
		if legacyConn == nil {
			return true, nil
		}
		asGroup := byte(0)
		if packet.Opcode == modernworld.CMSGBattlemasterJoinArena || join.AsGroup {
			asGroup = 1
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGBattlemasterJoinArena, modernworld.EncodeLegacyBattlemasterJoin(legacyGUID, join.Slot, asGroup, rated))
	default:
		return false, nil
	}
}

func (s *Server) handleModernPlayPacket(session *proxySession, dest *modernworld.PacketConn, packet modernworld.Packet) (bool, error) {
	if packet.Opcode >= modernworld.CMSGOpeningCinematic && packet.Opcode <= modernworld.CMSGCompleteCinematic {
		if len(packet.Body) != 0 {
			return true, fmt.Errorf("cinematic acknowledgement has %d unexpected bytes", len(packet.Body))
		}
		legacyOpcode := uint32(0xf9)
		if packet.Opcode == modernworld.CMSGNextCinematicCamera {
			legacyOpcode = 0xfb
		}
		if packet.Opcode == modernworld.CMSGCompleteCinematic {
			legacyOpcode = 0xfc
		}
		session.worldMu.Lock()
		conn := session.legacyWorld
		session.worldMu.Unlock()
		if conn == nil {
			return true, nil
		}
		return true, conn.WritePacket(legacyOpcode, nil)
	}
	if packet.Opcode == modernworld.CMSGSaveCUFProfiles {
		return true, s.saveCUFProfiles(session, packet.Body)
	}
	if modernworld.IsPetitionClientOpcode(packet.Opcode) {
		return true, s.handlePetitionRequest(session, packet)
	}
	if modernworld.IsGuildClientOpcode(packet.Opcode) {
		return true, s.handleGuildRequest(session, packet)
	}
	if packet.Opcode == modernworld.CMSGEmote || packet.Opcode == modernworld.CMSGMountSpecialAnim {
		op, err := modernworld.ParseRuntimeMiscRequest(packet.Opcode, packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		conn := session.legacyWorld
		session.worldMu.Unlock()
		if op == 0 || conn == nil {
			return true, nil
		}
		return true, conn.WritePacket(op, nil)
	}
	switch packet.Opcode {
	case modernworld.CMSGArenaTeamRoster, modernworld.CMSGArenaTeamAccept, modernworld.CMSGArenaTeamDisband,
		modernworld.CMSGArenaTeamRemove, modernworld.CMSGBattlemasterJoinArena, modernworld.CMSGBattlemasterJoinSkirmish:
		return s.handleArenaClient(session, dest, packet)
	case modernworld.CMSGBattlePetRequestJournalLock:
		return true, modernworld.ParseBattlePetRequestJournalLock(packet.Body)
	case modernworld.CMSGChangeRealmTicket:
		request, err := modernworld.ParseChangeRealmTicket(packet.Body)
		if err != nil {
			return true, err
		}
		if dest == nil {
			return true, fmt.Errorf("change-realm-ticket has no modern connection")
		}
		session.worldMu.Lock()
		session.clientSecret = request.Secret
		session.hasClientSecret = true
		session.worldMu.Unlock()
		s.log.Debug("change realm ticket", "account", session.legacy.Username, "token", request.Token)
		return true, dest.WritePacket(modernworld.SMSGChangeRealmTicketResponse, modernworld.EncodeChangeRealmTicketResponse(request.Token))
	case modernworld.CMSGLogoutRequest:
		idle, err := modernworld.ParseLogoutRequest(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("logout request", "account", session.legacy.Username, "idle", idle)
		return true, legacyConn.WritePacket(legacyworld.CMSGLogoutRequest, nil)
	case modernworld.CMSGLogoutCancel:
		if err := modernworld.ParseLogoutCancel(packet.Body); err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("logout cancel", "account", session.legacy.Username)
		return true, legacyConn.WritePacket(legacyworld.CMSGLogoutCancel, nil)
	case modernworld.CMSGAreaTrigger:
		request, err := modernworld.ParseAreaTrigger(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("area trigger", "account", session.legacy.Username, "id", request.ID,
			"entered", request.Entered, "from_client", request.FromClient)
		// WotLK only reports entry; forwarding modern exits retriggers portals.
		if !request.Entered {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGAreaTrigger, modernworld.EncodeLegacyAreaTrigger(request))
	case modernworld.CMSGSetTitle:
		request, err := modernworld.ParseSetTitle(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("set title", "account", session.legacy.Username, "title", request.TitleID)
		return true, legacyConn.WritePacket(legacyworld.CMSGSetTitle, modernworld.EncodeLegacySetTitle(request))
	case modernworld.CMSGTogglePvP:
		if len(packet.Body) != 0 {
			return true, fmt.Errorf("toggle-pvp request has a %d-byte body, want empty", len(packet.Body))
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("toggle pvp", "account", session.legacy.Username)
		return true, legacyConn.WritePacket(legacyworld.CMSGTogglePvP, nil)
	case modernworld.CMSGSetPvP:
		request, err := modernworld.ParseSetPvP(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("set pvp", "account", session.legacy.Username, "enable", request.Enable)
		return true, legacyConn.WritePacket(legacyworld.CMSGTogglePvP, modernworld.EncodeLegacySetPvP(request.Enable))
	case modernworld.CMSGUnlearnSkill:
		request, err := modernworld.ParseUnlearnSkill(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("unlearn skill", "account", session.legacy.Username, "skill", request.SkillLine)
		return true, legacyConn.WritePacket(legacyworld.CMSGUnlearnSkill, modernworld.EncodeLegacyUnlearnSkill(request))
	case modernworld.CMSGRemoveGlyph:
		request, err := modernworld.ParseRemoveGlyph(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("remove glyph", "account", session.legacy.Username, "slot", request.GlyphSlot)
		return true, legacyConn.WritePacket(legacyworld.CMSGRemoveGlyph, modernworld.EncodeLegacyRemoveGlyph(request))
	case modernworld.CMSGServerTimeOffsetRequest:
		if err := modernworld.ParseServerTimeOffsetRequest(packet.Body); err != nil {
			return true, err
		}
		return true, dest.WritePacket(modernworld.SMSGServerTimeOffset, modernworld.EncodeServerTimeOffset(time.Now()))
	case modernworld.CMSGQueryTime:
		if err := modernworld.ParseQueryTime(packet.Body); err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGQueryTime, nil)
	case modernworld.CMSGMoveTimeSkipped:
		mover, skipped, err := modernworld.ParseMoveTimeSkipped(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacyGUIDForModernLocked(mover)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known {
			return true, fmt.Errorf("move-time-skipped references an unknown mover")
		}
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGMoveTimeSkipped, modernworld.EncodeLegacyMoveTimeSkipped(legacyGUID, skipped))
	case modernworld.CMSGMoveSplineDone:
		movement, splineID, err := modernworld.ParseMoveSplineDone(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		moverGUID, moverKnown := session.legacyGUIDForModernLocked(movement.Mover)
		transportGUID, transportKnown := session.legacyGUIDForModernLocked(movement.Transport)
		if moverKnown {
			if session.objectPositions == nil {
				session.objectPositions = make(map[uint64][3]float32)
			}
			session.objectPositions[moverGUID] = [3]float32{movement.Move.X, movement.Move.Y, movement.Move.Z}
		}
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !moverKnown || (!transportKnown && (movement.Transport.Low != 0 || movement.Transport.High != 0)) {
			return true, fmt.Errorf("move-spline-done references an unknown mover or transport")
		}
		body, err := modernworld.EncodeLegacyMoveSplineDone(movement, moverGUID, transportGUID, splineID)
		if err != nil {
			return true, err
		}
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGMoveSplineDone, body)
	case modernworld.CMSGQueryNextMailTime:
		if err := modernworld.ParseQueryNextMailTime(packet.Body); err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGQueryNextMailTime, nil)
	case modernworld.CMSGMailGetList:
		request, err := modernworld.ParseMailboxRequest(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		mailbox := session.legacyMailboxLocked(request.Mailbox)
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("mail get list", "account", session.legacy.Username, "mailbox", fmt.Sprintf("0x%x", mailbox))
		return true, legacyConn.WritePacket(legacyworld.CMSGMailGetList, modernworld.EncodeLegacyMailGetList(mailbox))
	case modernworld.CMSGMailMarkAsRead:
		request, err := modernworld.ParseMailIDRequest(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		mailbox := session.legacyMailboxLocked(request.Mailbox)
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGMailMarkAsRead, modernworld.EncodeLegacyMailIDCommand(mailbox, request.MailID))
	case modernworld.CMSGMailCreateTextItem:
		request, err := modernworld.ParseMailIDRequest(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		mailbox := session.legacyMailboxLocked(request.Mailbox)
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGMailCreateTextItem, modernworld.EncodeLegacyMailIDCommand(mailbox, request.MailID))
	case modernworld.CMSGMailTakeItem:
		request, err := modernworld.ParseMailTakeItemRequest(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		mailbox := session.legacyMailboxLocked(request.Mailbox)
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGMailTakeItem, modernworld.EncodeLegacyMailTakeItem(mailbox, request.MailID, request.Attach))
	case modernworld.CMSGMailTakeMoney:
		request, err := modernworld.ParseMailTakeMoneyRequest(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		mailbox := session.legacyMailboxLocked(request.Mailbox)
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGMailTakeMoney, modernworld.EncodeLegacyMailIDCommand(mailbox, request.MailID))
	case modernworld.CMSGMailDelete:
		request, err := modernworld.ParseMailDeleteRequest(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		mailbox := session.currentInteractedGO
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGMailDelete, modernworld.EncodeLegacyMailDelete(mailbox, request.MailID))
	case modernworld.CMSGMailReturnToSender:
		request, err := modernworld.ParseMailReturnRequest(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		mailbox := session.currentInteractedGO
		sender, _ := session.legacyGUIDForModernLocked(request.Sender)
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGMailReturnToSender, modernworld.EncodeLegacyMailReturn(mailbox, request.MailID, sender))
	case modernworld.CMSGSendMail:
		request, err := modernworld.ParseSendMail(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		mailbox := session.legacyMailboxLocked(request.Mailbox)
		body := modernworld.EncodeLegacySendMail(request, mailbox, func(item modernworld.GUID128) uint64 {
			legacy, _ := session.legacyGUIDForModernLocked(item)
			return legacy
		})
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("send mail", "account", session.legacy.Username, "target", request.Target, "attachments", len(request.Attachments))
		return true, legacyConn.WritePacket(legacyworld.CMSGSendMail, body)
	case modernworld.CMSGSetDungeonDifficulty, modernworld.CMSGSetRaidDifficulty, modernworld.CMSGInstanceLockResponse:
		var body []byte
		var err error
		var opcode uint32
		switch packet.Opcode {
		case modernworld.CMSGSetDungeonDifficulty:
			body, err = modernworld.TranslateSetDungeonDifficulty(packet.Body)
			opcode = legacyworld.CMSGSetDungeonDifficulty
		case modernworld.CMSGSetRaidDifficulty:
			body, err = modernworld.TranslateSetRaidDifficulty(packet.Body)
			opcode = legacyworld.CMSGSetRaidDifficulty
		case modernworld.CMSGInstanceLockResponse:
			body, err = modernworld.TranslateInstanceLockResponse(packet.Body)
			opcode = legacyworld.CMSGInstanceLockResponse
		}
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(opcode, body)
	case modernworld.CMSGRequestRaidInfo:
		if err := modernworld.ParseRequestRaidInfo(packet.Body); err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGRequestRaidInfo, nil)
	case modernworld.CMSGResetInstances:
		if err := modernworld.ParseResetInstances(packet.Body); err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGResetInstances, nil)
	case modernworld.CMSGSummonResponse:
		response, err := modernworld.ParseSummonResponse(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacySummoner, known := session.legacyGUIDForModernLocked(response.Summoner)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known {
			return true, fmt.Errorf("summon-response references an unknown summoner")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("summon response", "account", session.legacy.Username, "summoner", fmt.Sprintf("0x%x", legacySummoner), "accept", response.Accept)
		return true, legacyConn.WritePacket(legacyworld.CMSGSummonResponse, modernworld.EncodeLegacySummonResponse(legacySummoner, response.Accept))
	case modernworld.CMSGSendContactList:
		flags, err := modernworld.ParseSendContactList(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGContactList, modernworld.EncodeLegacySendContactList(flags))
	case modernworld.CMSGAddFriend:
		request, err := modernworld.ParseAddFriend(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("add friend", "account", session.legacy.Username, "name", request.Name, "note_bytes", len(request.Note))
		return true, legacyConn.WritePacket(legacyworld.CMSGAddFriend, modernworld.EncodeLegacyAddFriend(request))
	case modernworld.CMSGAddIgnore:
		request, err := modernworld.ParseAddIgnore(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("add ignore", "account", session.legacy.Username, "name", request.Name)
		return true, legacyConn.WritePacket(legacyworld.CMSGAddIgnore, modernworld.EncodeLegacyAddIgnore(request.Name))
	case modernworld.CMSGDelFriend, modernworld.CMSGDelIgnore:
		request, err := modernworld.ParseDelFriend(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacySocialTargetLocked(request.GUID)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known {
			return true, fmt.Errorf("del-contact references an unknown player")
		}
		if legacyConn == nil {
			return true, nil
		}
		legacyOpcode := uint32(legacyworld.CMSGDelFriend)
		action := "del friend"
		if packet.Opcode == modernworld.CMSGDelIgnore {
			legacyOpcode = legacyworld.CMSGDelIgnore
			action = "del ignore"
		}
		s.log.Debug(action, "account", session.legacy.Username, "guid", fmt.Sprintf("0x%x", legacyGUID))
		return true, legacyConn.WritePacket(legacyOpcode, modernworld.EncodeLegacyDelFriend(legacyGUID))
	case modernworld.CMSGSetContactNotes:
		request, err := modernworld.ParseSetContactNotes(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacySocialTargetLocked(request.GUID)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known {
			return true, fmt.Errorf("set-contact-notes references an unknown player")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("set contact notes", "account", session.legacy.Username, "guid", fmt.Sprintf("0x%x", legacyGUID), "note_bytes", len(request.Notes))
		return true, legacyConn.WritePacket(legacyworld.CMSGSetContactNotes, modernworld.EncodeLegacySetContactNotes(legacyGUID, request.Notes))
	case modernworld.CMSGQueryRealmName:
		address, err := modernworld.ParseQueryRealmName(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		name := session.selectedRealm.Name
		if name == "" {
			name = "AzerothCore"
		}
		if address == 0 {
			address = realm.Address(session.selectedRealm.ID)
		}
		session.worldMu.Unlock()
		body, err := modernworld.EncodeRealmQueryResponse(address, name)
		if err != nil {
			return true, err
		}
		if dest == nil {
			return true, nil
		}
		return true, dest.WritePacket(modernworld.SMSGRealmQuery, body)
	case modernworld.CMSGGetMirrorImageData:
		guid, err := modernworld.ParseGetMirrorImageData(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacyGUIDForModernLocked(guid)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if (guid.Low != 0 || guid.High != 0) && !known {
			return true, fmt.Errorf("mirror-image query references an unknown object")
		}
		s.log.Debug("get mirror-image data", "account", session.legacy.Username, "legacy", fmt.Sprintf("0x%x", legacyGUID))
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGGetMirrorImageData, modernworld.EncodeLegacyGetMirrorImageData(legacyGUID))
	case modernworld.CMSGQueryCreature:
		entry, err := modernworld.ParseQueryCreature(packet.Body)
		if err != nil {
			return true, err
		}
		s.log.Debug("query creature", "account", session.legacy.Username, "entry", entry, "bytes", len(packet.Body))
		session.worldMu.Lock()
		if session.queriedCreatures == nil {
			session.queriedCreatures = make(map[uint32]struct{})
		}
		session.queriedCreatures[entry] = struct{}{}
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGCreatureQuery, modernworld.EncodeLegacyCreatureQuery(entry))
	case modernworld.CMSGQueryGameObject:
		entry, guid, err := modernworld.ParseQueryGameObject(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		if session.goQueryGUIDs == nil {
			session.goQueryGUIDs = make(map[uint32]modernworld.GUID128)
		}
		session.goQueryGUIDs[entry] = guid
		legacyGUID, _ := session.legacyGUIDForModernLocked(guid)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGGameObjectQuery, modernworld.EncodeLegacyGameObjectQuery(entry, legacyGUID))
	case modernworld.CMSGQueryNpcText:
		textID, guid, err := modernworld.ParseQueryNpcText(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, _ := session.legacyGUIDForModernLocked(guid)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("query npc text", "account", session.legacy.Username, "text", textID)
		return true, legacyConn.WritePacket(legacyworld.CMSGNpcTextQuery, modernworld.EncodeLegacyNpcTextQuery(textID, legacyGUID))
	case modernworld.CMSGQueryQuestInfo:
		entry, err := modernworld.ParseQueryQuestInfo(packet.Body)
		if err != nil {
			return true, err
		}
		s.log.Debug("query quest info", "account", session.legacy.Username, "entry", entry, "bytes", len(packet.Body))
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGQuestQuery, modernworld.EncodeLegacyQuestQuery(entry))
	case modernworld.CMSGQueryQuestCompletionNPCs:
		quests, err := modernworld.ParseQueryQuestCompletionNPCs(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("query completed quests", "account", session.legacy.Username, "quests_in_request", len(quests))
		return true, legacyConn.WritePacket(legacyworld.CMSGQueryQuestsCompleted, modernworld.EncodeLegacyQueryQuestsCompleted(quests))
	case modernworld.CMSGQuestPOIQuery:
		quests, err := modernworld.ParseQuestPOIQuery(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("quest POI query", "account", session.legacy.Username, "quests", len(quests))
		return true, legacyConn.WritePacket(legacyworld.CMSGQuestPOIQuery, modernworld.EncodeLegacyQuestPOIQuery(quests))
	case modernworld.CMSGQuestGiverStatusQuery:
		guid, err := modernworld.ParseQuestGiverStatusQuery(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacyGUIDForModernLocked(guid)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if (guid.Low != 0 || guid.High != 0) && !known {
			return true, fmt.Errorf("quest-giver status query references an unknown object")
		}
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGQuestGiverStatusQuery, modernworld.EncodeLegacyUnpackedGUID(legacyGUID))
	case modernworld.CMSGQuestGiverStatusMultipleQuery:
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGQuestGiverStatusMultipleQuery, nil)
	case modernworld.CMSGQuestGiverHello:
		giver, err := modernworld.ParseQuestGiverHello(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacyGUIDForModernLocked(giver)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if (giver.Low != 0 || giver.High != 0) && !known {
			return true, fmt.Errorf("quest-giver hello references an unknown object")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("quest-giver hello", "account", session.legacy.Username, "legacy", fmt.Sprintf("0x%x", legacyGUID))
		return true, legacyConn.WritePacket(legacyworld.CMSGQuestGiverHello, modernworld.EncodeLegacyUnpackedGUID(legacyGUID))
	case modernworld.CMSGQuestGiverQueryQuest:
		request, err := modernworld.ParseQuestGiverQueryQuest(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacyGUIDForModernLocked(request.Giver)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if (request.Giver.Low != 0 || request.Giver.High != 0) && !known {
			return true, fmt.Errorf("query-quest references an unknown quest giver")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("query quest giver", "account", session.legacy.Username, "quest", request.QuestID, "respond", request.RespondToGiver)
		return true, legacyConn.WritePacket(legacyworld.CMSGQuestGiverQueryQuest,
			modernworld.EncodeLegacyQuestGiverQuery(legacyGUID, request.QuestID, request.RespondToGiver))
	case modernworld.CMSGQuestGiverAcceptQuest:
		request, err := modernworld.ParseQuestGiverAcceptQuest(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacyGUIDForModernLocked(request.Giver)
		giverType := session.objectTypes[legacyGUID]
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if (request.Giver.Low != 0 || request.Giver.High != 0) && !known {
			return true, fmt.Errorf("accept-quest references an unknown quest giver")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("accept quest", "account", session.legacy.Username, "quest", request.QuestID)
		if err := legacyConn.WritePacket(legacyworld.CMSGQuestGiverAcceptQuest,
			modernworld.EncodeLegacyQuestGiverAccept(legacyGUID, request.QuestID, request.StartCheat)); err != nil {
			return true, err
		}
		// The 3.3.5 client invalidates the quest-giver marker locally when it
		// accepts a quest. Build 54261 keeps the last QuestGiverStatus instead,
		// and does not issue a new status query after CMSG accept. Queue one
		// behind the accept request so the legacy server computes the new status
		// after adding the quest and the modern overhead marker is refreshed.
		if giverType == 3 || giverType == 5 {
			s.log.Debug("refresh quest-giver status after accept", "account", session.legacy.Username, "quest", request.QuestID, "legacy", fmt.Sprintf("0x%x", legacyGUID))
			return true, legacyConn.WritePacket(legacyworld.CMSGQuestGiverStatusQuery,
				modernworld.EncodeLegacyUnpackedGUID(legacyGUID))
		}
		return true, nil
	case modernworld.CMSGQuestGiverCompleteQuest, modernworld.CMSGQuestGiverRequestReward:
		var (
			request modernworld.QuestGiverRequest
			err     error
			opcode  = legacyworld.CMSGQuestGiverCompleteQuest
		)
		if packet.Opcode == modernworld.CMSGQuestGiverCompleteQuest {
			request, err = modernworld.ParseQuestGiverCompleteQuest(packet.Body)
		} else {
			request, err = modernworld.ParseQuestGiverRequestReward(packet.Body)
			opcode = legacyworld.CMSGQuestGiverRequestReward
		}
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacyGUIDForModernLocked(request.Giver)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if (request.Giver.Low != 0 || request.Giver.High != 0) && !known {
			return true, fmt.Errorf("quest reward request references an unknown quest giver")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("quest reward request", "account", session.legacy.Username, "quest", request.QuestID, "opcode", opcode)
		return true, legacyConn.WritePacket(opcode, modernworld.EncodeLegacyQuestGiverRequest(legacyGUID, request.QuestID))
	case modernworld.CMSGQuestGiverChooseReward:
		request, err := modernworld.ParseQuestGiverChooseReward(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacyGUIDForModernLocked(request.Giver)
		choices := session.questRewardChoices[request.QuestID]
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if (request.Giver.Low != 0 || request.Giver.High != 0) && !known {
			return true, fmt.Errorf("quest reward choice references an unknown quest giver")
		}
		choice, err := modernworld.QuestRewardChoiceIndex(request.ItemID, choices)
		if err != nil {
			return true, err
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("choose quest reward", "account", session.legacy.Username, "quest", request.QuestID, "item", request.ItemID, "choice", choice)
		return true, legacyConn.WritePacket(legacyworld.CMSGQuestGiverChooseReward, modernworld.EncodeLegacyQuestChooseReward(legacyGUID, request.QuestID, choice))
	case modernworld.CMSGQuestLogRemoveQuest:
		slot, err := modernworld.ParseQuestLogRemoveQuest(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("abandon quest", "account", session.legacy.Username, "slot", slot)
		return true, legacyConn.WritePacket(legacyworld.CMSGQuestLogRemoveQuest, modernworld.EncodeLegacyQuestLogRemove(slot))
	case modernworld.CMSGQueryPlayerNames:
		guids, err := modernworld.ParseQueryPlayerNames(packet.Body)
		if err != nil {
			return true, err
		}
		s.log.Debug("player-name query received", "account", session.legacy.Username, "guids", guids, "hex", hex.EncodeToString(packet.Body))
		type nameLookup struct {
			modern   modernworld.GUID128
			legacy   uint64
			identity modernworld.LegacyNameIdentity
			level    byte
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		realmAddress := realm.Address(session.selectedRealm.ID)
		local := make([]nameLookup, 0, len(guids))
		forward := make([]nameLookup, 0)
		for _, guid := range guids {
			if guid.Low == 0 && guid.High == 0 {
				continue
			}
			legacyGUID, known := session.legacyGUIDForModernLocked(guid)
			playerGUID, isPlayer := modernworld.LegacyPlayerGUIDFromModern(guid)
			if !known && isPlayer && playerGUID != 0 {
				legacyGUID, known = playerGUID, true
			}
			if session.currentCharacter != 0 && guid.Low == uint64(uint32(session.currentCharacter)) {
				legacyGUID = session.currentCharacter
				known = true
				isPlayer = true
			}
			if !isPlayer {
				s.log.Debug("player-name query skipped non-player", "account", session.legacy.Username, "guid", guid)
				// Creature/item GUIDs must not be failed through the player name
				// cache: a Result=1 entry is what 3.4.3 displays as 未知目标.
				continue
			}
			if identity, level, ok := session.playerNameIdentityLocked(legacyGUID); ok {
				local = append(local, nameLookup{modern: guid, legacy: legacyGUID, identity: identity, level: level})
				continue
			}
			if known && legacyGUID != 0 {
				session.rememberPendingNameQueryLocked(legacyGUID, guid)
				forward = append(forward, nameLookup{modern: guid, legacy: legacyGUID})
			}
		}
		session.worldMu.Unlock()
		for _, lookup := range local {
			s.log.Debug("player-name query cache hit", "account", session.legacy.Username, "legacy_guid", lookup.legacy, "modern_guid", lookup.modern, "name", lookup.identity.Name)
			body, encodeErr := modernworld.EncodeQueryPlayerNameLookup(lookup.modern, lookup.identity, lookup.level, realmAddress,
				s.socialWowAccountGUID(session, lookup.legacy), s.socialBNetAccountGUID(session, lookup.legacy))
			if encodeErr != nil {
				return true, encodeErr
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGQueryPlayerNames, Body: body}); err != nil {
				return true, err
			}
		}
		if legacyConn == nil {
			return true, nil
		}
		for _, lookup := range forward {
			s.log.Debug("player-name query forwarded", "account", session.legacy.Username, "legacy_guid", lookup.legacy, "modern_guid", lookup.modern)
			if err := legacyConn.WritePacket(legacyworld.CMSGNameQuery, modernworld.EncodeLegacyNameQuery(lookup.legacy)); err != nil {
				return true, err
			}
		}
		return true, nil
	case modernworld.CMSGQueryPetName:
		guid, err := modernworld.ParseQueryPetName(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacyGUIDForModernLocked(guid)
		if !known {
			legacyGUID, known = modernworld.LegacyPetGUIDFromModern(guid)
		}
		petNumber, validPet := modernworld.LegacyPetNumber(legacyGUID)
		legacyConn := session.legacyWorld
		shouldQuery := false
		if known && validPet {
			if session.pendingPetNameGUIDs == nil {
				session.pendingPetNameGUIDs = make(map[uint32][]modernworld.GUID128)
			}
			// Coalesce repeated client queries while the legacy response is in
			// flight. The slice also safely handles an older mismatched identity
			// already cached before this session was reset.
			pending := session.pendingPetNameGUIDs[petNumber]
			duplicate := false
			for _, queued := range pending {
				if queued == guid {
					duplicate = true
					break
				}
			}
			if !duplicate {
				session.pendingPetNameGUIDs[petNumber] = append(pending, guid)
				shouldQuery = len(pending) == 0
			}
		}
		session.worldMu.Unlock()
		if !known || !validPet {
			if dest == nil {
				return true, nil
			}
			body, encodeErr := modernworld.EncodeQueryPetNameResponse(guid, modernworld.LegacyPetNameResponse{})
			if encodeErr != nil {
				return true, encodeErr
			}
			return true, dest.WritePacket(modernworld.SMSGQueryPetName, body)
		}
		if legacyConn == nil {
			return true, nil
		}
		if !shouldQuery {
			return true, nil
		}
		s.log.Debug("query pet name", "account", session.legacy.Username, "pet", fmt.Sprintf("0x%x", legacyGUID), "number", petNumber)
		return true, legacyConn.WritePacket(legacyworld.CMSGPetNameQuery, modernworld.EncodeLegacyPetNameQuery(petNumber, legacyGUID))
	case modernworld.CMSGWho:
		request, err := modernworld.ParseWhoRequest(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		session.lastWhoRequestID = request.RequestID
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("online-player search", "account", session.legacy.Username, "request_id", request.RequestID,
			"name", request.Name, "guild", request.Guild, "areas", len(request.Areas), "words", len(request.Words))
		return true, legacyConn.WritePacket(legacyworld.CMSGWho, modernworld.EncodeLegacyWhoRequest(request))
	case modernworld.CMSGChatMessageWhisper:
		whisper, err := modernworld.ParseChatWhisper(packet.Body)
		if err != nil {
			return true, err
		}
		body, err := modernworld.EncodeLegacyChatWhisper(whisper)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		session.lastWhisperTarget = whisper.Target
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("whisper message", "account", session.legacy.Username, "target", whisper.Target, "bytes", len(whisper.Text))
		return true, legacyConn.WritePacket(legacyworld.CMSGMessageChat, body)
	case modernworld.CMSGRequestPartyJoinUpdates:
		if err := modernworld.ParsePartyJoinUpdates(packet.Body); err != nil {
			return true, err
		}
		// WotLK has no party-join subscription stream. Its group list and member
		// status packets are pushed directly by the world server.
		return true, nil
	case modernworld.CMSGDoReadyCheck:
		selector, err := modernworld.ParseDoReadyCheck(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.readyCheckGeneration++
		generation := session.readyCheckGeneration
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("start ready check", "account", session.legacy.Username, "party_index", selector.PartyIndex)
		if err := legacyConn.WritePacket(legacyworld.MSGRaidReadyCheck, nil); err != nil {
			return true, err
		}
		s.scheduleLegacyReadyCheckFinish(session, generation)
		return true, nil
	case modernworld.CMSGReadyCheckResponse:
		response, err := modernworld.ParseReadyCheckResponse(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		partyGUID := session.partyGUID
		playerGUID := session.modernGUIDForLegacyLocked(session.currentCharacter)
		session.worldMu.Unlock()
		if legacyConn != nil {
			ready := byte(0)
			if response.Ready {
				ready = 1
			}
			if err := legacyConn.WritePacket(legacyworld.MSGRaidReadyCheck, []byte{ready}); err != nil {
				return true, err
			}
		}
		// The 3.3.5 server excludes the responding player from its broadcast.
		// Echo the semantic response locally so the 3.4.3 roster updates immediately.
		body := modernworld.EncodeReadyCheckResponse(partyGUID, playerGUID, response.Ready)
		return true, session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGReadyCheckResponse, Body: body})
	case modernworld.CMSGPartyUninvite:
		request, err := modernworld.ParsePartyUninvite(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyTarget, known := session.legacyPartyTargetLocked(request.Target)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known {
			return true, fmt.Errorf("party uninvite references an unknown player")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("party uninvite", "account", session.legacy.Username, "target", fmt.Sprintf("0x%x", legacyTarget), "reason", request.Reason)
		return true, legacyConn.WritePacket(legacyworld.CMSGGroupUninviteGUID, modernworld.EncodeLegacyPartyUninvite(legacyTarget, request.Reason))
	case modernworld.CMSGSetPartyLeader:
		request, err := modernworld.ParseSetPartyLeader(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyTarget, known := session.legacyPartyTargetLocked(request.Target)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known {
			return true, fmt.Errorf("set-party-leader references an unknown player")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("set party leader", "account", session.legacy.Username, "target", fmt.Sprintf("0x%x", legacyTarget))
		return true, legacyConn.WritePacket(legacyworld.CMSGGroupSetLeader, modernworld.EncodeLegacySetPartyLeader(legacyTarget))
	case modernworld.CMSGSetLootMethod:
		request, err := modernworld.ParseSetLootMethod(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		lootMaster, known := session.legacyPartyTargetLocked(request.LootMaster)
		legacyConn := session.legacyWorld
		// The 3.3.5 server normally confirms this through the next party
		// snapshot.  Apply the setting locally as well so a loot window opened
		// in the same round-trip immediately gets master-loot permission; the
		// authoritative snapshot will overwrite it when it arrives.
		if known {
			session.partyLootMethod = request.Method
			session.partyLootMaster = lootMaster
			session.partyLootThreshold = byte(request.LootThreshold)
		}
		session.worldMu.Unlock()
		if !known {
			return true, fmt.Errorf("set-loot-method references an unknown loot master")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("set loot method", "account", session.legacy.Username, "method", request.Method,
			"threshold", request.LootThreshold, "master", fmt.Sprintf("0x%x", lootMaster))
		return true, legacyConn.WritePacket(legacyworld.CMSGSetLootMethod, modernworld.EncodeLegacySetLootMethod(request, lootMaster))
	case modernworld.CMSGOptOutOfLoot:
		pass, err := modernworld.ParseOptOutOfLoot(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		state := uint32(0)
		if pass {
			state = 1
		}
		// AzerothCore reads passOnLoot as uint32, not the modern one-bit flag.
		return true, legacyConn.WritePacket(legacyworld.CMSGOptOutOfLoot, binary.LittleEndian.AppendUint32(nil, state))
	case modernworld.CMSGConvertRaid:
		raid, err := modernworld.ParseConvertRaid(packet.Body)
		if err != nil {
			return true, err
		}
		if !raid {
			// AzerothCore 3.3.5 only supports party -> raid. Forwarding the false
			// direction would incorrectly execute the same conversion again.
			s.log.Warn("raid-to-party conversion is unavailable on 3.3.5", "account", session.legacy.Username)
			return true, nil
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGGroupRaidConvert, nil)
	case modernworld.CMSGSetAssistantLeader:
		request, err := modernworld.ParseSetAssistantLeader(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyTarget, known := session.legacyPartyTargetLocked(request.Target)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known {
			return true, fmt.Errorf("set-assistant-leader references an unknown player")
		}
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGGroupAssistantLeader,
			modernworld.EncodeLegacySetAssistantLeader(legacyTarget, request.Apply))
	case modernworld.CMSGSetEveryoneIsAssistant:
		request, err := modernworld.ParseSetEveryoneIsAssistant(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		currentCharacter := session.currentCharacter
		members := make([]uint64, 0, len(session.partyMembers))
		for legacyGUID := range session.partyMembers {
			if legacyGUID != 0 && legacyGUID != currentCharacter {
				members = append(members, legacyGUID)
			}
		}
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		for _, legacyTarget := range members {
			if err := legacyConn.WritePacket(legacyworld.CMSGGroupAssistantLeader,
				modernworld.EncodeLegacySetAssistantLeader(legacyTarget, request.Apply)); err != nil {
				return true, err
			}
		}
		return true, nil
	case modernworld.CMSGSetPartyAssignment:
		request, err := modernworld.ParseSetPartyAssignment(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyTarget, known := session.legacyPartyTargetLocked(request.Target)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known {
			return true, fmt.Errorf("party assignment references an unknown player")
		}
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.MSGPartyAssignment,
			modernworld.EncodeLegacySetPartyAssignment(request, legacyTarget))
	case modernworld.CMSGInitiateRolePoll:
		selector, err := modernworld.ParseInitiateRolePoll(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		if !selector.HasParty {
			selector.PartyIndex = session.partyIndex
		}
		initiator := session.modernGUIDForLegacyLocked(session.currentCharacter)
		session.worldMu.Unlock()
		body := modernworld.EncodeRolePollInform(selector.PartyIndex, initiator)
		return true, s.broadcastPartyPacket(session, modernworld.Packet{Opcode: modernworld.SMSGRolePollInform, Body: body})
	case modernworld.CMSGSetRole:
		request, err := modernworld.ParseSetRole(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyTarget, known := session.legacyPartyTargetLocked(request.Target)
		if !request.HasParty {
			request.PartyIndex = session.partyIndex
		}
		oldRole := session.partyRoles[legacyTarget]
		from := session.modernGUIDForLegacyLocked(session.currentCharacter)
		legacyConn := session.legacyWorld
		currentCharacter := session.currentCharacter
		if legacyTarget == currentCharacter {
			session.lfgRoles = request.Role
		}
		session.worldMu.Unlock()
		if !known {
			return true, fmt.Errorf("set-role references an unknown player")
		}
		for _, partySession := range s.partySessions(session) {
			partySession.worldMu.Lock()
			if partySession.partyRoles == nil {
				partySession.partyRoles = make(map[uint64]byte)
			}
			partySession.partyRoles[legacyTarget] = request.Role
			partySession.worldMu.Unlock()
		}
		// 3.3.5 has no normal role-poll opcode. Its LFG role byte is the only
		// server-side equivalent and is valid when a player confirms their own role.
		if legacyConn != nil && legacyTarget == currentCharacter {
			if err := legacyConn.WritePacket(legacyworld.CMSGDFSetRoles, []byte{request.Role}); err != nil {
				return true, err
			}
		}
		body := modernworld.EncodeRoleChangedInform(request.PartyIndex, from, request.Target, oldRole, request.Role)
		return true, s.broadcastPartyPacket(session, modernworld.Packet{Opcode: modernworld.SMSGRoleChangedInform, Body: body})
	case modernworld.CMSGDFSetRoles:
		roles, err := modernworld.ParseDFSetRoles(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		if session.partyRoles == nil {
			session.partyRoles = make(map[uint64]byte)
		}
		session.partyRoles[session.currentCharacter] = roles
		session.lfgRoles = roles
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGDFSetRoles, []byte{roles})
	case modernworld.CMSGDFJoin:
		request, err := modernworld.ParseDFJoin(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		session.lfgRoles = request.Roles
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("lfg join", "account", session.legacy.Username, "roles", request.Roles, "slots", len(request.Slots))
		return true, legacyConn.WritePacket(legacyworld.CMSGLfgJoin, modernworld.EncodeLegacyLFGJoin(request))
	case modernworld.CMSGDFLeave:
		if err := modernworld.ParseDFLeave(packet.Body); err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("lfg leave", "account", session.legacy.Username)
		return true, legacyConn.WritePacket(legacyworld.CMSGLfgLeave, nil)
	case modernworld.CMSGDFProposalResponse:
		response, err := modernworld.ParseDFProposalResponse(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("lfg proposal response", "account", session.legacy.Username, "proposal", response.ProposalID, "accept", response.Accept)
		return true, legacyConn.WritePacket(legacyworld.CMSGLfgProposalResult, modernworld.EncodeLegacyLFGProposalResult(response))
	case modernworld.CMSGDFGetSystemInfo:
		player, err := modernworld.ParseDFGetSystemInfo(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		opcode := legacyworld.CMSGLfdPartyLockInfoRequest
		if player {
			opcode = legacyworld.CMSGLfdPlayerLockInfoRequest
		}
		s.log.Debug("lfg system info", "account", session.legacy.Username, "player", player)
		return true, legacyConn.WritePacket(opcode, nil)
	case modernworld.CMSGDFGetJoinStatus:
		if err := modernworld.ParseDFGetJoinStatus(packet.Body); err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("lfg join status", "account", session.legacy.Username)
		return true, legacyConn.WritePacket(legacyworld.CMSGLfgGetStatus, nil)
	case modernworld.CMSGDFBootPlayerVote:
		agree, err := modernworld.ParseDFBootPlayerVote(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		vote := byte(0)
		if agree {
			vote = 1
		}
		s.log.Debug("lfg boot vote", "account", session.legacy.Username, "agree", agree)
		return true, legacyConn.WritePacket(legacyworld.CMSGLfgSetBootVote, []byte{vote})
	case modernworld.CMSGDFTeleport:
		out, err := modernworld.ParseDFTeleport(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		// The legacy proxy also prints a vendor chat line. That text is not part of the protocol.
		direction := byte(0)
		if out {
			direction = 1
		}
		s.log.Debug("lfg teleport", "account", session.legacy.Username, "out", out)
		return true, legacyConn.WritePacket(legacyworld.CMSGLfgTeleport, []byte{direction})
	case modernworld.CMSGChangeSubGroup:
		request, err := modernworld.ParseChangeSubGroup(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyTarget, known := session.legacyPartyTargetLocked(request.Target)
		member, named := session.partyMembers[legacyTarget]
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known || !named || member.Name == "" {
			return true, fmt.Errorf("change-subgroup references a player without a cached group name")
		}
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGGroupChangeSubGroup,
			modernworld.EncodeLegacyChangeSubGroup(member.Name, request.NewSubGroup))
	case modernworld.CMSGSwapSubGroups:
		request, err := modernworld.ParseSwapSubGroups(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		firstGUID, firstKnown := session.legacyPartyTargetLocked(request.FirstTarget)
		secondGUID, secondKnown := session.legacyPartyTargetLocked(request.SecondTarget)
		first, firstNamed := session.partyMembers[firstGUID]
		second, secondNamed := session.partyMembers[secondGUID]
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !firstKnown || !secondKnown || !firstNamed || !secondNamed || first.Name == "" || second.Name == "" {
			return true, fmt.Errorf("swap-subgroups references a player without a cached group name")
		}
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGGroupSwapSubGroup,
			modernworld.EncodeLegacySwapSubGroups(first.Name, second.Name))
	case modernworld.CMSGMinimapPing:
		request, err := modernworld.ParseMinimapPing(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.MSGMinimapPing, modernworld.EncodeLegacyMinimapPing(request))
	case modernworld.CMSGRandomRoll:
		request, err := modernworld.ParseRandomRoll(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.MSGRandomRoll, modernworld.EncodeLegacyRandomRoll(request))
	case modernworld.CMSGRequestPartyMemberStats:
		request, err := modernworld.ParsePartyMemberStatsRequest(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyTarget, known := session.legacyGUIDForModernLocked(request.Target)
		if !known {
			legacyTarget, known = modernworld.LegacyPlayerGUIDFromModern(request.Target)
		}
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known {
			return true, fmt.Errorf("party-member-stats request references an unknown player")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("request party-member stats", "account", session.legacy.Username,
			"party_index", request.PartyIndex, "target", fmt.Sprintf("0x%x", legacyTarget))
		return true, legacyConn.WritePacket(legacyworld.CMSGRequestPartyMemberStats,
			modernworld.EncodeLegacyPartyMemberStatsRequest(legacyTarget))
	case modernworld.CMSGUpdateRaidTarget:
		request, err := modernworld.ParseRaidTargetRequest(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyTarget, known := session.legacyGUIDForModernLocked(request.Target)
		if !known {
			legacyTarget, known = modernworld.LegacyPlayerGUIDFromModern(request.Target)
		}
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known {
			return true, fmt.Errorf("raid-target request references an unknown object")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("update raid target", "account", session.legacy.Username,
			"party_index", request.PartyIndex, "symbol", request.Symbol, "target", fmt.Sprintf("0x%x", legacyTarget))
		return true, legacyConn.WritePacket(uint32(legacyworld.MSGRaidTargetUpdate),
			modernworld.EncodeLegacyRaidTargetRequest(request, legacyTarget))
	case modernworld.CMSGPartyInvite:
		invite, err := modernworld.ParsePartyInvite(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("party invite", "account", session.legacy.Username, "target", invite.TargetName,
			"target_realm", invite.TargetRealm, "party_index", invite.PartyIndex)
		return true, legacyConn.WritePacket(legacyworld.CMSGGroupInvite, modernworld.EncodeLegacyPartyInvite(invite.TargetName))
	case modernworld.CMSGPartyInviteResponse:
		response, err := modernworld.ParsePartyInviteResponse(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		opcode := legacyworld.CMSGGroupDecline
		if response.Accept {
			opcode = legacyworld.CMSGGroupAccept
		}
		s.log.Debug("party invite response", "account", session.legacy.Username, "accept", response.Accept,
			"party_index", response.PartyIndex, "roles", response.RolesDesired)
		return true, legacyConn.WritePacket(opcode, modernworld.EncodeLegacyPartyInviteResponse(response))
	case modernworld.CMSGLeaveGroup:
		if len(packet.Body) != 1 {
			return true, fmt.Errorf("leave-group has %d bytes, want 1", len(packet.Body))
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGGroupDisband, nil)
	case modernworld.CMSGSetSelection, modernworld.CMSGAttackSwing:
		guid, err := modernworld.ParsePackedGUID128Exact(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacyGUIDForModernLocked(guid)
		if !known && packet.Opcode == modernworld.CMSGSetSelection {
			// Player GUIDs are reversible even before an update-object/name-query
			// response has populated the session object map.
			if playerGUID, ok := modernworld.LegacyPlayerGUIDFromModern(guid); ok {
				legacyGUID, known = playerGUID, true
			}
		}
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		if (guid.Low != 0 || guid.High != 0) && !known {
			return true, fmt.Errorf("play packet references an unknown object")
		}
		opcode := uint32(legacyworld.CMSGSetSelection)
		kind := "set selection"
		if packet.Opcode == modernworld.CMSGAttackSwing {
			opcode = legacyworld.CMSGAttackSwing
			kind = "attack swing"
		}
		s.log.Debug(kind, "account", session.legacy.Username, "legacy", fmt.Sprintf("0x%x", legacyGUID), "known", known)
		return true, legacyConn.WritePacket(opcode, modernworld.EncodeLegacyUnpackedGUID(legacyGUID))
	case modernworld.CMSGPetAction:
		request, err := modernworld.ParsePetAction(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		pet, petKnown := session.legacyGUIDForModernLocked(request.Pet)
		target, targetKnown := session.legacyGUIDForModernLocked(request.Target)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !petKnown || ((request.Target.Low != 0 || request.Target.High != 0) && !targetKnown) {
			return true, fmt.Errorf("pet-action references an unknown pet or target")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("pet action", "account", session.legacy.Username, "pet", fmt.Sprintf("0x%x", pet), "target", fmt.Sprintf("0x%x", target), "action", fmt.Sprintf("0x%x", request.Action))
		return true, legacyConn.WritePacket(legacyworld.CMSGPetAction, modernworld.EncodeLegacyPetAction(pet, request.Action, target))
	case modernworld.CMSGPetSetAction:
		// The 3.4.3 client sends CMSG_PET_SET_ACTION to store a pet action-bar
		// slot and, critically, to toggle a pet spell's auto-cast (the action's
		// type bits carry the enabled/disabled state). legacy proxy forwards it to
		// the legacy CMSG_PET_SET_ACTION handler so the server records the bar
		// and the auto-cast state; dropping it left auto-cast ineffective.
		request, err := modernworld.ParsePetSetAction(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		pet, known := session.legacyGUIDForModernLocked(request.Pet)
		if !known {
			// The pet may not have been cached through its create object yet;
			// reconstruct the legacy HighGuid::Pet from the modern GUID fields.
			pet, known = modernworld.LegacyPetGUIDFromModern(request.Pet)
		}
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known {
			return true, fmt.Errorf("pet-set-action references an unknown pet")
		}
		if legacyConn == nil {
			return true, nil
		}
		positions := make([]uint32, 0, len(request.Entries))
		for _, entry := range request.Entries {
			positions = append(positions, entry.Position)
		}
		s.log.Debug("pet set action", "account", session.legacy.Username, "pet", fmt.Sprintf("0x%x", pet), "positions", positions)
		return true, legacyConn.WritePacket(legacyworld.CMSGPetSetAction, modernworld.EncodeLegacyPetSetAction(pet, request.Entries))
	case modernworld.CMSGPetRename:
		request, err := modernworld.ParsePetRename(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		pet, known := session.legacyGUIDForModernLocked(request.Pet)
		if !known {
			pet, known = modernworld.LegacyPetGUIDFromModern(request.Pet)
		}
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known {
			return true, fmt.Errorf("pet-rename references an unknown pet")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("pet rename", "account", session.legacy.Username, "pet", fmt.Sprintf("0x%x", pet), "name", request.Name)
		return true, legacyConn.WritePacket(legacyworld.CMSGPetRename, modernworld.EncodeLegacyPetRename(pet, request))
	case modernworld.CMSGPetStopAttack, modernworld.CMSGPetAbandon:
		guid, err := modernworld.ParsePetGUID(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		pet, known := session.legacyGUIDForModernLocked(guid)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known {
			return true, fmt.Errorf("pet request references an unknown pet")
		}
		if legacyConn == nil {
			return true, nil
		}
		opcode := legacyworld.CMSGPetStopAttack
		kind := "pet stop attack"
		if packet.Opcode == modernworld.CMSGPetAbandon {
			opcode = legacyworld.CMSGPetAbandon
			kind = "pet abandon"
		}
		s.log.Debug(kind, "account", session.legacy.Username, "pet", fmt.Sprintf("0x%x", pet))
		return true, legacyConn.WritePacket(opcode, modernworld.EncodeLegacyUnpackedGUID(pet))
	case modernworld.CMSGPetCancelAura:
		request, err := modernworld.ParsePetCancelAura(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		pet, known := session.legacyGUIDForModernLocked(request.Pet)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known {
			return true, fmt.Errorf("pet-cancel-aura references an unknown pet")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("pet cancel aura", "account", session.legacy.Username, "pet", fmt.Sprintf("0x%x", pet), "spell", request.SpellID)
		return true, legacyConn.WritePacket(legacyworld.CMSGPetCancelAura, modernworld.EncodeLegacyPetCancelAura(pet, request.SpellID))
	case modernworld.CMSGPetAutocast:
		request, err := modernworld.ParsePetAutocast(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		pet, known := session.legacyGUIDForModernLocked(request.Pet)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known {
			return true, fmt.Errorf("pet-autocast references an unknown pet")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("pet autocast", "account", session.legacy.Username, "pet", fmt.Sprintf("0x%x", pet), "spell", request.SpellID, "enabled", request.Enabled)
		return true, legacyConn.WritePacket(legacyworld.CMSGPetSpellAutocast, modernworld.EncodeLegacyPetAutocast(pet, request.SpellID, request.Enabled))
	case modernworld.CMSGRequestPetInfo:
		if len(packet.Body) != 0 {
			return true, fmt.Errorf("request-pet-info has %d bytes, want 0", len(packet.Body))
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGRequestPetInfo, nil)
	case modernworld.CMSGRequestStabledPets, modernworld.CMSGStablePet, modernworld.CMSGBuyStableSlot:
		guid, err := modernworld.ParseStableMaster(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacyGUIDForModernLocked(guid)
		if known {
			session.rememberStableMasterLocked(legacyGUID)
		}
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if (guid.Low != 0 || guid.High != 0) && !known {
			return true, fmt.Errorf("stable-master request references an unknown NPC")
		}
		if legacyConn == nil {
			return true, nil
		}
		opcode := uint32(legacyworld.MSGListStabledPets)
		kind := "request stabled pets"
		if packet.Opcode == modernworld.CMSGStablePet {
			opcode = legacyworld.CMSGStablePet
			kind = "stable pet"
		} else if packet.Opcode == modernworld.CMSGBuyStableSlot {
			opcode = legacyworld.CMSGBuyStableSlot
			kind = "buy stable slot"
		}
		s.log.Debug(kind, "account", session.legacy.Username, "legacy", fmt.Sprintf("0x%x", legacyGUID))
		return true, legacyConn.WritePacket(opcode, modernworld.EncodeLegacyStableMaster(legacyGUID))
	case modernworld.CMSGUnstablePet, modernworld.CMSGStableSwapPet:
		request, err := modernworld.ParseStablePetNumberRequest(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacyGUIDForModernLocked(request.Master)
		if known {
			session.rememberStableMasterLocked(legacyGUID)
		}
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if (request.Master.Low != 0 || request.Master.High != 0) && !known {
			return true, fmt.Errorf("stable-pet-number request references an unknown NPC")
		}
		if legacyConn == nil {
			return true, nil
		}
		opcode := legacyworld.CMSGUnstablePet
		kind := "unstable pet"
		if packet.Opcode == modernworld.CMSGStableSwapPet {
			opcode = legacyworld.CMSGStableSwapPet
			kind = "stable swap pet"
		}
		s.log.Debug(kind, "account", session.legacy.Username, "legacy", fmt.Sprintf("0x%x", legacyGUID), "pet_number", request.PetNumber)
		return true, legacyConn.WritePacket(opcode, modernworld.EncodeLegacyStablePetNumber(legacyGUID, request.PetNumber))
	case modernworld.CMSGAttackStop:
		if len(packet.Body) != 0 {
			return true, fmt.Errorf("attack-stop has %d trailing bytes", len(packet.Body))
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGAttackStop, nil)
	case modernworld.CMSGSetSheathed:
		state, err := modernworld.ParseSetSheathed(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGSetSheathed, modernworld.EncodeLegacySheathed(state))
	case modernworld.CMSGStandStateChange:
		state, err := modernworld.ParseStandStateChange(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGStandStateChange, binary.LittleEndian.AppendUint32(nil, state))
	case modernworld.CMSGSendTextEmote:
		request, err := modernworld.ParseSendTextEmote(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		target, known := session.legacyGUIDForModernLocked(request.Target)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if (request.Target.Low != 0 || request.Target.High != 0) && !known {
			return true, fmt.Errorf("text-emote references an unknown object")
		}
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGTextEmote, modernworld.EncodeLegacyTextEmote(request, target))
	case modernworld.CMSGQueryCorpseLocation:
		player, err := modernworld.ParseQueryCorpseLocation(packet.Body)
		if err != nil {
			s.log.Warn("parse corpse-location query failed", "account", session.legacy.Username,
				"opcode", packet.Opcode, "hex", hex.EncodeToString(packet.Body), "error", err)
			player = modernworld.GUID128{}
		} else {
			s.log.Debug("corpse-location query", "account", session.legacy.Username,
				"opcode", packet.Opcode, "hex", hex.EncodeToString(packet.Body), "low", player.Low, "high", player.High)
		}
		session.worldMu.Lock()
		if player.Low == 0 && player.High == 0 && session.currentCharacter != 0 {
			player = session.modernGUIDForLegacyLocked(session.currentCharacter)
		}
		session.corpseQueryPlayer = player
		pending := session.pendingCorpseLocation
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if pending != nil {
			return true, session.sendCorpseLocation(*pending)
		}
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.MSGCorpseQuery, nil)
	case modernworld.CMSGRepopRequest:
		checkInstance, err := modernworld.ParseRepopRequest(packet.Body)
		if err != nil {
			return true, err
		}
		s.log.Debug("release spirit request", "account", session.legacy.Username, "bytes", len(packet.Body), "check_instance", checkInstance)
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGRepopRequest, modernworld.EncodeLegacyRepopRequest(checkInstance))
	case modernworld.CMSGCloseInteraction:
		// Hermes treats this client notification as a no-op. WotLK has no
		// corresponding request and the legacy gossip state closes itself.
		return true, nil
	case modernworld.CMSGTaxiNodeStatusQuery, modernworld.CMSGTaxiQueryAvailableNodes, modernworld.CMSGEnableTaxiNode:
		guid, err := modernworld.ParseTaxiNPC(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacyGUIDForModernLocked(guid)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known {
			return true, fmt.Errorf("taxi interaction references an unknown flight master")
		}
		if legacyConn == nil {
			return true, nil
		}
		opcode := legacyworld.CMSGTaxiNodeStatusQuery
		switch packet.Opcode {
		case modernworld.CMSGTaxiQueryAvailableNodes:
			opcode = legacyworld.CMSGTaxiQueryAvailableNodes
		case modernworld.CMSGEnableTaxiNode:
			opcode = legacyworld.CMSGGossipHello
		}
		return true, legacyConn.WritePacket(opcode, modernworld.EncodeLegacyTaxiNPC(legacyGUID))
	case modernworld.CMSGActivateTaxi:
		request, err := modernworld.ParseActivateTaxi(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacyGUIDForModernLocked(request.FlightMaster)
		legacyConn := session.legacyWorld
		source := session.currentTaxiNode
		usable := append([]byte(nil), session.usableTaxiNodes...)
		session.worldMu.Unlock()
		if !known {
			return true, fmt.Errorf("activate-taxi references an unknown flight master")
		}
		route, err := modernworld.ShortestTaxiRoute(source, request.Destination, usable)
		if err != nil {
			return true, err
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("activate taxi", "account", session.legacy.Username, "source", source, "destination", request.Destination, "route", route)
		if len(route) == 2 {
			return true, legacyConn.WritePacket(legacyworld.CMSGActivateTaxi, modernworld.EncodeLegacyActivateTaxi(legacyGUID, route[0], route[1]))
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGActivateTaxiExpress, modernworld.EncodeLegacyActivateTaxiExpress(legacyGUID, route))
	case modernworld.CMSGGossipSelectOption:
		request, err := modernworld.ParseGossipSelectOption(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacyGUIDForModernLocked(request.Giver)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if (request.Giver.Low != 0 || request.Giver.High != 0) && !known {
			return true, fmt.Errorf("gossip selection references an unknown object")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("select gossip option", "account", session.legacy.Username, "menu", request.GossipID, "option", request.OptionIndex)
		return true, legacyConn.WritePacket(legacyworld.CMSGGossipSelectOption,
			modernworld.EncodeLegacyGossipSelectOption(legacyGUID, request.GossipID, request.OptionIndex, request.PromotionCode))
	case modernworld.CMSGBinderActivate:
		guid, err := modernworld.ParseBinderActivate(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacyGUIDForModernLocked(guid)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if (guid.Low != 0 || guid.High != 0) && !known {
			return true, fmt.Errorf("binder activation references an unknown innkeeper")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("binder activate", "account", session.legacy.Username, "legacy", fmt.Sprintf("0x%x", legacyGUID))
		return true, legacyConn.WritePacket(legacyworld.CMSGBinderActivate, modernworld.EncodeLegacyUnpackedGUID(legacyGUID))
	case modernworld.CMSGBankerActivate, modernworld.CMSGBuyBankSlot:
		guid, err := modernworld.ParseBankNPCActivate(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacyGUIDForModernLocked(guid)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if (guid.Low != 0 || guid.High != 0) && !known {
			return true, fmt.Errorf("banker activation references an unknown NPC")
		}
		if legacyConn == nil {
			return true, nil
		}
		opcode := legacyworld.CMSGBankerActivate
		if packet.Opcode == modernworld.CMSGBuyBankSlot {
			opcode = legacyworld.CMSGBuyBankSlot
		}
		s.log.Debug("banker activation", "account", session.legacy.Username, "opcode", opcode, "legacy", fmt.Sprintf("0x%x", legacyGUID))
		return true, legacyConn.WritePacket(opcode, modernworld.EncodeLegacyUnpackedGUID(legacyGUID))
	case modernworld.CMSGTalkToGossip:
		guid, err := modernworld.ParseTalkToGossip(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacyGUIDForModernLocked(guid)
		legacyConn := session.legacyWorld
		fields := session.objectFields[session.currentCharacter]
		npcFlags := modernworld.LegacyNPCFlags(session.objectFields[legacyGUID])
		_, _, _, ghost := modernworld.LegacyPlayerVitalState(fields)
		session.worldMu.Unlock()
		if (guid.Low != 0 || guid.High != 0) && !known {
			return true, fmt.Errorf("gossip references an unknown object")
		}
		if legacyConn != nil && modernworld.LegacyUnitUsesSpellClick(legacyGUID, npcFlags) {
			s.log.Debug("spell-click via gossip", "account", session.legacy.Username, "legacy", fmt.Sprintf("0x%x", legacyGUID), "npc_flags", fmt.Sprintf("0x%x", npcFlags))
			return true, legacyConn.WritePacket(legacyworld.CMSGSpellClick, modernworld.EncodeLegacyUnpackedGUID(legacyGUID))
		}
		if legacyConn != nil {
			if err := legacyConn.WritePacket(legacyworld.CMSGGossipHello, modernworld.EncodeLegacyUnpackedGUID(legacyGUID)); err != nil {
				return true, err
			}
		}
		s.log.Debug("talk to gossip", "account", session.legacy.Username, "ghost", ghost, "low", guid.Low)
		return true, nil
	case modernworld.CMSGSpiritHealerActivate:
		guid, err := modernworld.ParseSpiritHealerActivate(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacyGUIDForModernLocked(guid)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if (guid.Low != 0 || guid.High != 0) && !known {
			return true, fmt.Errorf("spirit healer references an unknown object")
		}
		s.log.Debug("spirit healer activate", "account", session.legacy.Username, "low", guid.Low)
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGSpiritHealerActivate, modernworld.EncodeLegacyUnpackedGUID(legacyGUID))
	case modernworld.CMSGAuctionHelloRequest:
		auctioneer, err := modernworld.ParseAuctionHelloRequest(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyAuctioneer, known := session.legacyGUIDForModernLocked(auctioneer)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if (auctioneer.Low != 0 || auctioneer.High != 0) && !known {
			return true, fmt.Errorf("auction hello references an unknown auctioneer")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("auction hello request", "account", session.legacy.Username, "auctioneer", fmt.Sprintf("0x%x", legacyAuctioneer))
		return true, legacyConn.WritePacket(uint32(legacyworld.MsgAuctionHello), modernworld.EncodeLegacyAuctionHelloRequest(legacyAuctioneer))
	case modernworld.CMSGAuctionListOwnedItems:
		request, err := modernworld.ParseAuctionListOwnedQuery(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyAuctioneer, known := session.legacyGUIDForModernLocked(request.Auctioneer)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known {
			return true, fmt.Errorf("owned-items query references an unknown auctioneer")
		}
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGAuctionListOwnedItems, modernworld.EncodeLegacyAuctionOffsetQuery(legacyAuctioneer, request.Offset))
	case modernworld.CMSGAuctionListBiddedItems:
		request, err := modernworld.ParseAuctionListBidderQuery(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyAuctioneer, known := session.legacyGUIDForModernLocked(request.Auctioneer)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known {
			return true, fmt.Errorf("bidder-items query references an unknown auctioneer")
		}
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGAuctionListBidderItems, modernworld.EncodeLegacyAuctionListBidder(legacyAuctioneer, request.Offset, request.IDs))
	case modernworld.CMSGAuctionListPendingSales:
		// 3.4.3 sends this with an empty body after AuctionHello. AzerothCore's
		// pending-sales handler always returns count=0, and Hermes 54261 Opcode.cs
		// omits the CMSG entirely so the live client was stuck waiting.
		if dest == nil {
			return true, nil
		}
		s.log.Debug("auction list pending sales", "account", session.legacy.Username, "bytes", len(packet.Body))
		return true, dest.WritePacket(modernworld.SMSGAuctionListPendingSalesResult, modernworld.EncodeAuctionPendingSalesResult())
	case modernworld.CMSGAuctionRemoveItem:
		request, err := modernworld.ParseAuctionRemoveRequest(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyAuctioneer, known := session.legacyGUIDForModernLocked(request.Auctioneer)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known {
			return true, fmt.Errorf("remove-item request references an unknown auctioneer")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("auction remove item", "account", session.legacy.Username, "auction", request.AuctionID, "auctioneer", fmt.Sprintf("0x%x", legacyAuctioneer))
		return true, legacyConn.WritePacket(legacyworld.CMSGAuctionRemoveItem, modernworld.EncodeLegacyAuctionRemove(legacyAuctioneer, request.AuctionID))
	case modernworld.CMSGAuctionPlaceBid:
		request, err := modernworld.ParseAuctionBidRequest(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyAuctioneer, known := session.legacyGUIDForModernLocked(request.Auctioneer)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known {
			return true, fmt.Errorf("place-bid request references an unknown auctioneer")
		}
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGAuctionPlaceBid, modernworld.EncodeLegacyAuctionBid(legacyAuctioneer, request.AuctionID, request.Bid))
	case modernworld.CMSGAuctionSellItem:
		request, err := modernworld.ParseAuctionSellRequest(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyAuctioneer, known := session.legacyGUIDForModernLocked(request.Auctioneer)
		legacyConn := session.legacyWorld
		resolved := make([]uint64, 0, len(request.Items))
		for _, sale := range request.Items {
			legacy := session.legacyItemForModernLocked(sale.Item)
			if legacy == 0 && (sale.Item.Low != 0 || sale.Item.High != 0) {
				s.log.Warn("auction sell item GUID unresolvable", "account", session.legacy.Username,
					"item", fmt.Sprintf("0x%016x%016x", sale.Item.High, sale.Item.Low))
			}
			resolved = append(resolved, legacy)
		}
		body := modernworld.EncodeLegacyAuctionSellResolved(request, legacyAuctioneer, resolved)
		session.worldMu.Unlock()
		if !known {
			return true, fmt.Errorf("sell-item request references an unknown auctioneer")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("auction sell item", "account", session.legacy.Username, "auctioneer", fmt.Sprintf("0x%x", legacyAuctioneer), "items", len(request.Items), "bytes", len(body))
		return true, legacyConn.WritePacket(legacyworld.CMSGAuctionSellItem, body)
	case modernworld.CMSGAuctionListItems:
		request, err := modernworld.ParseAuctionSearchRequest(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyAuctioneer, known := session.legacyGUIDForModernLocked(request.Auctioneer)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known {
			return true, fmt.Errorf("search request references an unknown auctioneer")
		}
		if legacyConn == nil {
			return true, nil
		}
		invType, itemClass, itemSubclass := modernworld.LegacyAuctionSearchFilters(request)
		body := modernworld.EncodeLegacyAuctionSearch(request, legacyAuctioneer)
		s.log.Debug("auction search", "account", session.legacy.Username, "auctioneer", fmt.Sprintf("0x%x", legacyAuctioneer),
			"name", request.Name, "min", request.MinLevel, "max", request.MaxLevel, "quality", request.Quality,
			"usable", request.OnlyUsable, "filters", len(request.ClassFilters),
			"class", itemClass, "subclass", itemSubclass, "inv", invType, "sorts", len(request.Sorts), "bytes", len(body))
		return true, legacyConn.WritePacket(legacyworld.CMSGAuctionListItems, body)
	case modernworld.CMSGReclaimCorpse:
		guid, err := modernworld.ParseReclaimCorpse(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacyGUIDForModernLocked(guid)
		if !known || legacyGUID == 0 {
			legacyGUID = session.currentCharacter
		}
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		s.log.Debug("reclaim corpse", "account", session.legacy.Username, "bytes", len(packet.Body), "legacy", fmt.Sprintf("0x%x", legacyGUID))
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGReclaimCorpse, modernworld.EncodeLegacyUnpackedGUID(legacyGUID))
	case modernworld.CMSGRequestCemeteryList:
		s.log.Debug("request cemetery list", "account", session.legacy.Username, "bytes", len(packet.Body))
		return true, nil
	case modernworld.CMSGGameObjectUse, modernworld.CMSGGameObjectReportUse:
		guid, err := modernworld.ParseGameObjectUse(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacyGUIDForModernLocked(guid)
		legacyConn := session.legacyWorld
		if packet.Opcode == modernworld.CMSGGameObjectReportUse && known {
			session.currentInteractedGO = legacyGUID
		}
		session.worldMu.Unlock()
		if (guid.Low != 0 || guid.High != 0) && !known {
			return true, fmt.Errorf("game-object use references an unknown object")
		}
		if legacyConn == nil {
			return true, nil
		}
		body := modernworld.EncodeLegacyUnpackedGUID(legacyGUID)
		if packet.Opcode == modernworld.CMSGGameObjectReportUse {
			// REPORT_USE is a client-side interaction marker in the modern
			// protocol.  HermesProxy only records the object and waits for the
			// subsequent GAME_OBJ_USE (or spell cast); forwarding it as a legacy
			// use causes elevators and other multi-step game objects to execute
			// twice or be rejected by the server.
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGGameObjectUse, body)
	case modernworld.CMSGUseItem:
		request, err := modernworld.ParseUseItem(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		castItem, itemKnown := session.legacyGUIDForModernLocked(request.CastItem)
		if !itemKnown {
			session.worldMu.Unlock()
			return true, fmt.Errorf("use-item references an unknown item")
		}
		unit := uint64(0)
		item := uint64(0)
		if request.Cast.Target.Unit.Low != 0 || request.Cast.Target.Unit.High != 0 {
			unit, _ = session.legacyGUIDForModernLocked(request.Cast.Target.Unit)
		}
		if request.Cast.Target.Item.Low != 0 || request.Cast.Target.Item.High != 0 {
			item, _ = session.legacyGUIDForModernLocked(request.Cast.Target.Item)
		}
		srcTransport := resolveSpellCastTransportLocked(session, request.Cast.Target.Src)
		dstTransport := resolveSpellCastTransportLocked(session, request.Cast.Target.Dst)
		var moveBody []byte
		if request.Cast.Move != nil {
			moverGUID, moverKnown := session.legacyGUIDForModernLocked(request.Cast.Move.Mover)
			transportGUID, transportKnown := session.legacyGUIDForModernLocked(request.Cast.Move.Transport)
			transportEmpty := request.Cast.Move.Transport.Low == 0 && request.Cast.Move.Transport.High == 0
			if moverKnown && (transportKnown || transportEmpty) {
				if encoded, encodeErr := modernworld.EncodeLegacyPlayerMovement(*request.Cast.Move, moverGUID, transportGUID); encodeErr == nil {
					moveBody = encoded
				} else {
					s.log.Warn("encode use-item movement failed", "account", session.legacy.Username, "error", encodeErr)
				}
			}
		}
		mapID := session.currentMapID
		legacyConn := session.legacyWorld
		session.trimPendingCastsLocked(8)
		session.pendingCasts = append(session.pendingCasts, modernworld.PendingCast{
			SpellID:      request.Cast.SpellID,
			CastItemGUID: castItem,
			TargetGUID:   unit,
			ClientCastID: request.Cast.CastID,
			ServerCastID: modernworld.ModernCastGUID(mapID, request.Cast.SpellID, 10000+request.Cast.CastID.Low),
			VisualID:     request.Cast.SpellXSpellVisualID,
		})
		session.rememberSpellVisualLocked(request.Cast.SpellID, request.Cast.SpellXSpellVisualID)
		s.log.Debug("use item", "account", session.legacy.Username, "spell", request.Cast.SpellID, "visual", request.Cast.SpellXSpellVisualID, "bag", request.PackSlot, "slot", request.Slot, "item", fmt.Sprintf("0x%x", castItem), "glyph_index", request.Cast.Misc[0], "misc1", request.Cast.Misc[1], "target_flags", fmt.Sprintf("0x%x", request.Cast.Target.Flags), "dst_transport", formatSpellTargetTransport(request.Cast.Target.Dst), "legacy_dst_transport", fmt.Sprintf("0x%x", dstTransport), "dst", formatSpellTargetLocation(request.Cast.Target.Dst))
		abandoned := session.takeAbandonedCastPacketsLocked()
		session.worldMu.Unlock()
		if err := session.writeInstancePackets(abandoned); err != nil {
			return true, err
		}
		if legacyConn == nil {
			return true, nil
		}
		if len(moveBody) > 0 {
			legacyMove, _ := modernworld.LegacyOpcodeForModernMovement(modernworld.CMSGMoveHeartbeat)
			if err := legacyConn.WritePacket(legacyMove, moveBody); err != nil {
				return true, err
			}
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGUseItem, modernworld.EncodeLegacyUseItem(request, castItem, unit, item, srcTransport, dstTransport))
	case modernworld.CMSGCastSpell:
		request, err := modernworld.ParseCastSpell(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		if session.currentInteractedGO != 0 {
			modernworld.InjectGatheringGameObjectTarget(&request, session.modernGUIDForLegacyLocked(session.currentInteractedGO))
		}
		unit := uint64(0)
		item := uint64(0)
		if request.Target.Unit.Low != 0 || request.Target.Unit.High != 0 {
			if legacyGUID, known := session.legacyGUIDForModernLocked(request.Target.Unit); known {
				unit = legacyGUID
			}
		}
		if request.Target.Item.Low != 0 || request.Target.Item.High != 0 {
			if legacyGUID, known := session.legacyGUIDForModernLocked(request.Target.Item); known {
				item = legacyGUID
			}
		}
		srcTransport := resolveSpellCastTransportLocked(session, request.Target.Src)
		dstTransport := resolveSpellCastTransportLocked(session, request.Target.Dst)
		var moveBody []byte
		if request.Move != nil {
			moverGUID, moverKnown := session.legacyGUIDForModernLocked(request.Move.Mover)
			transportGUID, transportKnown := session.legacyGUIDForModernLocked(request.Move.Transport)
			transportEmpty := request.Move.Transport.Low == 0 && request.Move.Transport.High == 0
			if moverKnown && (transportKnown || transportEmpty) {
				if encoded, encodeErr := modernworld.EncodeLegacyPlayerMovement(*request.Move, moverGUID, transportGUID); encodeErr == nil {
					moveBody = encoded
				} else {
					s.log.Warn("encode cast movement failed", "account", session.legacy.Username, "error", encodeErr)
				}
			}
		}
		mapID := session.currentMapID
		legacyConn := session.legacyWorld
		session.trimPendingCastsLocked(8)
		pending := modernworld.PendingCast{
			SpellID:      request.SpellID,
			TargetGUID:   unit,
			ClientCastID: request.CastID,
			ServerCastID: modernworld.ModernCastGUID(mapID, request.SpellID, 10000+request.CastID.Low),
			VisualID:     request.SpellXSpellVisualID,
		}
		session.pendingCasts = append(session.pendingCasts, pending)
		session.rememberSpellVisualLocked(request.SpellID, request.SpellXSpellVisualID)
		castBody := modernworld.EncodeLegacyCastSpell(request, unit, item, srcTransport, dstTransport)
		forward := session.enqueuePlayerCastLocked(pending, castBody)
		s.log.Debug("cast spell", "account", session.legacy.Username, "spell", request.SpellID, "visual", request.SpellXSpellVisualID, "flags", request.SendCastFlags, "target", fmt.Sprintf("0x%x", unit), "has_move", request.Move != nil, "held", forward.held)
		abandoned := session.takeAbandonedCastPacketsLocked()
		session.worldMu.Unlock()
		if err := session.writeInstancePackets(abandoned); err != nil {
			return true, err
		}
		if legacyConn == nil || forward.held || len(forward.castBody) == 0 {
			return true, nil
		}
		if len(moveBody) > 0 {
			legacyMove, _ := modernworld.LegacyOpcodeForModernMovement(modernworld.CMSGMoveHeartbeat)
			if err := legacyConn.WritePacket(legacyMove, moveBody); err != nil {
				return true, err
			}
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGCastSpell, forward.castBody)
	case modernworld.CMSGUpdateMissileTrajectory:
		request, err := modernworld.ParseUpdateMissileTrajectory(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		guid, known := session.legacyGUIDForModernLocked(request.Guid)
		if !known {
			guid = session.currentCharacter
		}
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGUpdateMissileTrajectory, modernworld.EncodeLegacyUpdateMissileTrajectory(guid, request))
	case modernworld.CMSGRequestVehicleExit, modernworld.CMSGRequestVehiclePrevSeat, modernworld.CMSGRequestVehicleNextSeat, modernworld.CMSGRequestVehicleSwitchSeat, modernworld.CMSGRideVehicleInteract, modernworld.CMSGEjectPassenger, modernworld.CMSGMoveChangeVehicleSeats:
		return s.handleModernVehicle(session, packet)
	case modernworld.CMSGCancelAutoRepeatSpell:
		if err := modernworld.ParseEmptyControl(packet.Body); err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGCancelAutoRepeatSpell, nil)
	case modernworld.CMSGCancelChannelling:
		if _, err := modernworld.ParseCancelChannelling(packet.Body); err != nil {
			return true, err
		}
		session.worldMu.Lock()
		spellID := session.currentChanneledSpell
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil || spellID == 0 {
			// The 3.4.3 client re-sends cancel on every escape press with an
			// unreliable spell id; mirror Hermes by only cancelling the tracked
			// channeled spell.
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGCancelChannelling, binary.LittleEndian.AppendUint32(nil, spellID))
	case modernworld.CMSGPetCastSpell:
		request, err := modernworld.ParsePetCastSpell(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		pet, known := session.legacyGUIDForModernLocked(request.Pet)
		if !known || pet == 0 {
			session.worldMu.Unlock()
			return true, fmt.Errorf("pet cast references an unknown pet")
		}
		legacyConn := session.legacyWorld
		unit := resolveSpellCastTargetLocked(session, request.Cast.Target.Unit)
		item := resolveSpellCastTargetLocked(session, request.Cast.Target.Item)
		srcTransport := resolveSpellCastTransportLocked(session, request.Cast.Target.Src)
		dstTransport := resolveSpellCastTransportLocked(session, request.Cast.Target.Dst)
		mapID := session.currentMapID
		session.trimPendingCastsLocked(8)
		session.pendingCasts = append(session.pendingCasts, modernworld.PendingCast{
			SpellID:      request.Cast.SpellID,
			PetGUID:      pet,
			TargetGUID:   unit,
			ClientCastID: request.Cast.CastID,
			ServerCastID: modernworld.ModernCastGUID(mapID, request.Cast.SpellID, 10000+request.Cast.CastID.Low),
			VisualID:     request.Cast.SpellXSpellVisualID,
		})
		session.rememberSpellVisualLocked(request.Cast.SpellID, request.Cast.SpellXSpellVisualID)
		abandoned := session.takeAbandonedCastPacketsLocked()
		session.worldMu.Unlock()
		if err := session.writeInstancePackets(abandoned); err != nil {
			return true, err
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("pet cast spell", "account", session.legacy.Username, "spell", request.Cast.SpellID, "pet", fmt.Sprintf("0x%x", pet))
		return true, legacyConn.WritePacket(legacyworld.CMSGPetCastSpell, modernworld.EncodeLegacyPetCastSpell(pet, request.Cast, unit, item, srcTransport, dstTransport))
	case modernworld.CMSGPetLearnTalent:
		request, err := modernworld.ParsePetLearnTalent(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		pet, known := session.legacyGUIDForModernLocked(request.Pet)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known || pet == 0 {
			return true, fmt.Errorf("pet-learn-talent references an unknown pet")
		}
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGPetLearnTalent, modernworld.EncodeLegacyPetLearnTalent(pet, request.Talent, request.Rank))
	case modernworld.CMSGSelfRes:
		if err := modernworld.ParseSelfRes(packet.Body); err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGSelfRes, nil)
	case modernworld.CMSGSpellClick:
		target, err := modernworld.ParseSpellClick(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyTarget, known := session.legacyGUIDForModernLocked(target)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known || legacyTarget == 0 {
			return true, fmt.Errorf("spell-click target is unknown")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("spell click", "account", session.legacy.Username, "legacy", fmt.Sprintf("0x%x", legacyTarget))
		return true, legacyConn.WritePacket(legacyworld.CMSGSpellClick, modernworld.EncodeLegacyUnpackedGUID(legacyTarget))
	case modernworld.CMSGTotemDestroyed:
		slot, err := modernworld.ParseTotemDestroyed(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGTotemDestroyed, modernworld.EncodeLegacyTotemDestroyed(slot))
	case modernworld.CMSGResurrectResponse:
		caster, response, err := modernworld.ParseResurrectResponse(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacyGUIDForModernLocked(caster)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if (caster.Low != 0 || caster.High != 0) && !known {
			return true, fmt.Errorf("resurrect-response references an unknown object")
		}
		s.log.Debug("resurrect response", "account", session.legacy.Username, "accepted", response != 0, "legacy", fmt.Sprintf("0x%x", legacyGUID))
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGResurrectResponse, modernworld.EncodeLegacyResurrectResponse(legacyGUID, response != 0))
	case modernworld.CMSGSetActionBarToggles:
		mask, err := modernworld.ParseSetActionBarToggles(packet.Body)
		if err != nil {
			return true, err
		}
		// WotLK 0x01BF is CMSG_SET_ACTION_BAR_TOGGLES from the client and
		// SMSG_PETITION_SHOW_SIGNATURES from the server. This AzerothCore
		// build rejects the client packet. Extra-bar bits already arrive in
		// PLAYER_FIELD_BYTES on ActivePlayer create.
		s.log.Debug("set action bar toggles", "account", session.legacy.Username, "mask", mask)
		return true, nil
	case modernworld.CMSGSetFactionAtWar, modernworld.CMSGSetFactionNotAtWar:
		index, err := modernworld.ParseFactionAtWar(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		atWar := packet.Opcode == modernworld.CMSGSetFactionAtWar
		s.log.Debug("faction at-war toggle", "account", session.legacy.Username, "index", index, "atWar", atWar)
		return true, legacyConn.WritePacket(legacyworld.CMSGSetFactionAtWar, modernworld.EncodeLegacyFactionAtWar(uint32(index), atWar))
	case modernworld.CMSGSetFactionInactive:
		request, err := modernworld.ParseFactionInactive(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("faction inactive toggle", "account", session.legacy.Username, "index", request.Index, "inactive", request.Inactive)
		return true, legacyConn.WritePacket(legacyworld.CMSGSetFactionInactive, modernworld.EncodeLegacyFactionInactive(request.Index, request.Inactive))
	case modernworld.CMSGSetWatchedFaction:
		index, err := modernworld.ParseWatchedFaction(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("watched faction", "account", session.legacy.Username, "index", index)
		return true, legacyConn.WritePacket(legacyworld.CMSGSetWatchedFaction, modernworld.EncodeLegacyWatchedFaction(index))
	case modernworld.CMSGSetActionButton:
		change, err := modernworld.ParseSetActionButton(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil || int(change.Index) >= 144 {
			return true, nil
		}
		s.log.Debug("set action button", "account", session.legacy.Username, "index", change.Index, "packed", fmt.Sprintf("0x%x", change.Packed))
		return true, legacyConn.WritePacket(legacyworld.CMSGSetActionButton, modernworld.EncodeLegacySetActionButton(change))
	case modernworld.CMSGCancelCast:
		request, err := modernworld.ParseCancelCast(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		forwardCancel := session.cancelSpellQueueLocked(request.CastID, request.SpellID)
		abandoned := session.takeAbandonedCastPacketsLocked()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if err := session.writeInstancePackets(abandoned); err != nil {
			return true, err
		}
		if legacyConn == nil || !forwardCancel {
			s.log.Debug("cancel cast", "account", session.legacy.Username, "spell", request.SpellID, "forward", forwardCancel)
			return true, nil
		}
		s.log.Debug("cancel cast", "account", session.legacy.Username, "spell", request.SpellID)
		return true, legacyConn.WritePacket(legacyworld.CMSGCancelCast, modernworld.EncodeLegacyCancelCast(request.SpellID))
	case modernworld.CMSGCancelMountAura:
		if err := modernworld.ParseCancelMountAura(packet.Body); err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("cancel mount aura", "account", session.legacy.Username)
		return true, legacyConn.WritePacket(legacyworld.CMSGCancelMountAura, nil)
	case modernworld.CMSGCancelAura:
		spellID, _, err := modernworld.ParseCancelAura(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		if modernworld.CancelAuraUsesMountOpcode(spellID) {
			s.log.Debug("cancel remapped mount aura", "account", session.legacy.Username, "spell", spellID)
			return true, legacyConn.WritePacket(legacyworld.CMSGCancelMountAura, nil)
		}
		s.log.Debug("cancel aura", "account", session.legacy.Username, "spell", spellID)
		return true, legacyConn.WritePacket(legacyworld.CMSGCancelAura, modernworld.EncodeLegacyCancelAura(spellID))
	case modernworld.CMSGChatRegisterPrefixes:
		prefixes, err := modernworld.ParseChatAddonPrefixes(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		if session.addonPrefixes == nil {
			session.addonPrefixes = make(map[string]struct{}, len(prefixes))
		}
		for _, prefix := range prefixes {
			session.addonPrefixes[prefix] = struct{}{}
		}
		session.worldMu.Unlock()
		s.log.Debug("register addon prefixes", "account", session.legacy.Username, "count", len(prefixes))
		return true, nil
	case modernworld.CMSGChatUnregisterPrefixes:
		if len(packet.Body) != 0 {
			return true, fmt.Errorf("unregister addon prefixes has %d bytes, want 0", len(packet.Body))
		}
		session.worldMu.Lock()
		clear(session.addonPrefixes)
		session.worldMu.Unlock()
		return true, nil
	case modernworld.CMSGChatAddonMessage, modernworld.CMSGChatAddonMessageWhisper:
		request, err := modernworld.ParseChatAddonMessage(packet.Body, packet.Opcode == modernworld.CMSGChatAddonMessageWhisper)
		if err != nil {
			return true, err
		}
		body, err := modernworld.EncodeLegacyChatAddonMessage(request)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		if packet.Opcode == modernworld.CMSGChatAddonMessageWhisper {
			session.rememberAddonWhisperTargetLocked(modernworld.LegacySameRealmName(request.Target), time.Now())
		}
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("addon chat", "account", session.legacy.Username, "prefix", request.Prefix, "target", request.Target, "whisper", packet.Opcode == modernworld.CMSGChatAddonMessageWhisper)
		return true, legacyConn.WritePacket(legacyworld.CMSGMessageChat, body)
	case modernworld.CMSGChatJoinChannel, modernworld.CMSGChatLeaveChannel:
		var (
			request modernworld.ChatChannelRequest
			err     error
			body    []byte
			opcode  uint32
		)
		if packet.Opcode == modernworld.CMSGChatJoinChannel {
			request, err = modernworld.ParseChatJoinChannel(packet.Body)
			body = modernworld.EncodeLegacyChatJoinChannel(request)
			opcode = legacyworld.CMSGJoinChannel
		} else {
			request, err = modernworld.ParseChatLeaveChannel(packet.Body)
			body = modernworld.EncodeLegacyChatLeaveChannel(request)
			opcode = legacyworld.CMSGLeaveChannel
		}
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("chat channel request", "account", session.legacy.Username, "opcode", packet.Opcode, "id", request.ChannelID, "name", request.Name)
		return true, legacyConn.WritePacket(opcode, body)
	case modernworld.CMSGChatChannelList, modernworld.CMSGChatChannelDisplayList, modernworld.CMSGChatChannelOwner, modernworld.CMSGChatChannelAnnouncements, modernworld.CMSGChatChannelDeclineInvite:
		name, err := modernworld.ParseChatChannelCommand(packet.Body)
		if err != nil {
			return true, err
		}
		legacyOpcode := legacyworld.CMSGChannelList
		switch packet.Opcode {
		case modernworld.CMSGChatChannelDisplayList:
			legacyOpcode = legacyworld.CMSGChannelDisplayList
		case modernworld.CMSGChatChannelOwner:
			legacyOpcode = legacyworld.CMSGChannelOwner
		case modernworld.CMSGChatChannelAnnouncements:
			legacyOpcode = legacyworld.CMSGChannelAnnouncements
		case modernworld.CMSGChatChannelDeclineInvite:
			legacyOpcode = legacyworld.CMSGChannelDeclineInvite
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("chat channel command", "account", session.legacy.Username, "opcode", packet.Opcode, "legacy", legacyOpcode, "name", name)
		return true, legacyConn.WritePacket(legacyOpcode, modernworld.EncodeLegacyChatChannelCommand(name))
	case modernworld.CMSGChatMessageChannel:
		message, err := modernworld.ParseChatMessageChannel(packet.Body)
		if err != nil {
			return true, err
		}
		body, err := modernworld.EncodeLegacyChatChannelMessage(message)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("chat channel message", "account", session.legacy.Username, "channel", message.Target, "bytes", len(message.Text))
		return true, legacyConn.WritePacket(legacyworld.CMSGMessageChat, body)
	case modernworld.CMSGChatMessageGuild, modernworld.CMSGChatMessageOfficer,
		modernworld.CMSGChatMessageSay, modernworld.CMSGChatMessageYell,
		modernworld.CMSGChatMessageParty, modernworld.CMSGChatMessageRaid,
		modernworld.CMSGChatMessageInstanceChat, modernworld.CMSGChatMessageRaidWarning:
		message, err := modernworld.ParseChatMessage(packet.Body)
		if err != nil {
			return true, err
		}
		messageType, ok := modernworld.LegacyChatTypeForOpcode(packet.Opcode)
		if !ok {
			return true, fmt.Errorf("chat opcode %d has no legacy type", packet.Opcode)
		}
		body, err := modernworld.EncodeLegacyChatMessage(messageType, message.Language, message.Text)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("chat message", "account", session.legacy.Username, "type", messageType, "bytes", len(message.Text), "command", len(message.Text) > 0 && message.Text[0] == '.')
		return true, legacyConn.WritePacket(legacyworld.CMSGMessageChat, body)
	case modernworld.CMSGChatMessageAFK, modernworld.CMSGChatMessageDND, modernworld.CMSGChatMessageEmote:
		status, err := modernworld.ParseChatStatusMessage(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		race := byte(0)
		if identity, _, ok := session.playerNameIdentityLocked(session.currentCharacter); ok && identity.Race != 0 {
			race = identity.Race
		} else {
			race = modernworld.UnitRaceFromFields(session.objectFields[session.currentCharacter])
		}
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		body, err := modernworld.EncodeLegacyChatStatus(modernworld.LegacyChatStatusType(packet.Opcode), race, status)
		if err != nil {
			return true, err
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("chat status", "account", session.legacy.Username, "opcode", packet.Opcode, "bytes", len(status))
		return true, legacyConn.WritePacket(legacyworld.CMSGMessageChat, body)
	case modernworld.CMSGFarSight:
		enabled, err := modernworld.ParseFarSight(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		legacyBody := []byte{0}
		if enabled {
			legacyBody[0] = 1
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGFarSight, legacyBody)
	case modernworld.CMSGInspect, modernworld.CMSGQueryInspectAchievements:
		target, err := modernworld.ParsePackedGUID128Exact(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyTarget, known := session.legacyPartyTargetLocked(target)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known || legacyTarget == 0 {
			return true, fmt.Errorf("inspect target is unknown")
		}
		if legacyConn == nil {
			return true, nil
		}
		opcode := legacyworld.CMSGInspect
		if packet.Opcode == modernworld.CMSGQueryInspectAchievements {
			opcode = legacyworld.CMSGQueryInspectAchievements
		}
		return true, legacyConn.WritePacket(opcode, modernworld.EncodeLegacyUnpackedGUID(legacyTarget))
	case modernworld.CMSGInspectPvp:
		target, err := modernworld.ParsePackedGUID128Exact(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyTarget, known := session.legacyPartyTargetLocked(target)
		legacyConn := session.legacyWorld
		// A fresh PvP inspect replaces arena-team records cached from any earlier
		// inspection so stale slots from another player are not replayed.
		session.inspectArenaTeams = nil
		session.worldMu.Unlock()
		if !known || legacyTarget == 0 {
			return true, fmt.Errorf("pvp inspect target is unknown")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("pvp inspect", "account", session.legacy.Username, "target", fmt.Sprintf("0x%x", legacyTarget))
		return true, legacyConn.WritePacket(uint32(legacyworld.MsgInspectArenaTeams), modernworld.EncodeLegacyUnpackedGUID(legacyTarget))
	case modernworld.CMSGDuelResponse:
		response, err := modernworld.ParseDuelResponse(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyArbiter, known := session.legacyGUIDForModernLocked(response.Arbiter)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known || legacyArbiter == 0 {
			return true, fmt.Errorf("duel arbiter is unknown")
		}
		if legacyConn == nil {
			return true, nil
		}
		opcode := legacyworld.CMSGDuelCancelled
		if response.Accepted {
			opcode = legacyworld.CMSGDuelAccepted
		}
		s.log.Debug("duel response", "account", session.legacy.Username, "accepted", response.Accepted,
			"forfeited", response.Forfeited, "arbiter", fmt.Sprintf("0x%x", legacyArbiter))
		return true, legacyConn.WritePacket(opcode, modernworld.EncodeLegacyDuelResponse(legacyArbiter))
	case modernworld.CMSGCanDuel:
		target, err := modernworld.ParseCanDuel(packet.Body)
		if err != nil {
			return true, err
		}
		return true, session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGCanDuelResult,
			Body: modernworld.EncodeCanDuelResult(target, true)})
	case modernworld.CMSGPushQuestToParty:
		questID, err := modernworld.ParsePushQuestToParty(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGPushQuestToParty, modernworld.EncodeLegacyQuestID(questID))
	case modernworld.CMSGQuestConfirmAccept:
		questID, err := modernworld.ParseQuestConfirmAccept(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("quest confirm accept", "account", session.legacy.Username, "quest", questID)
		return true, legacyConn.WritePacket(legacyworld.CMSGQuestConfirmAccept, modernworld.EncodeLegacyQuestID(questID))
	case modernworld.CMSGQuestPushResult:
		request, err := modernworld.ParseQuestPushResult(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacySender, known := session.legacyGUIDForModernLocked(request.Sender)
		if !known {
			legacySender, known = modernworld.LegacyPlayerGUIDFromModern(request.Sender)
		}
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known {
			return true, fmt.Errorf("quest-push-result references an unknown sender")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("quest push result", "account", session.legacy.Username, "quest", request.QuestID,
			"modern_result", request.Result, "legacy_result", modernworld.LegacyQuestPushReason(request.Result),
			"sender", fmt.Sprintf("0x%x", legacySender))
		return true, legacyConn.WritePacket(uint32(legacyworld.MSGQuestPushResult),
			modernworld.EncodeLegacyQuestPushResult(legacySender, request.QuestID, request.Result))
	case modernworld.CMSGGetAccountNotifications:
		return true, modernworld.ParseGetAccountNotifications(packet.Body)
	case modernworld.CMSGLootUnit, modernworld.CMSGLootRelease:
		guid, err := modernworld.ParsePackedGUID128Exact(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacyGUIDForModernLocked(guid)
		// 3.4 CMSG_LOOT_RELEASE often carries the player or a LootObject
		// GUID. AC only clears UNIT_DYNFLAG_LOOTABLE when the packed GUID
		// is the corpse we opened, so always prefer the stored loot unit.
		if packet.Opcode == modernworld.CMSGLootRelease && session.lootLegacyGUID != 0 {
			legacyGUID = session.lootLegacyGUID
			known = true
		} else if !known && guid == session.lootObjModern && session.lootLegacyGUID != 0 {
			legacyGUID = session.lootLegacyGUID
			known = true
		}
		legacyConn := session.legacyWorld
		if packet.Opcode == modernworld.CMSGLootUnit {
			session.lootLegacyGUID = legacyGUID
			session.lastLootTargetLegacy = legacyGUID
		}
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		if (guid.Low != 0 || guid.High != 0) && !known {
			return true, fmt.Errorf("play packet references an unknown object")
		}
		opcode := uint32(legacyworld.CMSGLoot)
		kind := "loot unit"
		if packet.Opcode == modernworld.CMSGLootRelease {
			opcode = legacyworld.CMSGLootRelease
			kind = "loot release"
		}
		s.log.Debug(kind, "account", session.legacy.Username, "legacy", fmt.Sprintf("0x%x", legacyGUID))
		return true, legacyConn.WritePacket(opcode, modernworld.EncodeLegacyUnpackedGUID(legacyGUID))
	case modernworld.CMSGLootMoney:
		if err := modernworld.ParseLootMoney(packet.Body); err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("loot money", "account", session.legacy.Username)
		return true, legacyConn.WritePacket(legacyworld.CMSGLootMoney, nil)
	case modernworld.CMSGLootItem:
		slots, err := modernworld.ParseLootItemSlots(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("loot item", "account", session.legacy.Username, "slots", len(slots))
		for _, slot := range slots {
			if err := legacyConn.WritePacket(legacyworld.CMSGAutostoreLootItem, modernworld.EncodeLegacyAutostoreLootItem(slot)); err != nil {
				return true, err
			}
		}
		return true, nil
	case modernworld.CMSGLootRoll:
		request, err := modernworld.ParseLootRoll(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, completed := session.legacyLootRollRequestLocked(request)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if completed {
			s.log.Debug("ignore completed loot roll response", "account", session.legacy.Username, "object", request.LootObj, "slot", request.Slot)
			return true, nil
		}
		if legacyGUID == 0 {
			return true, fmt.Errorf("loot-roll references an unknown roll object %v slot %d", request.LootObj, request.Slot)
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("loot roll", "account", session.legacy.Username, "slot", request.Slot, "type", request.RollType)
		return true, legacyConn.WritePacket(legacyworld.CMSGLootRoll, modernworld.EncodeLegacyLootRoll(request, legacyGUID))
	case modernworld.CMSGMasterLootItem:
		request, err := modernworld.ParseMasterLootRequest(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		target, targetKnown := session.legacyPartyTargetLocked(request.Target)
		lootGUID := session.lootLegacyGUID
		lootObj := session.lootObjModern
		lootMethod := session.partyLootMethod
		lootMaster := session.partyLootMaster
		self := session.currentCharacter
		legacyConn := session.legacyWorld
		eligible := len(session.masterLootCandidates) == 0
		for _, candidate := range session.masterLootCandidates {
			if candidate == target {
				eligible = true
				break
			}
		}
		session.worldMu.Unlock()
		if !targetKnown || target == 0 {
			return true, fmt.Errorf("master-loot target is unknown")
		}
		if lootGUID == 0 || lootMethod != 2 || lootMaster != self {
			return true, fmt.Errorf("master-loot request is outside an active master-loot session")
		}
		if !eligible {
			return true, fmt.Errorf("master-loot target is not in the candidate list")
		}
		if legacyConn == nil {
			return true, nil
		}
		for _, item := range request.Items {
			if item.LootObj != lootObj {
				return true, fmt.Errorf("master-loot item references a different loot object")
			}
			if err := legacyConn.WritePacket(legacyworld.CMSGLootMasterGive,
				modernworld.EncodeLegacyMasterLootGive(lootGUID, item.Slot, target)); err != nil {
				return true, err
			}
		}
		s.log.Debug("master loot assigned", "account", session.legacy.Username, "target", fmt.Sprintf("0x%x", target), "items", len(request.Items))
		return true, nil
	case modernworld.CMSGAutoEquipItem:
		item, err := modernworld.ParseAutoEquipItem(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("auto equip item", "account", session.legacy.Username, "bag", item.PackSlot, "slot", item.Slot)
		return true, legacyConn.WritePacket(legacyworld.CMSGAutoEquipItem, modernworld.EncodeLegacyAutoEquipItem(item))
	case modernworld.CMSGAutobankItem, modernworld.CMSGAutostoreBankItem:
		item, err := modernworld.ParseAutoEquipItem(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		opcode := legacyworld.CMSGAutobankItem
		if packet.Opcode == modernworld.CMSGAutostoreBankItem {
			opcode = legacyworld.CMSGAutostoreBankItem
		}
		s.log.Debug("bank item", "account", session.legacy.Username, "opcode", opcode, "bag", item.PackSlot, "slot", item.Slot)
		return true, legacyConn.WritePacket(opcode, modernworld.EncodeLegacyAutoEquipItem(item))
	case modernworld.CMSGAutoStoreBagItem:
		item, err := modernworld.ParseAutoStoreBagItem(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("auto-store bag item", "account", session.legacy.Username, "src", item.SrcPack, "slot", item.SrcSlot, "dst", item.DstPack)
		return true, legacyConn.WritePacket(legacyworld.CMSGAutostoreBagItem, modernworld.EncodeLegacyAutoStoreBagItem(item))
	case modernworld.CMSGSplitItem:
		item, err := modernworld.ParseSplitItem(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("split item", "account", session.legacy.Username, "src", item.SrcPack, "slot", item.SrcSlot, "dst", item.DstPack, "qty", item.Quantity)
		return true, legacyConn.WritePacket(legacyworld.CMSGSplitItem, modernworld.EncodeLegacySplitItem(item))
	case modernworld.CMSGWrapItem:
		item, err := modernworld.ParseWrapItem(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("wrap item", "account", session.legacy.Username, "gift", item.GiftPack, "slot", item.GiftSlot, "item", item.ItemPack)
		return true, legacyConn.WritePacket(legacyworld.CMSGWrapItem, modernworld.EncodeLegacyWrapItem(item))
	case modernworld.CMSGCancelTempEnchantment:
		slot, err := modernworld.ParseCancelTempEnchantment(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("cancel temp enchant", "account", session.legacy.Username, "slot", slot)
		return true, legacyConn.WritePacket(legacyworld.CMSGCancelTempEnchantment, binary.LittleEndian.AppendUint32(nil, slot))
	case modernworld.CMSGSocketGems:
		request, err := modernworld.ParseSocketGems(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		item, itemKnown := session.legacyGUIDForModernLocked(request.Item)
		var gems [3]uint64
		for index, gem := range request.Gems {
			legacyGem, known := session.legacyGUIDForModernLocked(gem)
			if !known {
				session.worldMu.Unlock()
				return true, fmt.Errorf("socket-gems gem %d is unknown", index)
			}
			gems[index] = legacyGem
		}
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if (request.Item.Low != 0 || request.Item.High != 0) && !itemKnown {
			return true, fmt.Errorf("socket-gems item is unknown")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("socket gems", "account", session.legacy.Username, "item", fmt.Sprintf("0x%x", item))
		if err := legacyConn.WritePacket(legacyworld.CMSGSocketGems, modernworld.EncodeLegacySocketGems(item, gems)); err != nil {
			return true, err
		}
		success := modernworld.EncodeSocketGemsSuccess(request.Item)
		if dest != nil {
			return true, dest.WritePacket(modernworld.SMSGSocketGemsSuccess, success)
		}
		return true, session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSocketGemsSuccess, Body: success})
	case modernworld.CMSGAutoEquipItemSlot:
		item, err := modernworld.ParseAutoEquipItemSlot(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacyGUIDForModernLocked(item.Item)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if (item.Item.Low != 0 || item.Item.High != 0) && !known {
			return true, fmt.Errorf("auto-equip references an unknown item")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("auto equip item slot", "account", session.legacy.Username, "item", fmt.Sprintf("0x%x", legacyGUID), "slot", item.DstSlot)
		return true, legacyConn.WritePacket(legacyworld.CMSGAutoEquipItemSlot, modernworld.EncodeLegacyAutoEquipItemSlot(legacyGUID, item.DstSlot))
	case modernworld.CMSGSetAmmo:
		itemID, err := modernworld.ParseSetAmmo(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("set ammo", "account", session.legacy.Username, "item", itemID)
		return true, legacyConn.WritePacket(legacyworld.CMSGSetAmmo, modernworld.EncodeLegacySetAmmo(itemID))
	case modernworld.CMSGDestroyItem:
		request, err := modernworld.ParseDestroyItem(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		session.questItemSyncPending = true
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("destroy item", "account", session.legacy.Username, "container", request.Container, "slot", request.Slot, "count", request.Count)
		return true, legacyConn.WritePacket(legacyworld.CMSGDestroyItem, modernworld.EncodeLegacyDestroyItem(request))
	case modernworld.CMSGOpenItem:
		request, err := modernworld.ParseOpenItem(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("open item", "account", session.legacy.Username, "container", request.PackSlot, "slot", request.Slot)
		return true, legacyConn.WritePacket(legacyworld.CMSGOpenItem, modernworld.EncodeLegacyOpenItem(request))
	case modernworld.CMSGReadItem:
		request, err := modernworld.ParseReadItem(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("read item", "account", session.legacy.Username, "container", request.PackSlot, "slot", request.Slot)
		return true, legacyConn.WritePacket(legacyworld.CMSGReadItem, modernworld.EncodeLegacyReadItem(request))
	case modernworld.CMSGQueryPageText:
		request, err := modernworld.ParseQueryPageText(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		item, known := session.legacyGUIDForModernLocked(request.Item)
		if !known && request.Item.Low != 0 {
			item = uint64(uint32(request.Item.Low)) | (0x4000 << 48)
		}
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGQueryPageText, modernworld.EncodeLegacyQueryPageText(request.PageTextID, item))
	case modernworld.CMSGRepairItem:
		request, err := modernworld.ParseRepairItem(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		vendor, vendorKnown := session.legacyGUIDForModernLocked(request.Vendor)
		item, itemKnown := session.legacyGUIDForModernLocked(request.Item)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !vendorKnown || !itemKnown {
			return true, fmt.Errorf("repair-item references an unknown vendor or item")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("repair item", "account", session.legacy.Username, "vendor", fmt.Sprintf("0x%x", vendor), "item", fmt.Sprintf("0x%x", item), "guild_bank", request.UseGuildBank)
		return true, legacyConn.WritePacket(legacyworld.CMSGRepairItem, modernworld.EncodeLegacyRepairItem(request, vendor, item))
	case modernworld.CMSGInitiateTrade:
		target, err := modernworld.ParseInitiateTrade(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyTarget, known := session.legacyPartyTargetLocked(target)
		legacyConn := session.legacyWorld
		session.tradeID = 0
		session.tradeClientState = 0
		session.tradeServerState = 0
		session.tradeActive = true
		session.worldMu.Unlock()
		if !known || legacyTarget == 0 {
			return true, fmt.Errorf("trade target is unknown")
		}
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGInitiateTrade, modernworld.EncodeLegacyTradeGUID(legacyTarget))
	case modernworld.CMSGBeginTrade, modernworld.CMSGBusyTrade, modernworld.CMSGIgnoreTrade,
		modernworld.CMSGUnacceptTrade, modernworld.CMSGCancelTrade:
		if err := modernworld.ParseEmptyTrade(packet.Body); err != nil {
			return true, err
		}
		legacyOpcode := legacyworld.CMSGBeginTrade
		switch packet.Opcode {
		case modernworld.CMSGBusyTrade:
			legacyOpcode = legacyworld.CMSGBusyTrade
		case modernworld.CMSGIgnoreTrade:
			legacyOpcode = legacyworld.CMSGIgnoreTrade
		case modernworld.CMSGUnacceptTrade:
			legacyOpcode = legacyworld.CMSGUnacceptTrade
		case modernworld.CMSGCancelTrade:
			legacyOpcode = legacyworld.CMSGCancelTrade
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		created := session.activePlayerCreated
		session.worldMu.Unlock()
		if legacyConn == nil || !created {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyOpcode, nil)
	case modernworld.CMSGAcceptTrade:
		stateIndex, err := modernworld.ParseAcceptTrade(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		session.tradeClientState = stateIndex
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGAcceptTrade, modernworld.EncodeLegacyAcceptTrade(stateIndex))
	case modernworld.CMSGSetTradeGold:
		gold, err := modernworld.ParseSetTradeGold(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		session.tradeClientState++
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGSetTradeGold, modernworld.EncodeLegacySetTradeGold(gold))
	case modernworld.CMSGClearTradeItem:
		slot, err := modernworld.ParseClearTradeItem(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		session.tradeClientState++
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGClearTradeItem, []byte{slot})
	case modernworld.CMSGSetTradeItem:
		item, err := modernworld.ParseSetTradeItem(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		session.tradeClientState++
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		return true, legacyConn.WritePacket(legacyworld.CMSGSetTradeItem, modernworld.EncodeLegacySetTradeItem(item))
	case modernworld.CMSGSwapItem:
		swap, err := modernworld.ParseSwapItem(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("swap item", "account", session.legacy.Username,
			"dstBag", swap.DstBag, "dstSlot", swap.DstSlot, "srcBag", swap.SrcBag, "srcSlot", swap.SrcSlot)
		return true, legacyConn.WritePacket(legacyworld.CMSGSwapItem, modernworld.EncodeLegacySwapItem(swap))
	case modernworld.CMSGSwapInvItem:
		swap, err := modernworld.ParseSwapInvItem(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("swap inv item", "account", session.legacy.Username, "src", swap.SrcSlot, "dst", swap.DstSlot)
		return true, legacyConn.WritePacket(legacyworld.CMSGSwapInvItem, modernworld.EncodeLegacySwapInvItem(swap))
	case modernworld.CMSGListInventory, modernworld.CMSGTrainerList:
		var (
			guid modernworld.GUID128
			err  error
		)
		if packet.Opcode == modernworld.CMSGListInventory {
			guid, err = modernworld.ParseListInventory(packet.Body)
		} else {
			guid, err = modernworld.ParseTrainerList(packet.Body)
		}
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacyGUIDForModernLocked(guid)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if (guid.Low != 0 || guid.High != 0) && !known {
			return true, fmt.Errorf("NPC interaction references an unknown object")
		}
		if legacyConn == nil {
			return true, nil
		}
		opcode := legacyworld.CMSGListInventory
		if packet.Opcode == modernworld.CMSGTrainerList {
			opcode = legacyworld.CMSGTrainerList
		}
		return true, legacyConn.WritePacket(opcode, modernworld.EncodeLegacyUnpackedGUID(legacyGUID))
	case modernworld.CMSGBuyItem:
		request, err := modernworld.ParseBuyItem(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		vendor, known := session.legacyGUIDForModernLocked(request.Vendor)
		buyCount := session.vendorBuyCounts[request.ItemID]
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if (request.Vendor.Low != 0 || request.Vendor.High != 0) && !known {
			return true, fmt.Errorf("buy-item references an unknown vendor")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("buy item", "account", session.legacy.Username, "item", request.ItemID, "quantity", request.Quantity, "buyCount", buyCount, "muid", request.Muid)
		return true, legacyConn.WritePacket(legacyworld.CMSGBuyItem, modernworld.EncodeLegacyBuyItem(request, vendor, buyCount))
	case modernworld.CMSGSellItem:
		request, err := modernworld.ParseSellItem(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		vendor, vendorKnown := session.legacyGUIDForModernLocked(request.Vendor)
		item, itemKnown := session.legacyGUIDForModernLocked(request.Item)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !vendorKnown || !itemKnown {
			return true, fmt.Errorf("sell-item references an unknown vendor or item")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("sell item", "account", session.legacy.Username, "item", fmt.Sprintf("0x%x", item), "amount", request.Amount)
		return true, legacyConn.WritePacket(legacyworld.CMSGSellItem, modernworld.EncodeLegacySellItem(request, vendor, item))
	case modernworld.CMSGBuyBackItem:
		request, err := modernworld.ParseBuyBackItem(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		vendor, known := session.legacyGUIDForModernLocked(request.Vendor)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known {
			return true, fmt.Errorf("buyback-item references an unknown vendor")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("buy back item", "account", session.legacy.Username, "slot", request.Slot)
		return true, legacyConn.WritePacket(legacyworld.CMSGBuyBackItem, modernworld.EncodeLegacyBuyBackItem(request, vendor))
	case modernworld.CMSGTrainerBuySpell:
		request, err := modernworld.ParseTrainerBuySpell(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		trainer, known := session.legacyGUIDForModernLocked(request.Trainer)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if (request.Trainer.Low != 0 || request.Trainer.High != 0) && !known {
			return true, fmt.Errorf("trainer-buy-spell references an unknown trainer")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("learn trainer spell", "account", session.legacy.Username, "spell", request.SpellID)
		return true, legacyConn.WritePacket(legacyworld.CMSGTrainerBuySpell, modernworld.EncodeLegacyTrainerBuySpell(trainer, request.SpellID))
	case modernworld.CMSGLearnTalent:
		request, err := modernworld.ParseLearnTalent(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("learn talent", "account", session.legacy.Username, "talent", request.TalentID, "rank", request.Rank)
		return true, legacyConn.WritePacket(legacyworld.CMSGLearnTalent, modernworld.EncodeLegacyLearnTalent(request))
	case modernworld.CMSGConfirmRespecWipe:
		request, err := modernworld.ParseConfirmRespecWipe(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		trainer, known := session.legacyGUIDForModernLocked(request.Trainer)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if (request.Trainer.Low != 0 || request.Trainer.High != 0) && !known {
			return true, fmt.Errorf("confirm-respec-wipe references an unknown trainer")
		}
		if legacyConn == nil {
			return true, nil
		}
		switch request.RespecType {
		case modernworld.SpecResetTalents:
			s.log.Debug("confirm talent wipe", "account", session.legacy.Username, "trainer", trainer)
			return true, legacyConn.WritePacket(uint32(legacyworld.MSGTalentWipeConfirm), modernworld.EncodeLegacyTalentWipeConfirm(trainer))
		case modernworld.SpecResetPetTalents:
			s.log.Debug("confirm pet talent wipe", "account", session.legacy.Username, "trainer", trainer)
			return true, legacyConn.WritePacket(legacyworld.CMSGPetUnlearn, modernworld.EncodeLegacyTalentWipeConfirm(trainer))
		default:
			s.log.Debug("unhandled respec type", "account", session.legacy.Username, "type", request.RespecType)
			return true, nil
		}
	case modernworld.CMSGRequestForcedReactions:
		if len(packet.Body) != 0 {
			return true, fmt.Errorf("request-forced-reactions has %d bytes, want 0", len(packet.Body))
		}
		if dest == nil {
			return true, nil
		}
		return true, dest.WritePacket(modernworld.SMSGSetForcedReactions, binary.LittleEndian.AppendUint32(nil, 0))
	case modernworld.CMSGBattlemasterJoin:
		join, err := modernworld.ParseBattlegroundJoin(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacyGUIDForModernLocked(join.Battlemaster)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known {
			return true, fmt.Errorf("battlemaster GUID is unknown")
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("battlemaster join", "account", session.legacy.Username, "type", join.BgTypeID, "instance", join.InstanceID, "group", join.AsGroup)
		return true, legacyConn.WritePacket(legacyworld.CMSGBattlemasterJoin, modernworld.EncodeLegacyBattlegroundJoin(legacyGUID, join))
	case modernworld.CMSGBattlefieldPort:
		port, err := modernworld.ParseBattlefieldPort(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		queue, err := modernworld.RequireBattlegroundQueue(session.battlegroundQueues, port.TicketID)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if err != nil {
			return true, err
		}
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("battlefield port", "account", session.legacy.Username, "ticket", port.TicketID, "accept", port.Accept, "arena", queue.ArenaType)
		return true, legacyConn.WritePacket(legacyworld.CMSGBattlefieldPort, modernworld.EncodeLegacyBattlefieldPort(queue, port.Accept))
	case modernworld.CMSGBattlefieldList:
		listID, err := modernworld.ParseBattlefieldListRequest(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("battlefield list", "account", session.legacy.Username, "type", listID)
		return true, legacyConn.WritePacket(legacyworld.CMSGBattlefieldList, modernworld.EncodeLegacyBattlefieldListRequest(listID))
	case modernworld.CMSGBattlefieldLeave:
		session.worldMu.Lock()
		var queue *modernworld.BattlegroundQueue
		if stored, ok := session.battlegroundQueues[1]; ok {
			queue = &stored
		}
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("battlefield leave", "account", session.legacy.Username, "known", queue != nil)
		return true, legacyConn.WritePacket(legacyworld.CMSGLeaveBattlefield, modernworld.EncodeLegacyBattlefieldLeave(queue))
	case modernworld.CMSGPvpLogData:
		if err := modernworld.ParseEmptyBattlegroundRequest(packet.Body, "pvp log request"); err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("pvp log request", "account", session.legacy.Username)
		return true, legacyConn.WritePacket(uint32(legacyworld.MsgPvpLogData), nil)
	case modernworld.CMSGRequestBattlefieldStatus:
		if err := modernworld.ParseEmptyBattlegroundRequest(packet.Body, "battlefield status request"); err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if legacyConn == nil {
			return true, nil
		}
		s.log.Debug("battlefield status request", "account", session.legacy.Username)
		return true, legacyConn.WritePacket(legacyworld.CMSGBattlefieldStatus, nil)
	case modernworld.CMSGAreaSpiritHealerQuery, modernworld.CMSGAreaSpiritHealerQueue:
		healer, err := modernworld.ParseAreaSpiritHealer(packet.Body)
		if err != nil {
			return true, err
		}
		session.worldMu.Lock()
		legacyGUID, known := session.legacyGUIDForModernLocked(healer)
		legacyConn := session.legacyWorld
		session.worldMu.Unlock()
		if !known {
			return true, fmt.Errorf("area spirit healer GUID is unknown")
		}
		if legacyConn == nil {
			return true, nil
		}
		opcode := legacyworld.CMSGAreaSpiritHealerQuery
		if packet.Opcode == modernworld.CMSGAreaSpiritHealerQueue {
			opcode = legacyworld.CMSGAreaSpiritHealerQueue
		}
		s.log.Debug("area spirit healer", "account", session.legacy.Username, "opcode", packet.Opcode)
		return true, legacyConn.WritePacket(opcode, modernworld.EncodeLegacyAreaSpiritHealer(legacyGUID))
	case modernworld.CMSGGuildSetAchievementTracking,
		modernworld.CMSGViolenceLevel,
		modernworld.CMSGRequestPVPRewards,
		modernworld.CMSGQueryCountdownTimer,
		modernworld.CMSGRequestLFGListBlacklist,
		modernworld.CMSGRequestConquestFormulaConstants,
		modernworld.CMSGOverrideScreenFlash,
		modernworld.CMSGGetItemPurchaseData,
		modernworld.CMSGItemPurchaseRefund,
		modernworld.CMSGAddToy,
		modernworld.CMSGRequestRatedPVPInfo,
		modernworld.CMSGLFGListGetStatus,
		modernworld.CMSGRequestBattlePetJournal,
		modernworld.CMSGCalendarGetNumPending,
		modernworld.CMSGGMTicketGetCaseStatus,
		modernworld.CMSGGetAccountCharacterList,
		modernworld.CMSGBattlePayGetProductList,
		modernworld.CMSGBattlePayGetPurchaseList,
		modernworld.CMSGGetUndeleteCooldownStatus,
		modernworld.CMSGUpdateVASPurchaseStates,
		modernworld.CMSGBattlenetRequest,
		modernworld.CMSGReportEnabledAddons,
		modernworld.CMSGReportClientVariables,
		modernworld.CMSGReportKeybindingExecutionCounts,
		modernworld.CMSGSocialContractRequest,
		modernworld.CMSGQueuedMessagesEnd,
		modernworld.CMSGDiscardedTimeSyncAcks:
		if err := modernworld.ValidateModernOnlyFeatureRequest(packet.Opcode, packet.Body); err != nil {
			return true, err
		}
		s.log.Debug("modern-only feature probe acknowledged", "account", session.legacy.Username, "opcode", packet.Opcode, "bytes", len(packet.Body))
		return true, nil
	default:
		return false, nil
	}
}

func sendWorldGlue(world *modernworld.PacketConn, realmAddress uint32) error {
	hotfixIndex, err := modernworld.EncodeAvailableHotfixes(realmAddress)
	if err != nil {
		return err
	}
	packets := []modernworld.Packet{
		{Opcode: modernworld.SMSGSetTimeZoneInformation, Body: modernworld.EncodeSetTimeZoneInformation("Asia/Shanghai", "Asia/Shanghai")},
		{Opcode: modernworld.SMSGFeatureSystemStatusGlue, Body: modernworld.EncodeFeatureSystemStatusGlue()},
		{Opcode: modernworld.SMSGCacheVersion, Body: modernworld.EncodeLegacyCacheVersion(0)},
		{Opcode: modernworld.SMSGAvailableHotfixes, Body: hotfixIndex},
		{Opcode: modernworld.SMSGBattleNetConnectionStatus, Body: modernworld.EncodeBattleNetConnectionStatus(1, false)},
	}
	for _, packet := range packets {
		if err := world.WritePacket(packet.Opcode, packet.Body); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) relayTranslatedLFG(session *proxySession, label string, opcode uint16, body []byte, translate func([]byte, modernworld.LFGContext) ([]byte, error)) error {
	session.worldMu.Lock()
	modernBody, err := translate(body, session.lfgContextLocked())
	session.worldMu.Unlock()
	if err != nil {
		s.log.Warn(label+" malformed", "account", session.legacy.Username, "error", err)
		return nil
	}
	s.log.Debug(label, "account", session.legacy.Username)
	return session.sendInstance(modernworld.Packet{Opcode: opcode, Body: modernBody})
}

func (s *Server) relayLegacyLFGUpdate(session *proxySession, body []byte, party bool) error {
	session.worldMu.Lock()
	modernBody, parsed, err := modernworld.TranslateLegacyLFGUpdate(body, party, session.lfgContextLocked())
	if err != nil {
		session.worldMu.Unlock()
		s.log.Warn("lfg update malformed", "account", session.legacy.Username, "party", party, "error", err)
		return nil
	}
	next, send := modernworld.NextLFGQueueMode(session.lfgQueueMode, party, parsed.UpdateType, parsed.HasExtra)
	session.lfgQueueMode = next
	session.worldMu.Unlock()
	if !send {
		s.log.Debug("lfg update suppressed", "account", session.legacy.Username, "party", party, "type", parsed.UpdateType)
		return nil
	}
	s.log.Debug("lfg update", "account", session.legacy.Username, "party", party, "type", parsed.UpdateType)
	return session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGLFGUpdateStatus, Body: modernBody})
}

func (s *Server) handleLegacyBattleground(session *proxySession, packet legacyworld.Packet) (bool, error) {
	switch packet.Opcode {
	case legacyworld.SMSGBattlefieldStatus,
		legacyworld.SMSGBattlefieldList,
		legacyworld.MsgPvpLogData,
		legacyworld.MsgBattlegroundPlayerPositions,
		legacyworld.SMSGBattlegroundPlayerJoined,
		legacyworld.SMSGBattlegroundPlayerLeft,
		legacyworld.SMSGAreaSpiritHealerTime,
		legacyworld.SMSGPvpCredit,
		legacyworld.SMSGPlayerSkinned,
		legacyworld.SMSGGroupJoinedBattleground:
	default:
		return false, nil
	}
	session.worldMu.Lock()
	ctx := session.battlegroundContextLocked()
	var packets []modernworld.Packet
	var err error
	var label string
	switch packet.Opcode {
	case legacyworld.SMSGBattlefieldStatus:
		label = "battlefield status"
		packets, err = modernworld.TranslateLegacyBattlefieldStatus(packet.Body, ctx)
	case legacyworld.SMSGBattlefieldList:
		label = "battlefield list"
		var translated modernworld.Packet
		translated, err = modernworld.TranslateLegacyBattlefieldList(packet.Body, session.modernGUIDForLegacyLocked)
		packets = []modernworld.Packet{translated}
	case legacyworld.MsgPvpLogData:
		label = "pvp log"
		var translated modernworld.Packet
		translated, err = modernworld.TranslateLegacyPVPLog(packet.Body, ctx)
		packets = []modernworld.Packet{translated}
	case legacyworld.MsgBattlegroundPlayerPositions:
		label = "battleground positions"
		var translated modernworld.Packet
		translated, err = modernworld.TranslateLegacyBattlegroundPositions(packet.Body, ctx)
		packets = []modernworld.Packet{translated}
	case legacyworld.SMSGBattlegroundPlayerJoined:
		label = "battleground player joined"
		var translated modernworld.Packet
		translated, err = modernworld.TranslateLegacyBattlegroundPlayer(packet.Body, modernworld.SMSGBattlegroundPlayerJoin, session.modernGUIDForLegacyLocked)
		packets = []modernworld.Packet{translated}
	case legacyworld.SMSGBattlegroundPlayerLeft:
		label = "battleground player left"
		var translated modernworld.Packet
		translated, err = modernworld.TranslateLegacyBattlegroundPlayer(packet.Body, modernworld.SMSGBattlegroundPlayerLeft, session.modernGUIDForLegacyLocked)
		packets = []modernworld.Packet{translated}
	case legacyworld.SMSGAreaSpiritHealerTime:
		label = "spirit healer time"
		var translated modernworld.Packet
		translated, err = modernworld.TranslateLegacyAreaSpiritHealerTime(packet.Body, session.modernGUIDForLegacyLocked)
		packets = []modernworld.Packet{translated}
	case legacyworld.SMSGPvpCredit:
		label = "pvp credit"
		var translated modernworld.Packet
		translated, err = modernworld.TranslateLegacyPVPCredit(packet.Body, session.modernGUIDForLegacyLocked)
		packets = []modernworld.Packet{translated}
	case legacyworld.SMSGGroupJoinedBattleground:
		label = "battleground join result"
		packets, err = modernworld.TranslateLegacyGroupJoinedBattleground(packet.Body, ctx)
	case legacyworld.SMSGPlayerSkinned:
		label = "player skinned"
		var translated modernworld.Packet
		translated, err = modernworld.TranslateLegacyPlayerSkinned(packet.Body)
		packets = []modernworld.Packet{translated}
	default:
		session.worldMu.Unlock()
		return false, nil
	}
	session.worldMu.Unlock()
	if err != nil {
		s.log.Warn(label+" malformed", "account", session.legacy.Username, "error", err)
		return true, nil
	}
	if len(packets) == 0 {
		s.log.Debug(label+" suppressed", "account", session.legacy.Username)
		return true, nil
	}
	s.log.Debug(label, "account", session.legacy.Username, "packets", len(packets))
	for _, translated := range packets {
		if err := session.sendInstance(translated); err != nil {
			return true, err
		}
	}
	return true, nil
}

func (s *Server) relayLegacyWorld(session *proxySession, world *modernworld.PacketConn, legacyConn legacyWorldConnection) {
	var deferredLoginPackets []legacyworld.Packet
	var replayPackets []legacyworld.Packet
	for {
		var packet legacyworld.Packet
		if len(replayPackets) != 0 {
			packet = replayPackets[0]
			replayPackets = replayPackets[1:]
		} else {
			var err error
			packet, err = legacyConn.ReadPacket()
			if err != nil {
				if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
					s.log.Debug("legacy world connection ended", "account", session.legacy.Username, "error", err)
				}
				return
			}
		}

		session.worldMu.Lock()
		completionSyncPending := session.completedQuestSyncPending
		completionSyncRequested := session.completedQuestSyncRequested
		if completionSyncPending && !completionSyncRequested {
			session.completedQuestSyncRequested = true
		}
		session.worldMu.Unlock()
		if completionSyncPending {
			if !completionSyncRequested {
				if err := legacyConn.WritePacket(legacyworld.CMSGQueryQuestsCompleted, nil); err != nil {
					s.log.Warn("query completed quests during login failed", "account", session.legacy.Username, "error", err)
					return
				}
			}
			if packet.Opcode != legacyworld.SMSGQueryQuestsCompletedResponse {
				packet.Body = append([]byte(nil), packet.Body...)
				deferredLoginPackets = append(deferredLoginPackets, packet)
				if len(deferredLoginPackets) < maxCompletedQuestLoginPackets {
					continue
				}
				session.worldMu.Lock()
				session.completedQuestSyncPending = false
				session.completedQuestSyncRequested = false
				session.worldMu.Unlock()
				s.log.Warn("completed-quest login sync exceeded packet limit; releasing login packets",
					"account", session.legacy.Username, "packets", len(deferredLoginPackets))
				replayPackets = append(replayPackets, deferredLoginPackets...)
				deferredLoginPackets = nil
				continue
			}

			count, syncErr := s.syncCompletedQuests(session, packet.Body)
			session.worldMu.Lock()
			session.completedQuestSyncPending = false
			session.completedQuestSyncRequested = false
			session.worldMu.Unlock()
			if syncErr != nil {
				s.log.Warn("synchronize completed quests before active player failed", "account", session.legacy.Username, "error", syncErr)
			} else {
				s.log.Debug("completed quests ready before active player", "account", session.legacy.Username,
					"quests", count, "deferred_packets", len(deferredLoginPackets))
			}
			replayPackets = append(replayPackets, deferredLoginPackets...)
			deferredLoginPackets = nil
			continue
		}
		if packet.Opcode == 0x51E {
			moves, err := modernworld.ParseLegacyMultipleMoves(packet.Body)
			if err != nil {
				s.log.Warn("multiple moves translation failed", "error", err)
				continue
			}
			expanded := make([]legacyworld.Packet, 0, len(moves)+len(replayPackets))
			for _, move := range moves {
				expanded = append(expanded, legacyworld.Packet{Opcode: move.Opcode, Body: move.Body})
			}
			replayPackets = append(expanded, replayPackets...)
			continue
		}
		if modernworld.IsLegacyRuntimeNotification(packet.Opcode) {
			session.worldMu.Lock()
			translated, err := modernworld.TranslateLegacyRuntimeNotification(packet.Opcode, packet.Body, session.modernGUIDForLegacyLocked)
			session.worldMu.Unlock()
			if err != nil {
				s.log.Warn("runtime notification translation failed", "opcode", packet.Opcode, "error", err)
				continue
			}
			if err := session.sendInstance(translated); err != nil {
				return
			}
			continue
		}
		if packet.Opcode == 0x1DF || packet.Opcode == 0x278 || packet.Opcode == 0x369 {
			if err := s.handleLegacyRuntimeMisc(session, packet); err != nil {
				s.log.Warn("runtime misc translation failed", "opcode", packet.Opcode, "error", err)
				if !errors.Is(err, errInvalidRuntimeMiscPacket) {
					return
				}
			}
			continue
		}
		if modernworld.IsPetitionServerOpcode(packet.Opcode) {
			if err := s.handleLegacyPetition(session, packet.Opcode, packet.Body); err != nil {
				s.log.Warn("petition translation failed", "opcode", packet.Opcode, "error", err)
				if !errors.Is(err, errInvalidPetitionPacket) {
					return
				}
			}
			continue
		}
		if modernworld.IsGuildServerOpcode(packet.Opcode) {
			if err := s.handleLegacyGuild(session, packet.Opcode, packet.Body); err != nil {
				s.log.Warn("guild translation failed", "opcode", packet.Opcode, "error", err)
				if !errors.Is(err, errInvalidGuildPacket) {
					return
				}
			}
			continue
		}
		if packet.Opcode == modernworld.LegacySMSGCancelVehicleAura {
			if len(packet.Body) != 0 {
				s.log.Warn("cancel vehicle aura has unexpected payload")
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGOnCancelExpectedRideVehicleAura}); err != nil {
				return
			}
			continue
		}
		if packet.Opcode == modernworld.LegacySMSGPlayerVehicleData {
			session.worldMu.Lock()
			body, err := modernworld.TranslatePlayerVehicleData(packet.Body, session.modernGUIDForLegacyLocked)
			session.worldMu.Unlock()
			if err != nil {
				s.log.Warn("vehicle data translation failed", "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSetVehicleRecID, Body: body}); err != nil {
				return
			}
			continue
		}
		if packet.Opcode == modernworld.LegacySMSGMoveKnockBack || packet.Opcode == modernworld.LegacyMSGMoveKnockBack {
			var out modernworld.Packet
			if packet.Opcode == modernworld.LegacySMSGMoveKnockBack {
				k, err := modernworld.ParseLegacyKnockBack(packet.Body)
				if err != nil {
					s.log.Warn("parse knockback failed", "error", err)
					continue
				}
				session.worldMu.Lock()
				guid := session.modernGUIDForLegacyLocked(k.GUID)
				session.worldMu.Unlock()
				out = modernworld.Packet{Opcode: modernworld.SMSGMoveKnockBack, Body: modernworld.EncodeModernKnockBack(k, guid)}
			} else {
				guid, move, err := modernworld.ParseLegacyKnockBackUpdate(packet.Body)
				if err != nil {
					s.log.Warn("parse knockback update failed", "error", err)
					continue
				}
				session.worldMu.Lock()
				mover, transport := session.modernGUIDForLegacyLocked(guid), session.modernGUIDForLegacyLocked(move.TransportGUID)
				if session.objectPositions == nil {
					session.objectPositions = make(map[uint64][3]float32)
				}
				session.objectPositions[guid] = [3]float32{move.X, move.Y, move.Z}
				session.worldMu.Unlock()
				out = modernworld.Packet{Opcode: modernworld.SMSGMoveUpdateKnockBack, Body: modernworld.EncodeMoveUpdate(move, mover, transport)}
			}
			if err := session.sendInstance(out); err != nil {
				return
			}
			continue
		}
		if change, handled, speedErr := modernworld.ParseLegacySpeedChange(packet.Opcode, packet.Body); handled {
			if speedErr != nil {
				s.log.Warn("parse legacy speed-change failed", "account", session.legacy.Username, "opcode", packet.Opcode, "error", speedErr)
				continue
			}
			session.worldMu.Lock()
			mapID := session.currentMapID
			mover, known := session.objectGUIDs[change.GUID]
			session.worldMu.Unlock()
			if !known {
				mover = modernworld.ModernGUIDForLegacy(change.GUID, mapID)
			}
			body, encodeErr := modernworld.EncodeModernSpeedChange(change, mover)
			if encodeErr != nil {
				s.log.Warn("encode modern speed-change failed", "account", session.legacy.Username, "error", encodeErr)
				continue
			}
			s.log.Debug("speed-change", "account", session.legacy.Username, "legacy", packet.Opcode, "modern", change.Modern, "speed", change.Speed)
			if err := session.sendInstance(modernworld.Packet{Opcode: change.Modern, Body: body}); err != nil {
				return
			}
			continue
		}
		if change, handled, flagErr := modernworld.ParseLegacyMovementFlagChange(packet.Opcode, packet.Body); handled {
			if flagErr != nil {
				s.log.Warn("parse legacy movement-flag change failed", "account", session.legacy.Username, "opcode", packet.Opcode, "error", flagErr)
				continue
			}
			session.worldMu.Lock()
			mapID := session.currentMapID
			mover, known := session.objectGUIDs[change.GUID]
			session.worldMu.Unlock()
			if !known {
				mover = modernworld.ModernGUIDForLegacy(change.GUID, mapID)
			}
			body, encodeErr := modernworld.EncodeModernMovementFlagChange(change, mover)
			if encodeErr != nil {
				s.log.Warn("encode modern movement-flag change failed", "account", session.legacy.Username, "error", encodeErr)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: change.Modern, Body: body}); err != nil {
				return
			}
			if play, ok := modernworld.PlayHoverAnimForSplineOpcode(packet.Opcode); ok {
				hoverBody := modernworld.EncodeSetPlayHoverAnim(mover, play)
				if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSetPlayHoverAnim, Body: hoverBody}); err != nil {
					return
				}
			}
			continue
		}
		if packet.Opcode == legacyworld.SMSGMoveSetCollisionHeight {
			change, parseErr := modernworld.ParseLegacyCollisionHeightChange(packet.Body)
			if parseErr != nil {
				s.log.Warn("parse legacy collision-height change failed", "account", session.legacy.Username, "error", parseErr)
				continue
			}
			session.worldMu.Lock()
			mover := session.modernGUIDForLegacyLocked(change.GUID)
			session.worldMu.Unlock()
			body := modernworld.EncodeModernCollisionHeightChange(change, mover)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGMoveSetCollisionHeight, Body: body}); err != nil {
				return
			}
			continue
		}
		if modernworld.IsLegacyPlayerMovementOpcode(packet.Opcode) {
			legacyMover, move, parseErr := modernworld.ParseLegacyPlayerMovementForOpcode(packet.Opcode, packet.Body)
			if parseErr != nil {
				s.log.Warn("parse legacy player movement failed", "account", session.legacy.Username, "opcode", packet.Opcode, "error", parseErr)
				continue
			}
			session.worldMu.Lock()
			mapID := session.currentMapID
			mover, moverKnown := session.objectGUIDs[legacyMover]
			transport, transportKnown := session.objectGUIDs[move.TransportGUID]
			if session.objectPositions == nil {
				session.objectPositions = make(map[uint64][3]float32)
			}
			session.objectPositions[legacyMover] = [3]float32{move.X, move.Y, move.Z}
			session.worldMu.Unlock()
			if !moverKnown {
				mover = modernworld.ModernGUIDForLegacy(legacyMover, mapID)
			}
			if move.TransportGUID != 0 && !transportKnown {
				transport = modernworld.ModernGUIDForLegacy(move.TransportGUID, mapID)
			}
			opcode := modernworld.SMSGMoveUpdate
			body := modernworld.EncodeMoveUpdate(move, mover, transport)
			if speedOpcode, speedBody, isSpeed := modernworld.EncodeMoveUpdateSpeed(packet.Opcode, move, mover, transport); isSpeed {
				opcode, body = speedOpcode, speedBody
			}
			if packet.Opcode == 0x0518 {
				opcode = modernworld.SMSGMoveUpdateCollisionHeight
				body = modernworld.EncodeMoveUpdateCollisionHeight(move, mover, transport)
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: opcode, Body: body}); err != nil {
				return
			}
			continue
		}
		switch packet.Opcode {
		case legacyworld.SMSGDuelRequested:
			request, err := modernworld.ParseLegacyDuelRequested(packet.Body)
			if err != nil {
				s.log.Warn("parse duel-requested failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			arbiter := session.modernGUIDForLegacyLocked(request.Arbiter)
			requestedBy := session.modernGUIDForLegacyLocked(request.RequestedBy)
			session.worldMu.Unlock()
			requestedByAccount := s.socialWowAccountGUID(session, request.RequestedBy)
			body := modernworld.EncodeDuelRequested(arbiter, requestedBy, requestedByAccount)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGDuelRequested, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGDuelCountdown:
			countdown, err := modernworld.ParseLegacyDuelCountdown(packet.Body)
			if err != nil {
				s.log.Warn("parse duel-countdown failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGDuelCountdown,
				Body: modernworld.EncodeDuelCountdown(countdown)}); err != nil {
				return
			}
		case legacyworld.SMSGDuelComplete:
			started, err := modernworld.ParseLegacyDuelComplete(packet.Body)
			if err != nil {
				s.log.Warn("parse duel-complete failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGDuelComplete,
				Body: modernworld.EncodeDuelComplete(started)}); err != nil {
				return
			}
		case legacyworld.SMSGDuelWinner:
			winner, err := modernworld.ParseLegacyDuelWinner(packet.Body)
			if err != nil {
				s.log.Warn("parse duel-winner failed", "account", session.legacy.Username, "error", err)
				continue
			}
			body := modernworld.EncodeDuelWinner(winner, realm.Address(session.selectedRealm.ID))
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGDuelWinner, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGDuelInBounds, legacyworld.SMSGDuelOutOfBounds:
			if err := modernworld.ValidateLegacyDuelBoundary(packet.Body); err != nil {
				s.log.Warn("parse duel-boundary failed", "account", session.legacy.Username, "opcode", packet.Opcode, "error", err)
				continue
			}
			opcode := modernworld.SMSGDuelInBounds
			if packet.Opcode == legacyworld.SMSGDuelOutOfBounds {
				opcode = modernworld.SMSGDuelOutOfBounds
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: opcode}); err != nil {
				return
			}
		case legacyworld.SMSGExplorationExperience:
			experience, err := modernworld.ParseLegacyExplorationExperience(packet.Body)
			if err != nil {
				s.log.Warn("parse exploration-experience failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGExplorationExperience,
				Body: modernworld.EncodeExplorationExperience(experience)}); err != nil {
				return
			}
		case legacyworld.SMSGDismount:
			legacyGUID, err := modernworld.ParseLegacyDismount(packet.Body)
			if err != nil {
				s.log.Warn("parse dismount failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			guid := session.modernGUIDForLegacyLocked(legacyGUID)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGDismount,
				Body: modernworld.EncodeDismount(guid)}); err != nil {
				return
			}
		case legacyworld.SMSGLogoutResponse:
			response, err := modernworld.ParseLegacyLogoutResponse(packet.Body)
			if err != nil {
				s.log.Warn("parse legacy logout response failed", "account", session.legacy.Username, "error", err)
				continue
			}
			s.log.Debug("logout response", "account", session.legacy.Username, "result", response.Result, "instant", response.Instant)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGLogoutResponse, Body: modernworld.EncodeLogoutResponse(response)}); err != nil {
				return
			}
		case legacyworld.SMSGLogoutComplete:
			if len(packet.Body) != 0 {
				s.log.Warn("legacy logout-complete malformed", "account", session.legacy.Username, "bytes", len(packet.Body))
				continue
			}
			// legacy proxy routes LogoutComplete over the realm connection, then closes
			// the no-longer-valid instance connection before character selection.
			if err := world.WritePacket(modernworld.SMSGLogoutComplete, modernworld.EncodeLogoutComplete()); err != nil {
				return
			}
			session.worldMu.Lock()
			instanceWorld := session.instanceWorld
			session.instanceWorld = nil
			session.resetCharacterSessionLocked()
			session.worldMu.Unlock()
			if instanceWorld != nil {
				_ = instanceWorld.Close()
			}
			s.log.Debug("logout complete", "account", session.legacy.Username)
		case legacyworld.SMSGLogoutCancelAck:
			if len(packet.Body) != 0 {
				s.log.Warn("legacy logout-cancel acknowledgment malformed", "account", session.legacy.Username, "bytes", len(packet.Body))
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGLogoutCancelAck, Body: modernworld.EncodeLogoutCancelAck()}); err != nil {
				return
			}
			s.log.Debug("logout cancel acknowledged", "account", session.legacy.Username)
		case legacyworld.SMSGEnumCharactersResult:
			characters, err := modernworld.ParseLegacyCharacterEnum(packet.Body)
			if err != nil {
				s.log.Warn("parse legacy character enumeration failed", "account", session.legacy.Username, "error", err)
				continue
			}
			knownCharacters := make(map[uint64]struct{}, len(characters))
			knownCharacterInfo := make(map[uint64]modernworld.LegacyCharacter, len(characters))
			for _, character := range characters {
				knownCharacters[character.GUID] = struct{}{}
				knownCharacterInfo[character.GUID] = character
			}
			session.worldMu.Lock()
			session.knownCharacters = knownCharacters
			session.knownCharacterInfo = knownCharacterInfo
			for _, character := range characters {
				session.rememberPlayerNameLocked(character.GUID, character.Name)
			}
			internalCreateEnum := session.awaitingCreateEnum
			pendingName := session.pendingCreateName
			pendingCode := session.pendingCreateCode
			if internalCreateEnum {
				session.awaitingCreateEnum = false
				session.pendingCreateName = ""
				session.pendingCreateCode = 0
			}
			session.worldMu.Unlock()
			if internalCreateEnum {
				var createdGUID uint64
				for _, character := range characters {
					if character.FirstLogin && strings.EqualFold(character.Name, pendingName) {
						createdGUID = character.GUID
						break
					}
				}
				body := modernworld.EncodeCreateCharacterResult(pendingCode, createdGUID)
				if err := world.WritePacket(modernworld.SMSGCreateCharacter, body); err != nil {
					return
				}
				continue
			}
			body, err := modernworld.TranslateCharacterEnum(packet.Body, time.Now())
			if err != nil {
				s.log.Warn("translate legacy character enumeration failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := world.WritePacket(modernworld.SMSGEnumCharactersResult, body); err != nil {
				s.log.Debug("send modern character enumeration failed", "account", session.legacy.Username, "error", err)
				return
			}
		case legacyworld.SMSGCreateCharacter:
			if len(packet.Body) != 1 {
				s.log.Warn("legacy create-character response malformed", "account", session.legacy.Username, "bytes", len(packet.Body))
				continue
			}
			legacyCode := packet.Body[0]
			session.worldMu.Lock()
			pendingName := session.pendingCreateName
			if legacyCode == 47 && pendingName != "" {
				session.pendingCreateCode = legacyCode
				session.awaitingCreateEnum = true
				session.worldMu.Unlock()
				if err := legacyConn.WritePacket(legacyworld.CMSGEnumCharacters, nil); err != nil {
					return
				}
				continue
			}
			session.pendingCreateName = ""
			session.pendingCreateCode = 0
			session.awaitingCreateEnum = false
			session.worldMu.Unlock()
			if err := world.WritePacket(modernworld.SMSGCreateCharacter, modernworld.EncodeCreateCharacterResult(legacyCode, 0)); err != nil {
				return
			}
		case legacyworld.SMSGDeleteCharacter:
			if len(packet.Body) != 1 {
				s.log.Warn("legacy delete-character response malformed", "account", session.legacy.Username, "bytes", len(packet.Body))
				continue
			}
			if err := world.WritePacket(modernworld.SMSGDeleteCharacter, modernworld.EncodeDeleteCharacterResult(packet.Body[0])); err != nil {
				return
			}
		case legacyworld.SMSGCharacterRenameResult:
			result, err := modernworld.ParseLegacyCharacterRenameResult(packet.Body)
			if err != nil {
				s.log.Warn("parse character rename result failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			body := modernworld.EncodeCharacterRenameResult(result, session.modernGUIDForLegacyLocked)
			session.worldMu.Unlock()
			if err := world.WritePacket(modernworld.SMSGCharacterRenameResult, body); err != nil {
				return
			}
		case legacyworld.SMSGTitleEarned:
			index, earned, err := modernworld.ParseLegacyTitleEarned(packet.Body)
			if err != nil {
				s.log.Warn("parse title earned failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if !earned {
				// 3.4.3 has no title-lost packet; losing a title only updates the
				// known-titles field, which the active-player delta already covers.
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGTitleEarned, Body: modernworld.EncodeTitleEarned(index)}); err != nil {
				return
			}
		case legacyworld.SMSGResyncRunes:
			state, err := modernworld.ParseLegacyRuneResync(packet.Body)
			if err != nil {
				s.log.Warn("parse rune resync failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			session.runeState = state
			session.hasRuneState = true
			session.runeCooldownStarted = [6]time.Time{}
			created := session.activePlayerCreated
			session.worldMu.Unlock()
			if !created {
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGResyncRunes, Body: modernworld.EncodeRuneResync(state)}); err != nil {
				return
			}
		case legacyworld.SMSGConvertRune:
			slot, runeType, err := modernworld.ParseLegacyRuneConversion(packet.Body)
			if err != nil {
				s.log.Warn("parse rune conversion failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGConvertRune, Body: modernworld.EncodeRuneConversion(slot, runeType)}); err != nil {
				return
			}
		case legacyworld.SMSGAddRunePower:
			power, err := modernworld.ParseLegacyAddRunePower(packet.Body)
			if err != nil {
				s.log.Warn("parse add rune power failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGAddRunePower, Body: modernworld.EncodeAddRunePower(power)}); err != nil {
				return
			}
		case legacyworld.MsgInspectHonorStats:
			honor, err := modernworld.ParseLegacyHonorInspect(packet.Body)
			if err != nil {
				s.log.Warn("parse honor-inspect failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			body := modernworld.EncodeHonorInspectResult(honor, session.modernGUIDForLegacyLocked)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGInspectHonorStats, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGArenaTeamCommandResult, legacyworld.SMSGArenaTeamEvent, legacyworld.SMSGArenaTeamInvite,
			legacyworld.SMSGArenaTeamQueryResponse, legacyworld.SMSGArenaTeamRoster, legacyworld.SMSGArenaTeamStats:
			if err := s.handleLegacyArena(session, packet); err != nil {
				s.log.Warn("translate arena packet failed", "account", session.legacy.Username, "opcode", packet.Opcode, "error", err)
			}
		case legacyworld.MsgInspectArenaTeams:
			legacyTarget, team, err := modernworld.ParseLegacyArenaTeamInspect(packet.Body)
			if err != nil {
				s.log.Warn("parse arena-inspect failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			if session.inspectArenaTeams == nil {
				session.inspectArenaTeams = make(map[uint64][modernworld.ArenaSlotCount]modernworld.ArenaTeamInspect)
			}
			teams := session.inspectArenaTeams[legacyTarget]
			teams[team.Slot] = team
			session.inspectArenaTeams[legacyTarget] = teams
			modernTarget := session.modernGUIDForLegacyLocked(legacyTarget)
			body := modernworld.EncodeInspectPvp(modernTarget, teams)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGInspectPvp, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGAccountDataTimes:
			var accountTimes [modernworld.AccountDataCount]int64
			session.worldMu.Lock()
			playerGUID := session.currentCharacter
			for index, stored := range session.accountData {
				if stored != nil {
					accountTimes[index] = stored.Time
				}
			}
			session.worldMu.Unlock()
			body := modernworld.EncodeAccountDataTimes(playerGUID, time.Now().Unix(), accountTimes)
			if err := session.sendRealm(modernworld.Packet{Opcode: modernworld.SMSGAccountDataTimes, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGLoginVerifyWorld:
			body, err := modernworld.EncodeLoginVerifyWorld(packet.Body)
			if err != nil {
				s.log.Warn("translate legacy login-verify-world failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGLoginVerifyWorld, Body: body}); err != nil {
				s.log.Debug("send modern login-verify-world failed", "account", session.legacy.Username, "error", err)
				return
			}
			// legacy proxy restores the Compact Unit Frame profile immediately
			// after LoginVerifyWorld. Without this packet the roster exists, but
			// the built-in raid frame has no active/shown profile and stays hidden.
			cufBody, cufErr := s.loadCUFProfiles(session)
			if cufErr != nil {
				s.log.Warn("load CUF profiles failed; using defaults", "account", session.legacy.Username, "error", cufErr)
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGLoadCUFProfiles, Body: cufBody}); err != nil {
				s.log.Debug("send CUF profiles failed", "account", session.legacy.Username, "error", err)
				return
			}
			s.log.Debug("CUF profiles sent", "account", session.legacy.Username,
				"opcode", fmt.Sprintf("0x%04x", modernworld.SMSGLoadCUFProfiles),
				"bytes", len(cufBody), "hex", fmt.Sprintf("%x", cufBody))
			session.worldMu.Lock()
			session.currentMapID = uint16(binary.LittleEndian.Uint32(packet.Body[:4]))
			session.mapReady = true
			hasDifficulty := session.hasMapDifficulty
			session.worldMu.Unlock()
			if hasDifficulty {
				if err := session.sendMapDifficulty(); err != nil {
					return
				}
			}
		case legacyworld.SMSGCharacterLoginFailed:
			body, err := modernworld.EncodeCharacterLoginFailed(packet.Body)
			if err != nil {
				s.log.Warn("translate legacy character-login-failed failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := world.WritePacket(modernworld.SMSGCharacterLoginFailed, body); err != nil {
				return
			}
		case legacyworld.SMSGFeatureSystemStatus:
			if err := session.sendRealm(modernworld.Packet{Opcode: modernworld.SMSGFeatureSystemStatus, Body: modernworld.EncodeFeatureSystemStatus()}); err != nil {
				return
			}
		case legacyworld.SMSGMOTD:
			body, err := modernworld.EncodeMOTD(packet.Body)
			if err != nil {
				s.log.Warn("translate legacy MOTD failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendRealm(modernworld.Packet{Opcode: modernworld.SMSGMOTD, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGTutorialFlags:
			if len(packet.Body) != 32 {
				s.log.Warn("legacy tutorial flags malformed", "account", session.legacy.Username, "bytes", len(packet.Body))
				continue
			}
			if err := session.sendRealm(modernworld.Packet{Opcode: modernworld.SMSGTutorialFlags, Body: packet.Body}); err != nil {
				return
			}
		case legacyworld.SMSGTimeSyncRequest:
			if len(packet.Body) != 4 {
				s.log.Warn("legacy time-sync request malformed", "account", session.legacy.Username, "bytes", len(packet.Body))
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGTimeSyncRequest, Body: packet.Body}); err != nil {
				return
			}
		case legacyworld.SMSGBindPointUpdate:
			body, err := modernworld.EncodeBindPointUpdate(packet.Body)
			if err != nil {
				s.log.Warn("translate legacy bind-point update failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGBindPointUpdate, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGPlayerBound:
			if len(packet.Body) != 12 {
				s.log.Warn("legacy player-bound malformed", "account", session.legacy.Username, "bytes", len(packet.Body))
				continue
			}
			legacyBinder := binary.LittleEndian.Uint64(packet.Body)
			session.worldMu.Lock()
			binder := session.modernGUIDForLegacyLocked(legacyBinder)
			session.worldMu.Unlock()
			body, err := modernworld.EncodePlayerBound(packet.Body, binder)
			if err != nil {
				s.log.Warn("translate legacy player-bound failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendRealm(modernworld.Packet{Opcode: modernworld.SMSGPlayerBound, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGBinderConfirm:
			if len(packet.Body) != 8 {
				s.log.Warn("legacy binder-confirm malformed", "account", session.legacy.Username, "bytes", len(packet.Body))
				continue
			}
			legacyBinder := binary.LittleEndian.Uint64(packet.Body)
			session.worldMu.Lock()
			binder := session.modernGUIDForLegacyLocked(legacyBinder)
			session.worldMu.Unlock()
			// Build 54261 replaced SMSG_BINDER_CONFIRM with the generic NPC
			// interaction-open result. Interaction type 20 opens the binder UI.
			s.log.Debug("legacy binder confirm", "account", session.legacy.Username,
				"legacy", fmt.Sprintf("0x%x", legacyBinder))
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGNpcInteractionOpenResult,
				Body: modernworld.EncodeNpcInteraction(binder, modernworld.PlayerInteractionBinder, true)}); err != nil {
				return
			}
		case legacyworld.SMSGShowBank:
			if len(packet.Body) != 8 {
				s.log.Warn("legacy show-bank malformed", "account", session.legacy.Username, "bytes", len(packet.Body))
				continue
			}
			legacyBanker := binary.LittleEndian.Uint64(packet.Body)
			session.worldMu.Lock()
			banker := session.modernGUIDForLegacyLocked(legacyBanker)
			session.worldMu.Unlock()
			// Build 54261 replaced SMSG_SHOW_BANK with the generic NPC
			// interaction-open result. Interaction type 8 opens the bank UI.
			s.log.Debug("legacy show bank", "account", session.legacy.Username,
				"legacy", fmt.Sprintf("0x%x", legacyBanker))
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGNpcInteractionOpenResult,
				Body: modernworld.EncodeNpcInteraction(banker, modernworld.PlayerInteractionBanker, true)}); err != nil {
				return
			}
		case legacyworld.SMSGTaxiNodeStatus:
			legacyGUID, learned, err := modernworld.ParseLegacyTaxiNodeStatus(packet.Body)
			if err != nil {
				s.log.Warn("translate legacy taxi-node-status failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			flightMaster := session.modernGUIDForLegacyLocked(legacyGUID)
			session.worldMu.Unlock()
			if err := session.sendRealm(modernworld.Packet{Opcode: modernworld.SMSGTaxiNodeStatus, Body: modernworld.EncodeTaxiNodeStatus(flightMaster, learned)}); err != nil {
				return
			}
		case legacyworld.SMSGShowTaxiNodes:
			menu, err := modernworld.ParseLegacyTaxiMenu(packet.Body)
			if err != nil {
				s.log.Warn("translate legacy show-taxi-nodes failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			flightMaster := session.modernGUIDForLegacyLocked(menu.FlightMaster)
			session.currentTaxiNode = menu.CurrentNode
			session.usableTaxiNodes = append(session.usableTaxiNodes[:0], menu.NodeMask...)
			session.worldMu.Unlock()
			if err := session.sendRealm(modernworld.Packet{Opcode: modernworld.SMSGShowTaxiNodes, Body: modernworld.EncodeShowTaxiNodes(menu, flightMaster)}); err != nil {
				return
			}
		case legacyworld.SMSGNewTaxiPath:
			if len(packet.Body) != 0 {
				s.log.Warn("legacy new-taxi-path has trailing bytes", "account", session.legacy.Username, "bytes", len(packet.Body))
				continue
			}
			if err := session.sendRealm(modernworld.Packet{Opcode: modernworld.SMSGNewTaxiPath}); err != nil {
				return
			}
		case legacyworld.SMSGActivateTaxiReply:
			reply, err := modernworld.ParseLegacyActivateTaxiReply(packet.Body)
			if err != nil {
				s.log.Warn("translate legacy activate-taxi-reply failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if reply == 0 {
				// Build 54261 must see success after the player taxi spline or it
				// snaps directly to the destination.
				session.worldMu.Lock()
				session.taxiReplyPending = true
				session.worldMu.Unlock()
				continue
			}
			session.worldMu.Lock()
			session.taxiReplyPending = false
			session.worldMu.Unlock()
			if err := session.sendRealm(modernworld.Packet{Opcode: modernworld.SMSGActivateTaxiReply, Body: modernworld.EncodeActivateTaxiReply(reply)}); err != nil {
				return
			}
		case legacyworld.SMSGLoginSetTimeSpeed:
			body, err := modernworld.EncodeLoginSetTimeSpeed(packet.Body)
			if err != nil {
				s.log.Warn("translate legacy login-set-time-speed failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGLoginSetTimeSpeed, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGInitializeFactions:
			body, err := modernworld.EncodeInitializeFactions(packet.Body)
			if err != nil {
				s.log.Warn("translate legacy initialize-factions failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGInitializeFactions, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGSetForcedReactions:
			body, err := modernworld.EncodeSetForcedReactions(packet.Body)
			if err != nil {
				s.log.Warn("translate legacy forced-reactions failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSetForcedReactions, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGPlayedTime:
			body, err := modernworld.EncodePlayedTime(packet.Body)
			if err != nil {
				s.log.Warn("translate legacy played-time failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGPlayedTime, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGInitWorldStates:
			states, body, err := modernworld.TranslateInitWorldStates(packet.Body)
			if err != nil {
				s.log.Warn("translate legacy init-world-states failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			session.currentMapID = uint16(states.MapID)
			session.currentZoneID = states.ZoneID
			session.initWorldStates = append(session.initWorldStates[:0], body...)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGInitWorldStates, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGUpdateActionButtons:
			buttons, reason, err := modernworld.ParseLegacyActionButtons(packet.Body)
			if err != nil {
				s.log.Warn("parse legacy action buttons failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if reason == 2 {
				continue
			}
			session.worldMu.Lock()
			session.actionButtons = append(session.actionButtons[:0], buttons...)
			session.actionButtonReason = reason
			session.hasActionButtons = true
			session.fillMultiCastButtonsLocked()
			buttons = append([]int32(nil), session.actionButtons...)
			created := session.activePlayerCreated
			filled := modernworld.CountNonZeroActionButtons(buttons)
			multicast := modernworld.MultiCastActionPreview(buttons)
			session.worldMu.Unlock()
			s.log.Debug("legacy action buttons", "account", session.legacy.Username, "reason", reason, "filled", filled, "created", created, "slots", modernworld.ActionButtonSlotPreview(buttons, 12), "multicast", multicast)
			if err := sendModernActionButtons(session, buttons); err != nil {
				return
			}
		case legacyworld.SMSGSendKnownSpells:
			known, err := modernworld.ParseLegacyKnownSpells(packet.Body)
			if err != nil {
				s.log.Warn("parse legacy known spells failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			session.knownSpells = append(session.knownSpells[:0], known.Spells...)
			session.spellHistory = append(session.spellHistory[:0], known.History...)
			session.hasKnownSpells = true
			session.fillMultiCastButtonsLocked()
			sendNow := !session.knownSpellsSent
			if sendNow {
				session.knownSpellsSent = true
			}
			session.worldMu.Unlock()
			s.log.Debug("legacy known spells", "account", session.legacy.Username, "spells", len(known.Spells), "history", len(known.History), "initial_login", known.InitialLogin, "send_now", sendNow)
			if sendNow {
				if err := sendModernKnownSpells(session, known.Spells, known.History, known.InitialLogin); err != nil {
					return
				}
			}
		case legacyworld.SMSGSendUnlearnSpells:
			spells, err := modernworld.ParseLegacySendUnlearnSpells(packet.Body)
			if err != nil {
				s.log.Warn("parse legacy send-unlearn-spells failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			session.unlearnSpells = append(session.unlearnSpells[:0], spells...)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSendUnlearnSpells, Body: modernworld.EncodeSendUnlearnSpells(spells)}); err != nil {
				return
			}
		case legacyworld.SMSGLearnedSpell:
			spellID, err := modernworld.ParseLegacyLearnedSpell(packet.Body)
			if err != nil {
				s.log.Warn("parse legacy learned-spell failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			session.knownSpells = appendKnownSpell(session.knownSpells, spellID)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGLearnedSpells, Body: modernworld.EncodeLearnedSpells([]uint32{spellID}, false)}); err != nil {
				return
			}
		case legacyworld.SMSGUnlearnedSpells:
			spellID, err := modernworld.ParseLegacyUnlearnedSpell(packet.Body)
			if err != nil {
				s.log.Warn("parse legacy unlearned-spell failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			session.knownSpells = removeKnownSpell(session.knownSpells, spellID)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGUnlearnedSpells, Body: modernworld.EncodeUnlearnedSpells([]uint32{spellID}, false)}); err != nil {
				return
			}
		case legacyworld.SMSGSupercededSpells:
			if _, _, err := modernworld.ParseLegacySupercededSpells(packet.Body); err != nil {
				s.log.Warn("parse legacy superceded-spells failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.forwardSupercededSpells(packet.Body); err != nil {
				return
			}
		case legacyworld.SMSGSpellCooldown:
			legacyGUID, flags, cooldowns, err := modernworld.ParseLegacySpellCooldown(packet.Body)
			if err != nil {
				s.log.Warn("parse legacy spell-cooldown failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			caster := session.modernGUIDForLegacyLocked(legacyGUID)
			session.worldMu.Unlock()
			s.log.Debug("spell cooldown", "account", session.legacy.Username, "flags", flags, "count", len(cooldowns), "spells", cooldowns)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSpellCooldown, Body: modernworld.EncodeSpellCooldown(caster, flags, cooldowns)}); err != nil {
				return
			}
		case legacyworld.SMSGCooldownEvent:
			spellID, legacyGUID, err := modernworld.ParseLegacyCooldownSpellAndGUID(packet.Body)
			if err != nil {
				s.log.Warn("parse legacy cooldown-event failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGCooldownEvent, Body: modernworld.EncodeCooldownEvent(spellID, modernworld.LegacyGUIDIsPet(legacyGUID))}); err != nil {
				return
			}
		case legacyworld.SMSGClearCooldown:
			spellID, legacyGUID, err := modernworld.ParseLegacyCooldownSpellAndGUID(packet.Body)
			if err != nil {
				s.log.Warn("parse legacy clear-cooldown failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGClearCooldown, Body: modernworld.EncodeClearCooldown(spellID, modernworld.LegacyGUIDIsPet(legacyGUID))}); err != nil {
				return
			}
		case legacyworld.SMSGDestroyObject:
			if len(packet.Body) < 8 {
				s.log.Warn("legacy destroy-object is truncated", "account", session.legacy.Username, "bytes", len(packet.Body))
				continue
			}
			legacyGUID := binary.LittleEndian.Uint64(packet.Body)
			session.worldMu.Lock()
			mapID := session.currentMapID
			modernGUID, known := session.objectGUIDs[legacyGUID]
			delete(session.objectGUIDs, legacyGUID)
			delete(session.objectTypes, legacyGUID)
			delete(session.objectFields, legacyGUID)
			delete(session.objectPositions, legacyGUID)
			session.worldMu.Unlock()
			if !known {
				modernGUID = modernworld.ModernGUIDForLegacy(legacyGUID, mapID)
			}
			body := modernworld.EncodeObjectRemovals(mapID, []modernworld.GUID128{modernGUID}, nil)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGUpdateObject, Body: body}); err != nil {
				return
			}
			if err := session.sendCurrencyUpdate(); err != nil {
				return
			}
		case legacyworld.SMSGOnMonsterMove, legacyworld.SMSGMonsterMoveTransport:
			move, err := modernworld.ParseLegacyMonsterMove(packet.Body, packet.Opcode == legacyworld.SMSGMonsterMoveTransport)
			if err != nil {
				s.log.Warn("parse legacy monster move failed", "account", session.legacy.Username, "opcode", packet.Opcode, "error", err)
				continue
			}
			session.worldMu.Lock()
			mapID := session.currentMapID
			currentCharacter := session.currentCharacter
			taxiReplyPending := session.taxiReplyPending
			taxiStart := move.MoverGUID == currentCharacter && modernworld.IsLegacyTaxiFlight(move) &&
				(taxiReplyPending || time.Now().Before(session.taxiLoginGraceUntil))
			if taxiStart {
				session.taxiLoginGraceUntil = time.Time{}
				move.TaxiStart = true
			}
			move.Swimming = modernworld.LegacyUnitIsSwimming(session.objectFields[move.MoverGUID])
			mover, moverKnown := session.objectGUIDs[move.MoverGUID]
			transport, transportKnown := session.objectGUIDs[move.TransportGUID]
			facingTarget, facingKnown := session.objectGUIDs[move.FacingTarget]
			if session.objectPositions == nil {
				session.objectPositions = make(map[uint64][3]float32)
			}
			session.objectPositions[move.MoverGUID] = [3]float32{move.StartX, move.StartY, move.StartZ}
			session.worldMu.Unlock()
			if !moverKnown {
				mover = modernworld.ModernGUIDForLegacy(move.MoverGUID, mapID)
			}
			if move.TransportGUID != 0 && !transportKnown {
				transport = modernworld.ModernGUIDForLegacy(move.TransportGUID, mapID)
			}
			if move.FacingTarget != 0 && !facingKnown {
				facingTarget = modernworld.ModernGUIDForLegacy(move.FacingTarget, mapID)
			}
			if taxiStart {
				stop := move
				stop.TaxiStart = false
				stop.SplineType = 1
				stop.Flags = 0
				stop.Duration = 0
				stop.Points = nil
				stop.PackedDeltas = nil
				stop.End = [3]float32{}
				if stop.SplineID >= 2 {
					stop.SplineID -= 2
				}
				for index := 0; index < 2; index++ {
					stopBody, encodeErr := modernworld.EncodeMonsterMove(stop, mover, transport, facingTarget)
					if encodeErr != nil {
						s.log.Warn("encode taxi pre-spline failed", "account", session.legacy.Username, "error", encodeErr)
						break
					}
					if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGOnMonsterMove, Body: stopBody}); err != nil {
						return
					}
					if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGControlUpdate, Body: modernworld.EncodeControlUpdate(mover, false)}); err != nil {
						return
					}
					stop.SplineID++
				}
			}
			body, err := modernworld.EncodeMonsterMove(move, mover, transport, facingTarget)
			if err != nil {
				s.log.Warn("translate legacy monster move failed", "account", session.legacy.Username, "guid", move.MoverGUID, "error", err)
				continue
			}
			if move.FacingTarget != 0 {
				s.log.Debug("monster facing-target move",
					"account", session.legacy.Username,
					"legacy_mover", fmt.Sprintf("0x%x", move.MoverGUID),
					"legacy_target", fmt.Sprintf("0x%x", move.FacingTarget),
					"modern_mover", fmt.Sprintf("%016x:%016x", mover.High, mover.Low),
					"modern_target", fmt.Sprintf("%016x:%016x", facingTarget.High, facingTarget.Low),
					"spline_id", move.SplineID,
					"flags", fmt.Sprintf("0x%x", move.Flags),
					"duration", move.Duration,
					"points", len(move.Points),
					"packed_deltas", len(move.PackedDeltas),
					"bytes", len(body))
			}
			if move.HasJumpExtra() {
				s.log.Debug("monster trajectory move",
					"account", session.legacy.Username,
					"legacy_mover", fmt.Sprintf("0x%x", move.MoverGUID),
					"transport", fmt.Sprintf("0x%x", move.TransportGUID),
					"gravity", move.JumpGravity,
					"start_time", move.JumpStartTime,
					"duration", move.Duration,
					"end", fmt.Sprintf("%g,%g,%g", move.End[0], move.End[1], move.End[2]),
					"flags", fmt.Sprintf("0x%x", move.Flags),
					"bytes", len(body))
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGOnMonsterMove, Body: body}); err != nil {
				return
			}
			if taxiStart && taxiReplyPending {
				session.worldMu.Lock()
				session.taxiReplyPending = false
				session.worldMu.Unlock()
				if err := session.sendRealm(modernworld.Packet{Opcode: modernworld.SMSGActivateTaxiReply, Body: modernworld.EncodeActivateTaxiReply(0)}); err != nil {
					return
				}
			}
		case legacyworld.SMSGControlUpdate:
			legacyGUID, hasControl, err := modernworld.ParseLegacyControlUpdate(packet.Body)
			if err != nil {
				s.log.Warn("parse legacy control update failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			mapID := session.currentMapID
			modernGUID, known := session.objectGUIDs[legacyGUID]
			session.worldMu.Unlock()
			if !known {
				modernGUID = modernworld.ModernGUIDForLegacy(legacyGUID, mapID)
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGControlUpdate, Body: modernworld.EncodeControlUpdate(modernGUID, hasControl)}); err != nil {
				return
			}
			if hasControl {
				if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGMoveSetActiveMover, Body: modernworld.EncodeMoveSetActiveMover(modernGUID)}); err != nil {
					return
				}
			}
		case legacyworld.SMSGPowerUpdate:
			legacyGUID, power, err := modernworld.ParseLegacyPowerUpdate(packet.Body)
			if err != nil {
				s.log.Warn("parse legacy power update failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			mapID := session.currentMapID
			modernGUID, known := session.objectGUIDs[legacyGUID]
			session.worldMu.Unlock()
			if !known {
				modernGUID = modernworld.ModernGUIDForLegacy(legacyGUID, mapID)
			}
			body, err := modernworld.EncodePowerUpdate(modernGUID, []modernworld.PowerValue{power})
			if err != nil {
				s.log.Warn("encode modern power update failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendRealm(modernworld.Packet{Opcode: modernworld.SMSGPowerUpdate, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGHealthUpdate:
			legacyGUID, health, err := modernworld.ParseLegacyHealthUpdate(packet.Body)
			if err != nil {
				s.log.Warn("parse legacy health update failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			mapID := session.currentMapID
			modernGUID, known := session.objectGUIDs[legacyGUID]
			session.worldMu.Unlock()
			if !known {
				modernGUID = modernworld.ModernGUIDForLegacy(legacyGUID, mapID)
			}
			body, err := modernworld.EncodeHealthUpdate(modernGUID, health)
			if err != nil {
				s.log.Warn("encode modern health update failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGHealthUpdate, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGUpdateWorldState:
			body, err := modernworld.TranslateUpdateWorldState(packet.Body)
			if err != nil {
				s.log.Warn("translate update-world-state failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGUpdateWorldState, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGQueryTimeResponse:
			body, err := modernworld.TranslateQueryTimeResponse(packet.Body)
			if err != nil {
				s.log.Warn("translate query-time response failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGQueryTime, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGNextMailTime:
			result, err := modernworld.ParseLegacyNextMailTime(packet.Body)
			if err != nil {
				s.log.Warn("parse next-mail-time failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			body := modernworld.EncodeNextMailTime(result, session.modernGUIDForLegacyLocked)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGMailQueryNextTimeResult, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGReadItemResultOK, legacyworld.SMSGReadItemResultFailed:
			itemGUID, err := modernworld.ParseLegacyReadItemResult(packet.Body)
			if err != nil {
				s.log.Warn("parse read-item result failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			item := session.modernGUIDForLegacyLocked(itemGUID)
			session.worldMu.Unlock()
			var body []byte
			if packet.Opcode == legacyworld.SMSGReadItemResultOK {
				body = modernworld.EncodeReadItemResultOK(item)
			} else {
				body = modernworld.EncodeReadItemResultFailed(item)
			}
			opcode := modernworld.SMSGReadItemResultOK
			if packet.Opcode == legacyworld.SMSGReadItemResultFailed {
				opcode = modernworld.SMSGReadItemResultFailed
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: opcode, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGQueryPageTextResponse:
			page, err := modernworld.ParseLegacyPageText(packet.Body)
			if err != nil {
				s.log.Warn("parse page-text failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGQueryPageTextResponse, Body: modernworld.EncodePageTextResponse(page)}); err != nil {
				return
			}
		case legacyworld.SMSGMailListResult:
			result, err := modernworld.ParseLegacyMailListResult(packet.Body)
			if err != nil {
				s.log.Warn("parse mail list failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			body := modernworld.EncodeMailListResult(result, session.modernGUIDForLegacyLocked)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGMailListResult, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGMailCommandResult:
			result, err := modernworld.ParseLegacyMailCommandResult(packet.Body)
			if err != nil {
				s.log.Warn("parse mail command result failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGMailCommandResult, Body: modernworld.EncodeMailCommandResult(result)}); err != nil {
				return
			}
		case legacyworld.SMSGNotifyReceivedMail:
			delay, err := modernworld.ParseLegacyNotifyMail(packet.Body)
			if err != nil {
				s.log.Warn("parse notify-mail failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGNotifyReceivedMail, Body: modernworld.EncodeNotifyMail(delay)}); err != nil {
				return
			}
		case legacyworld.MsgAuctionHello:
			auctioneer, open, err := modernworld.ParseLegacyAuctionHello(packet.Body)
			if err != nil {
				s.log.Warn("parse auction hello failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			modernAuctioneer := session.modernGUIDForLegacyLocked(auctioneer)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGNpcInteractionOpenResult, Body: modernworld.EncodeNpcInteraction(modernAuctioneer, modernworld.PlayerInteractionAuctioneer, open)}); err != nil {
				return
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGAuctionHelloResponse, Body: modernworld.EncodeAuctionHelloResponse(modernAuctioneer, open)}); err != nil {
				return
			}
			// The modern client expects the Auctions tab pre-populated once the
			// frame opens; HermesProxy answers its own hello with a legacy owned
			// listing query, so mirror that.
			if err := legacyConn.WritePacket(legacyworld.CMSGAuctionListOwnedItems, modernworld.EncodeLegacyAuctionOffsetQuery(auctioneer, 0)); err != nil {
				return
			}
		case legacyworld.SMSGAuctionCommandResult:
			result, err := modernworld.ParseLegacyAuctionCommandResult(packet.Body)
			if err != nil {
				s.log.Warn("parse auction command result failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			body := modernworld.EncodeAuctionCommandResult(result, session.modernGUIDForLegacyLocked)
			session.worldMu.Unlock()
			s.log.Debug("auction command result", "account", session.legacy.Username, "auction", result.AuctionID, "command", result.Command, "error", result.ErrorCode)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGAuctionCommandResult, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGAuctionBidderNotification:
			notification, err := modernworld.ParseLegacyAuctionBidderNotification(packet.Body)
			if err != nil {
				s.log.Warn("parse auction bidder notification failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			var body []byte
			opcode := modernworld.SMSGAuctionOutbidNotification
			if notification.BidAmount == 0 {
				opcode = modernworld.SMSGAuctionWonNotification
				body = modernworld.EncodeAuctionWonNotification(notification, session.modernGUIDForLegacyLocked)
			} else {
				body = modernworld.EncodeAuctionOutbidNotification(notification, session.modernGUIDForLegacyLocked)
			}
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: opcode, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGAuctionOwnerNotification:
			notification, err := modernworld.ParseLegacyAuctionOwnerNotification(packet.Body)
			if err != nil {
				s.log.Warn("parse auction owner notification failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			var body []byte
			opcode := modernworld.SMSGAuctionOwnerBidNotification
			if notification.Buyer == 0 {
				opcode = modernworld.SMSGAuctionClosedNotification
				body = modernworld.EncodeAuctionClosedNotification(notification)
			} else {
				body = modernworld.EncodeAuctionOwnerBidNotification(notification, session.modernGUIDForLegacyLocked)
			}
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: opcode, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGAuctionListItemsResult:
			result, err := modernworld.ParseLegacyAuctionListResult(packet.Body)
			if err != nil {
				s.log.Warn("parse auction list result failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			body := modernworld.EncodeAuctionBrowseResult(result, session.modernGUIDForLegacyLocked)
			session.worldMu.Unlock()
			s.log.Debug("auction list result", "account", session.legacy.Username, "items", len(result.Items),
				"total", result.TotalItemsCount, "more", result.HasMoreResults, "bytes", len(body))
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGAuctionListItemsResult, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGAuctionListOwnedItemsResult:
			result, err := modernworld.ParseLegacyAuctionListResult(packet.Body)
			if err != nil {
				s.log.Warn("parse auction owned result failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			body := modernworld.EncodeAuctionMyItemsResult(result, session.modernGUIDForLegacyLocked)
			session.worldMu.Unlock()
			s.log.Debug("auction owned result", "account", session.legacy.Username, "items", len(result.Items), "bytes", len(body))
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGAuctionListOwnedItemsResult, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGAuctionListBidderItemsResult:
			result, err := modernworld.ParseLegacyAuctionListResult(packet.Body)
			if err != nil {
				s.log.Warn("parse auction bidder result failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			body := modernworld.EncodeAuctionMyItemsResult(result, session.modernGUIDForLegacyLocked)
			session.worldMu.Unlock()
			s.log.Debug("auction bidder result", "account", session.legacy.Username, "items", len(result.Items), "bytes", len(body))
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGAuctionListBiddedItemsResult, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGRaidInstanceInfo:
			locks, err := modernworld.ParseLegacyInstanceInfo(packet.Body)
			if err != nil {
				s.log.Warn("parse raid-instance-info failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGInstanceInfo, Body: modernworld.EncodeInstanceInfo(locks)}); err != nil {
				return
			}
		case legacyworld.SMSGInstanceReset:
			mapID, err := modernworld.ParseLegacyInstanceReset(packet.Body)
			if err != nil {
				s.log.Warn("parse instance-reset failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGInstanceReset, Body: modernworld.EncodeInstanceReset(mapID)}); err != nil {
				return
			}
		case legacyworld.SMSGInstanceResetFailed:
			failure, err := modernworld.ParseLegacyInstanceResetFailed(packet.Body)
			if err != nil {
				s.log.Warn("parse instance-reset-failed failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGInstanceResetFailed, Body: modernworld.EncodeInstanceResetFailed(failure)}); err != nil {
				return
			}
		case legacyworld.SMSGResetFailedNotify:
			mapID, err := modernworld.ParseLegacyResetFailedNotify(packet.Body)
			if err != nil {
				s.log.Warn("parse reset-failed-notify failed", "account", session.legacy.Username, "error", err)
				continue
			}
			s.log.Debug("instance reset failed notification", "account", session.legacy.Username, "map", mapID)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGResetFailedNotify}); err != nil {
				return
			}
		case legacyworld.SMSGSummonRequest:
			legacySummoner, areaID, err := modernworld.ParseLegacySummonRequest(packet.Body)
			if err != nil {
				s.log.Warn("parse summon-request failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			summoner := session.modernGUIDForLegacyLocked(legacySummoner)
			realmAddress := realm.Address(session.selectedRealm.ID)
			session.worldMu.Unlock()
			s.log.Debug("summon request", "account", session.legacy.Username, "summoner", fmt.Sprintf("0x%x", legacySummoner), "area", areaID)
			body := modernworld.EncodeSummonRequest(summoner, realmAddress, areaID)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSummonRequest, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGUpdateInstanceOwnership:
			body, err := modernworld.TranslateInstanceOwnership(packet.Body)
			if err != nil {
				s.log.Warn("translate instance-ownership failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGUpdateInstanceOwnership, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGContactList:
			list, err := modernworld.ParseLegacyContactList(packet.Body)
			if err != nil {
				s.log.Warn("parse contact-list failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			realmAddress := realm.Address(session.selectedRealm.ID)
			mapID := session.currentMapID
			session.worldMu.Unlock()
			body := modernworld.EncodeContactList(list, realmAddress,
				func(guid uint64) modernworld.GUID128 { return modernworld.ModernGUIDForLegacy(guid, mapID) },
				func(guid uint64) modernworld.GUID128 { return s.socialWowAccountGUID(session, guid) })
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGContactList, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGFriendStatus:
			status, err := modernworld.ParseLegacyFriendStatus(packet.Body)
			if err != nil {
				s.log.Warn("translate friend status failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			realmAddress := realm.Address(session.selectedRealm.ID)
			mapID := session.currentMapID
			session.worldMu.Unlock()
			body := modernworld.EncodeFriendStatus(status, realmAddress,
				func(guid uint64) modernworld.GUID128 { return modernworld.ModernGUIDForLegacy(guid, mapID) },
				func(guid uint64) modernworld.GUID128 { return s.socialWowAccountGUID(session, guid) })
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGFriendStatus, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGWho:
			entries, err := modernworld.ParseLegacyWhoResponse(packet.Body)
			if err != nil {
				s.log.Warn("translate online-player search failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			requestID := session.lastWhoRequestID
			realmAddress := realm.Address(session.selectedRealm.ID)
			session.worldMu.Unlock()
			body := modernworld.EncodeWhoResponse(entries, requestID, realmAddress)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGWho, Body: body}); err != nil {
				return
			}
			s.log.Debug("online-player search result", "account", session.legacy.Username, "request_id", requestID, "players", len(entries))
		case legacyworld.SMSGPartyInvite:
			invite, err := modernworld.ParseLegacyPartyInvite(packet.Body)
			if err != nil {
				s.log.Warn("translate party invite failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			realmAddress := realm.Address(session.selectedRealm.ID)
			realmName := session.selectedRealm.Name
			session.worldMu.Unlock()
			body := modernworld.EncodePartyInvite(invite, realmAddress, realmName)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGPartyInvite, Body: body}); err != nil {
				return
			}
			s.log.Debug("party invite received", "account", session.legacy.Username, "inviter", invite.InviterName, "can_accept", invite.CanAccept)
		case legacyworld.SMSGGroupList:
			groupType := byte(0)
			if len(packet.Body) > 0 {
				groupType = packet.Body[0]
			}
			session.worldMu.Lock()
			partyWasActive := session.partyLegacyGUID != 0
			legacyConnForNames := session.legacyWorld
			character := session.knownCharacterInfo[session.currentCharacter]
			snapshot, snapshotErr := modernworld.ParseLegacyPartySnapshot(packet.Body, session.currentCharacter, character.Name)
			self := modernworld.PartyPlayer{
				GUID:         session.modernGUIDForLegacyLocked(session.currentCharacter),
				Name:         character.Name,
				ClassID:      character.Class,
				FactionGroup: modernworld.FactionGroupForRace(character.Race),
			}
			sequence := session.partySequence
			session.partySequence++
			resolvePartyPlayer := func(legacyGUID uint64) modernworld.PartyPlayer {
				classID, factionGroup := modernworld.LegacyPartyPlayerClassAndFaction(session.objectFields[legacyGUID])
				source := "objectFields"
				if classID == 0 {
					identity := session.playerIdentities[legacyGUID]
					classID = identity.Class
					factionGroup = modernworld.FactionGroupForRace(identity.Race)
					source = "identity"
				}
				if classID == 0 {
					if character, known := session.knownCharacterInfo[legacyGUID]; known {
						classID = character.Class
						factionGroup = modernworld.FactionGroupForRace(character.Race)
						source = "characterEnum"
					}
				}
				s.log.Debug("resolve party member class", "account", session.legacy.Username,
					"member", fmt.Sprintf("0x%x", legacyGUID), "class", classID, "faction", factionGroup, "source", source)
				return modernworld.PartyPlayer{
					GUID:         session.modernGUIDForLegacyLocked(legacyGUID),
					ClassID:      classID,
					FactionGroup: factionGroup,
				}
			}
			session.worldMu.Unlock()
			s.log.Debug("group list received", "account", session.legacy.Username,
				"group_type", groupType, "bytes", len(packet.Body), "hex", fmt.Sprintf("%x", packet.Body))
			session.worldMu.Lock()
			body, rosterOrder, err := modernworld.TranslateLegacyGroupListWithOrder(packet.Body, self, session.currentCharacter, sequence, resolvePartyPlayer,
				func(group, leader uint64, members []uint64) []uint64 {
					return s.stablePartyOrder(partyOrderKey{realm: session.selectedRealm.ID, group: group}, leader, members)
				})
			var partyNameQueries []uint64
			type partyNameSeed struct {
				identity modernworld.LegacyNameIdentity
				level    byte
			}
			var partyNameSeeds []partyNameSeed
			if err == nil && snapshotErr == nil {
				if snapshot.Destroyed {
					session.partyLegacyGUID = 0
					session.partyGUID = modernworld.GUID128{}
					session.partyIndex = 0
					session.partyMembers = nil
					session.partyRoles = nil
					session.partyLootMethod = 0
					session.partyLootMaster = 0
					session.partyLootThreshold = 2
				} else {
					session.partyLegacyGUID = snapshot.LegacyGUID
					session.partyGUID = snapshot.GUID
					session.partyIndex = snapshot.PartyIndex
					session.partyLootMethod = snapshot.LootMethod
					session.partyLootMaster = snapshot.LootMaster
					session.partyLootThreshold = snapshot.LootThreshold
					session.partyMembers = make(map[uint64]modernworld.LegacyPartyMember, len(snapshot.Members))
					session.partyRoles = make(map[uint64]byte, len(snapshot.Members))
					for _, member := range snapshot.Members {
						session.partyMembers[member.GUID] = member
						session.partyRoles[member.GUID] = member.Roles
						session.rememberPlayerNameLocked(member.GUID, member.Name)
						identity, identityKnown := session.playerIdentities[member.GUID]
						level := byte(1)
						if character, known := session.knownCharacterInfo[member.GUID]; known {
							identity = modernworld.LegacyNameIdentity{
								GUID:  character.GUID,
								Name:  character.Name,
								Race:  character.Race,
								Sex:   character.Sex,
								Class: character.Class,
							}
							level = character.Level
							identityKnown = true
						}
						if identityKnown && identity.GUID != 0 && identity.Name != "" {
							partyNameSeeds = append(partyNameSeeds, partyNameSeed{identity: identity, level: level})
						}
						if legacyConnForNames != nil && member.GUID != session.currentCharacter && session.queuePlayerNameQueryLocked(member.GUID) {
							partyNameQueries = append(partyNameQueries, member.GUID)
						}
					}
				}
			}
			session.worldMu.Unlock()

			if err != nil {
				s.log.Warn("translate group list failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if snapshotErr != nil {
				s.log.Warn("cache group state failed", "account", session.legacy.Username, "error", snapshotErr)
			}
			// legacy proxy's player cache is already populated when PartyUpdate reaches the
			// client. Seed the equivalent client-side cache first: if the roster is
			// parsed while the local player's lookup record is missing, build 54261
			// keeps a nameless leader row and rejects name-based loot-master targets
			// even when the lookup response arrives immediately afterwards.
			for _, seed := range partyNameSeeds {
				session.worldMu.Lock()
				realmAddress := realm.Address(session.selectedRealm.ID)
				session.worldMu.Unlock()
				nameBody, encodeErr := modernworld.EncodeQueryPlayerNameIdentity(seed.identity, seed.level, realmAddress,
					s.socialWowAccountGUID(session, seed.identity.GUID), s.socialBNetAccountGUID(session, seed.identity.GUID))
				if encodeErr != nil {
					s.log.Warn("encode party-member identity failed", "account", session.legacy.Username, "error", encodeErr)
					continue
				}
				if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGQueryPlayerNames, Body: nameBody}); err != nil {
					return
				}
				s.log.Debug("party-member identity seeded", "account", session.legacy.Username,
					"member", fmt.Sprintf("0x%x", seed.identity.GUID), "name", seed.identity.Name,
					"race", seed.identity.Race, "class", seed.identity.Class, "level", seed.level)
			}
			if snapshotErr == nil && snapshot.Destroyed && partyWasActive {
				// Build 54261 expects GroupUninvite before the empty PartyUpdate when
				// the local player leaves. GroupDestroyed alone leaves the portrait's
				// cached leader badge and compact raid-frame state behind.
				if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGGroupUninvite}); err != nil {
					return
				}
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGPartyUpdate, Body: body}); err != nil {
				return
			}
			s.log.Debug("party update sent", "account", session.legacy.Username,
				"order", rosterOrder, "bytes", len(body), "hex", fmt.Sprintf("%x", body))
			// Populate the modern client name cache after the roster is visible.
			// The legacy response contains authoritative race/sex/class data, so do
			// not synthesize a partial modern identity from the roster name alone.
			for _, legacyGUID := range partyNameQueries {
				if legacyConnForNames == nil {
					break
				}
				if err := legacyConnForNames.WritePacket(legacyworld.CMSGNameQuery, modernworld.EncodeLegacyNameQuery(legacyGUID)); err != nil {
					return
				}
				s.log.Debug("request party-member identity", "account", session.legacy.Username,
					"member", fmt.Sprintf("0x%x", legacyGUID))
			}
		case legacyworld.SMSGPartyMemberPartialState:
			state, err := modernworld.ParseLegacyPartyMemberPartialState(packet.Body)
			if err != nil {
				s.log.Warn("translate party-member partial state failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			state.MemberGUID = session.modernGUIDForLegacyLocked(state.LegacyGUID)
			if state.Pet != nil && state.Pet.LegacyGUID != nil {
				guid := session.modernGUIDForLegacyLocked(*state.Pet.LegacyGUID)
				state.Pet.GUID = &guid
			}
			session.worldMu.Unlock()
			body := modernworld.EncodePartyMemberPartialState(state)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGPartyMemberPartialState, Body: body}); err != nil {
				return
			}
			s.log.Debug("party-member partial state sent", "account", session.legacy.Username,
				"member", fmt.Sprintf("0x%x", state.MemberGUID.Low), "bytes", len(body), "hex", fmt.Sprintf("%x", body))
		case legacyworld.SMSGPartyMemberFullState:
			state, err := modernworld.ParseLegacyPartyMemberFullState(packet.Body)
			if err != nil {
				s.log.Warn("translate party-member full state failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			state.Member.MemberGUID = session.modernGUIDForLegacyLocked(state.Member.LegacyGUID)
			if state.Member.Pet != nil && state.Member.Pet.LegacyGUID != nil {
				guid := session.modernGUIDForLegacyLocked(*state.Member.Pet.LegacyGUID)
				state.Member.Pet.GUID = &guid
			}
			session.worldMu.Unlock()
			body := modernworld.EncodePartyMemberFullState(state, [2]byte{1, 0})
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGPartyMemberFullState, Body: body}); err != nil {
				return
			}
			s.log.Debug("party-member full state sent", "account", session.legacy.Username,
				"member", fmt.Sprintf("0x%x", state.Member.MemberGUID.Low), "bytes", len(body), "hex", fmt.Sprintf("%x", body))
		case uint16(legacyworld.MSGRaidReadyCheck):
			initiatorLegacy, err := modernworld.ParseLegacyReadyCheckStarted(packet.Body)
			if err != nil {
				s.log.Warn("translate ready-check start failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			partyIndex := session.partyIndex
			partyGUID := session.partyGUID
			initiator := session.modernGUIDForLegacyLocked(initiatorLegacy)
			session.worldMu.Unlock()
			body := modernworld.EncodeReadyCheckStarted(partyIndex, partyGUID, initiator)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGReadyCheckStarted, Body: body}); err != nil {
				return
			}
		case uint16(legacyworld.MSGRaidReadyCheckConfirm):
			playerLegacy, ready, err := modernworld.ParseLegacyReadyCheckResponse(packet.Body)
			if err != nil {
				s.log.Warn("translate ready-check response failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			partyGUID := session.partyGUID
			player := session.modernGUIDForLegacyLocked(playerLegacy)
			session.worldMu.Unlock()
			body := modernworld.EncodeReadyCheckResponse(partyGUID, player, ready)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGReadyCheckResponse, Body: body}); err != nil {
				return
			}
		case uint16(legacyworld.MSGRaidReadyCheckFinished):
			if len(packet.Body) != 0 {
				s.log.Warn("legacy ready-check completion malformed", "account", session.legacy.Username, "bytes", len(packet.Body))
				continue
			}
			session.worldMu.Lock()
			session.readyCheckGeneration++
			partyIndex := session.partyIndex
			partyGUID := session.partyGUID
			session.worldMu.Unlock()
			body := modernworld.EncodeReadyCheckCompleted(partyIndex, partyGUID)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGReadyCheckCompleted, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGGroupSetLeader:
			session.worldMu.Lock()
			partyIndex := session.partyIndex
			session.worldMu.Unlock()
			body, err := modernworld.TranslateLegacyGroupNewLeader(packet.Body, partyIndex)
			if err != nil {
				s.log.Warn("translate group new leader failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGGroupNewLeader, Body: body}); err != nil {
				return
			}
		case uint16(legacyworld.MSGMinimapPing):
			session.worldMu.Lock()
			body, err := modernworld.TranslateLegacyMinimapPing(packet.Body, session.modernGUIDForLegacyLocked)
			session.worldMu.Unlock()
			if err != nil {
				s.log.Warn("translate minimap ping failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGMinimapPing, Body: body}); err != nil {
				return
			}
		case uint16(legacyworld.MSGRandomRoll):
			session.worldMu.Lock()
			body, err := modernworld.TranslateLegacyRandomRoll(packet.Body, session.modernGUIDForLegacyLocked)
			session.worldMu.Unlock()
			if err != nil {
				s.log.Warn("translate random roll failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGRandomRoll, Body: body}); err != nil {
				return
			}
		case legacyworld.MSGRaidTargetUpdate:
			update, err := modernworld.ParseLegacyRaidTargetUpdate(packet.Body)
			if err != nil {
				s.log.Warn("translate raid-target update failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if update.All {
				targets := make([]modernworld.RaidTarget, len(update.Targets))
				session.worldMu.Lock()
				for index, target := range update.Targets {
					targets[index] = modernworld.RaidTarget{
						Symbol: target.Symbol,
						Target: session.modernGUIDForLegacyLocked(target.Target),
					}
				}
				session.worldMu.Unlock()
				body := modernworld.EncodeRaidTargetUpdateAll(0, targets)
				if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGRaidTargetUpdateAll, Body: body}); err != nil {
					return
				}
				continue
			}
			session.worldMu.Lock()
			target := session.modernGUIDForLegacyLocked(update.Target)
			changedBy := session.modernGUIDForLegacyLocked(update.ChangedBy)
			session.worldMu.Unlock()
			body := modernworld.EncodeRaidTargetUpdateSingle(0, update.Symbol, target, changedBy)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGRaidTargetUpdateSingle, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGGroupDecline:
			body, err := modernworld.TranslateLegacyGroupDecline(packet.Body)
			if err != nil {
				s.log.Warn("translate group decline failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGGroupDecline, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGGroupUninvite:
			if len(packet.Body) != 0 {
				s.log.Warn("legacy group-uninvite malformed", "account", session.legacy.Username, "bytes", len(packet.Body))
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGGroupUninvite}); err != nil {
				return
			}
		case legacyworld.SMSGGroupDestroyed:
			if len(packet.Body) != 0 {
				s.log.Warn("legacy group-destroyed malformed", "account", session.legacy.Username, "bytes", len(packet.Body))
				continue
			}
			session.worldMu.Lock()
			sequence := session.partySequence
			session.partySequence++
			session.partyLegacyGUID = 0
			session.partyGUID = modernworld.GUID128{}
			session.partyIndex = 0
			session.partyMembers = nil
			session.partyRoles = nil
			session.lfgQueueMode = modernworld.LFGQueueIdle
			session.partyLootMethod = 0
			session.partyLootMaster = 0
			session.partyLootThreshold = 2
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGGroupDestroyed}); err != nil {
				return
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGPartyUpdate,
				Body: modernworld.EncodeDestroyedPartyUpdate(sequence)}); err != nil {
				return
			}
		case legacyworld.SMSGRaidGroupOnly:
			body, err := modernworld.TranslateRaidGroupOnly(packet.Body)
			if err != nil {
				s.log.Warn("translate raid-group-only failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGRaidGroupOnly, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGLFGDisabled:
			if err := s.relayTranslatedLFG(session, "lfg disabled", modernworld.SMSGLFGDisabled, packet.Body, func(body []byte, _ modernworld.LFGContext) ([]byte, error) {
				return modernworld.TranslateLegacyLFGDisabled(body)
			}); err != nil {
				return
			}
		case legacyworld.SMSGLFGOfferContinue:
			if err := s.relayTranslatedLFG(session, "lfg offer continue", modernworld.SMSGLFGOfferContinue, packet.Body, func(body []byte, _ modernworld.LFGContext) ([]byte, error) {
				return modernworld.TranslateLegacyLFGOfferContinue(body)
			}); err != nil {
				return
			}
		case legacyworld.SMSGLFGPlayerInfo:
			if err := s.relayTranslatedLFG(session, "lfg player info", modernworld.SMSGLFGPlayerInfo, packet.Body, func(body []byte, _ modernworld.LFGContext) ([]byte, error) {
				return modernworld.TranslateLegacyLFGPlayerInfo(body)
			}); err != nil {
				return
			}
		case legacyworld.SMSGLFGPartyInfo:
			if err := s.relayTranslatedLFG(session, "lfg party info", modernworld.SMSGLFGPartyInfo, packet.Body, modernworld.TranslateLegacyLFGPartyInfo); err != nil {
				return
			}
		case legacyworld.SMSGLFGJoinResult:
			if err := s.relayTranslatedLFG(session, "lfg join result", modernworld.SMSGLFGJoinResult, packet.Body, modernworld.TranslateLegacyLFGJoinResult); err != nil {
				return
			}
		case legacyworld.SMSGLFGQueueStatus:
			if err := s.relayTranslatedLFG(session, "lfg queue status", modernworld.SMSGLFGQueueStatus, packet.Body, modernworld.TranslateLegacyLFGQueueStatus); err != nil {
				return
			}
		case legacyworld.SMSGLFGRoleCheckUpdate:
			if err := s.relayTranslatedLFG(session, "lfg role check", modernworld.SMSGLFGRoleCheckUpdate, packet.Body, modernworld.TranslateLegacyLFGRoleCheckUpdate); err != nil {
				return
			}
		case legacyworld.SMSGLFGProposalUpdate:
			if err := s.relayTranslatedLFG(session, "lfg proposal", modernworld.SMSGLFGProposalUpdate, packet.Body, modernworld.TranslateLegacyLFGProposalUpdate); err != nil {
				return
			}
		case legacyworld.SMSGLFGPlayerReward:
			if err := s.relayTranslatedLFG(session, "lfg player reward", modernworld.SMSGLFGPlayerReward, packet.Body, func(body []byte, _ modernworld.LFGContext) ([]byte, error) {
				return modernworld.TranslateLegacyLFGPlayerReward(body)
			}); err != nil {
				return
			}
		case legacyworld.SMSGLFGUpdatePlayer:
			if err := s.relayLegacyLFGUpdate(session, packet.Body, false); err != nil {
				return
			}
		case legacyworld.SMSGLFGUpdateParty:
			if err := s.relayLegacyLFGUpdate(session, packet.Body, true); err != nil {
				return
			}
		case legacyworld.SMSGLFGRoleChosen:
			if err := s.relayTranslatedLFG(session, "lfg role chosen", modernworld.SMSGRoleChosen, packet.Body, modernworld.TranslateLegacyLFGRoleChosen); err != nil {
				return
			}
		case legacyworld.SMSGLFGTeleportDenied:
			if err := s.relayTranslatedLFG(session, "lfg teleport denied", modernworld.SMSGLFGTeleportDenied, packet.Body, func(body []byte, _ modernworld.LFGContext) ([]byte, error) {
				return modernworld.TranslateLegacyLFGTeleportDenied(body)
			}); err != nil {
				return
			}
		case legacyworld.SMSGPartyCommandResult:
			body, err := modernworld.TranslateLegacyPartyCommandResult(packet.Body)
			if err != nil {
				s.log.Warn("translate party command result failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGPartyCommandResult, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGInstanceDifficulty:
			difficulty, err := modernworld.ParseLegacyInstanceDifficulty(packet.Body)
			if err != nil {
				s.log.Warn("parse instance-difficulty failed", "error", err)
				continue
			}
			session.worldMu.Lock()
			session.mapDifficulty, session.hasMapDifficulty = difficulty, true
			ready := session.mapReady
			session.worldMu.Unlock()
			if ready {
				if err := session.sendMapDifficulty(); err != nil {
					return
				}
			}
		case legacyworld.SMSGUpdateInstanceEncounterUnit:
			event, err := modernworld.ParseEncounterEvent(packet.Body)
			if err != nil {
				s.log.Warn("parse encounter failed", "error", err)
				continue
			}
			session.worldMu.Lock()
			if session.encounterFrames == nil {
				session.encounterFrames = make(modernworld.EncounterFrames)
			}
			outputs := session.encounterFrames.Apply(event, session.visibleEncounterGUIDLocked)
			if event.Kind >= 3 && event.Kind <= 6 {
				out, err := modernworld.TranslateEncounterUnit(packet.Body, nil)
				if err != nil {
					session.worldMu.Unlock()
					s.log.Warn("translate encounter failed", "error", err)
					continue
				}
				outputs = append(outputs, out)
			}
			outputs = session.wrapEncounterPacketsLocked(outputs)
			inProgress := session.encounterInProgress
			frames := len(session.encounterFrames)
			session.worldMu.Unlock()
			s.log.Debug("instance encounter",
				"account", session.legacy.Username,
				"kind", event.Kind,
				"guid", fmt.Sprintf("0x%x", event.GUID),
				"in_progress", inProgress,
				"frames", frames,
				"packets", len(outputs))
			for _, out := range outputs {
				if err := session.sendInstance(out); err != nil {
					return
				}
			}
		case legacyworld.SMSGSetRaidDifficulty, legacyworld.SMSGInstanceSaveCreated, legacyworld.SMSGRaidInstanceMessage, legacyworld.SMSGInstanceLockWarningQuery:
			var body []byte
			var err error
			var opcode uint16
			switch packet.Opcode {
			case legacyworld.SMSGInstanceLockWarningQuery:
				body, err = modernworld.TranslatePendingRaidLock(packet.Body)
				opcode = modernworld.SMSGPendingRaidLock
			case legacyworld.SMSGSetRaidDifficulty:
				body, err = modernworld.TranslateLegacyRaidDifficulty(packet.Body)
				opcode = modernworld.SMSGRaidDifficultySet
			case legacyworld.SMSGInstanceSaveCreated:
				body, err = modernworld.TranslateInstanceSaveCreated(packet.Body)
				opcode = modernworld.SMSGInstanceSaveCreated
			case legacyworld.SMSGRaidInstanceMessage:
				body, err = modernworld.TranslateRaidInstanceMessage(packet.Body)
				opcode = modernworld.SMSGRaidInstanceMessage
			}
			if err != nil {
				s.log.Warn("translate pve notification failed", "opcode", packet.Opcode, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: opcode, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGSetDungeonDifficulty:
			difficulty, err := modernworld.ParseLegacyDungeonDifficulty(packet.Body)
			if err != nil {
				s.log.Warn("parse dungeon-difficulty failed", "account", session.legacy.Username, "opcode", packet.Opcode, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSetDungeonDifficulty, Body: modernworld.EncodeDungeonDifficulty(difficulty)}); err != nil {
				return
			}
		case legacyworld.SMSGClientCacheVersion:
			if len(packet.Body) != 4 {
				s.log.Warn("legacy cache-version malformed", "account", session.legacy.Username, "bytes", len(packet.Body))
				continue
			}
			if err := session.sendRealm(modernworld.Packet{Opcode: modernworld.SMSGCacheVersion, Body: modernworld.EncodeLegacyCacheVersion(binary.LittleEndian.Uint32(packet.Body))}); err != nil {
				return
			}
		case legacyworld.SMSGEquipmentSetList:
			if len(packet.Body) != 4 || binary.LittleEndian.Uint32(packet.Body) != 0 {
				s.log.Warn("non-empty legacy equipment-set list is not translatable", "account", session.legacy.Username, "bytes", len(packet.Body))
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGLoadEquipmentSet, Body: append([]byte(nil), packet.Body...)}); err != nil {
				return
			}
		case legacyworld.SMSGAddonInfo, legacyworld.SMSGLearnedDanceMoves:
			// Modern addon registration is handled independently. Wrath dance data
			// has no 3.4 equivalent, so both legacy-only metadata packets end here.
			continue
		case legacyworld.SMSGStartMirrorTimer:
			timer, err := modernworld.ParseLegacyStartMirrorTimer(packet.Body)
			if err != nil {
				s.log.Warn("parse start-mirror-timer failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGStartMirrorTimer, Body: modernworld.EncodeStartMirrorTimer(timer)}); err != nil {
				return
			}
		case legacyworld.SMSGStopMirrorTimer:
			timer, err := modernworld.ParseLegacyStopMirrorTimer(packet.Body)
			if err != nil {
				s.log.Warn("parse stop-mirror-timer failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGStopMirrorTimer, Body: modernworld.EncodeStopMirrorTimer(timer)}); err != nil {
				return
			}
		case legacyworld.SMSGPauseMirrorTimer:
			timer, paused, err := modernworld.ParseLegacyPauseMirrorTimer(packet.Body)
			if err != nil {
				s.log.Warn("parse pause-mirror-timer failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGPauseMirrorTimer, Body: modernworld.EncodePauseMirrorTimer(timer, paused)}); err != nil {
				return
			}
		case legacyworld.SMSGPlayMusic:
			soundID, err := modernworld.ParseLegacyPlayMusic(packet.Body)
			if err != nil {
				s.log.Warn("parse play-music failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGPlayMusic, Body: modernworld.EncodePlayMusic(soundID)}); err != nil {
				return
			}
		case legacyworld.SMSGZoneUnderAttack:
			areaID, err := modernworld.ParseLegacyZoneUnderAttack(packet.Body)
			if err != nil {
				s.log.Warn("parse zone-under-attack failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGZoneUnderAttack, Body: modernworld.EncodeZoneUnderAttack(areaID)}); err != nil {
				return
			}
		case legacyworld.SMSGDefenseMessage:
			message, err := modernworld.ParseLegacyDefenseMessage(packet.Body)
			if err != nil {
				s.log.Warn("parse defense-message failed", "account", session.legacy.Username, "error", err)
				continue
			}
			body, err := modernworld.EncodeDefenseMessage(message)
			if err != nil {
				s.log.Warn("encode defense-message failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGDefenseMessage, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGChatServerMessage:
			message, err := modernworld.ParseLegacyChatServerMessage(packet.Body)
			if err != nil {
				s.log.Warn("parse chat-server-message failed", "account", session.legacy.Username, "error", err)
				continue
			}
			body, err := modernworld.EncodeChatServerMessage(message)
			if err != nil {
				s.log.Warn("encode chat-server-message failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGChatServerMessage, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGQueryItemTextResponse:
			itemGUID, text, present, err := modernworld.ParseLegacyQueryItemText(packet.Body)
			if err != nil {
				s.log.Warn("parse item-text failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if !present {
				continue
			}
			session.worldMu.Lock()
			item := session.modernGUIDForLegacyLocked(itemGUID)
			session.worldMu.Unlock()
			body, err := modernworld.EncodeQueryItemTextResponse(item, text)
			if err != nil {
				s.log.Warn("encode item-text failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGQueryItemTextResponse, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGInvalidatePlayer:
			legacyGUID, err := modernworld.ParseLegacyInvalidatePlayer(packet.Body)
			if err != nil {
				s.log.Warn("parse invalidate-player failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			guid := session.modernGUIDForLegacyLocked(legacyGUID)
			delete(session.playerNames, legacyGUID)
			delete(session.playerIdentities, legacyGUID)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGInvalidatePlayer, Body: modernworld.EncodeInvalidatePlayer(guid)}); err != nil {
				return
			}
		case legacyworld.SMSGSpecialMountAnim:
			legacyGUID, err := modernworld.ParseLegacySpecialMountAnim(packet.Body)
			if err != nil {
				s.log.Warn("parse special-mount-anim failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			guid := session.modernGUIDForLegacyLocked(legacyGUID)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSpecialMountAnim, Body: modernworld.EncodeSpecialMountAnim(guid)}); err != nil {
				return
			}
		case legacyworld.SMSGPlaySound:
			soundKitID, err := modernworld.ParseLegacyPlaySound(packet.Body)
			if err != nil {
				s.log.Warn("parse play-sound failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			source := session.modernGUIDForLegacyLocked(session.currentCharacter)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGPlaySound, Body: modernworld.EncodePlaySound(soundKitID, source)}); err != nil {
				return
			}
		case legacyworld.SMSGGameObjectDespawn:
			legacyGUID, err := modernworld.ParseLegacyUnpackedGUID(packet.Body)
			if err != nil {
				s.log.Warn("parse game-object-despawn failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			guid := session.modernGUIDForLegacyLocked(legacyGUID)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGGameObjectDespawn, Body: modernworld.EncodeGameObjectDespawn(guid)}); err != nil {
				return
			}
		case legacyworld.SMSGGameObjectCustomAnim:
			anim, err := modernworld.ParseLegacyGameObjectCustomAnim(packet.Body)
			if err != nil {
				s.log.Warn("parse game-object-custom-anim failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			guid := session.modernGUIDForLegacyLocked(anim.GUID)
			session.worldMu.Unlock()
			body := modernworld.EncodeGameObjectCustomAnim(guid, anim.CustomAnim)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGGameObjectCustomAnim, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGGameObjectResetState:
			legacyGUID, err := modernworld.ParseLegacyUnpackedGUID(packet.Body)
			if err != nil {
				s.log.Warn("parse game-object-reset-state failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			guid := session.modernGUIDForLegacyLocked(legacyGUID)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGGameObjectResetState, Body: modernworld.EncodeGameObjectResetState(guid)}); err != nil {
				return
			}
		case legacyworld.SMSGFishNotHooked, legacyworld.SMSGFishEscaped:
			if err := modernworld.ValidateLegacyFishEvent(packet.Body); err != nil {
				s.log.Warn("parse fish event failed", "account", session.legacy.Username, "opcode", packet.Opcode, "error", err)
				continue
			}
			opcode := modernworld.SMSGFishNotHooked
			if packet.Opcode == legacyworld.SMSGFishEscaped {
				opcode = modernworld.SMSGFishEscaped
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: opcode}); err != nil {
				return
			}
		case legacyworld.SMSGSetFactionStanding:
			update, err := modernworld.ParseLegacyFactionStanding(packet.Body)
			if err != nil {
				s.log.Warn("parse faction-standing failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSetFactionStanding, Body: modernworld.EncodeFactionStanding(update)}); err != nil {
				return
			}
		case legacyworld.SMSGSetFactionVisible:
			index, err := modernworld.ParseLegacyFactionVisible(packet.Body)
			if err != nil {
				s.log.Warn("parse faction-visible failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSetFactionVisible, Body: modernworld.EncodeFactionVisible(index)}); err != nil {
				return
			}
		case legacyworld.SMSGCriteriaUpdate:
			update, err := modernworld.ParseLegacyCriteriaUpdate(packet.Body)
			if err != nil {
				s.log.Warn("parse criteria-update failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			player := session.modernGUIDForLegacyLocked(update.Player)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGCriteriaUpdate, Body: modernworld.EncodeCriteriaUpdate(update, player)}); err != nil {
				return
			}
		case legacyworld.SMSGAllAchievementData:
			data, err := modernworld.ParseLegacyAllAchievementData(packet.Body)
			if err != nil {
				s.log.Warn("parse all-achievement-data failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			owner := session.modernGUIDForLegacyLocked(session.currentCharacter)
			realmAddress := realm.Address(session.selectedRealm.ID)
			session.worldMu.Unlock()
			body := modernworld.EncodeAllAchievementData(data, owner, realmAddress)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGAllAchievementData, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGAchievementEarned:
			earned, err := modernworld.ParseLegacyAchievementEarned(packet.Body)
			if err != nil {
				s.log.Warn("parse achievement-earned failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			player := session.modernGUIDForLegacyLocked(earned.Player)
			realmAddress := realm.Address(session.selectedRealm.ID)
			session.worldMu.Unlock()
			body := modernworld.EncodeAchievementEarned(earned, player, realmAddress)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGAchievementEarned, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGAchievementDeleted:
			achievementID, err := modernworld.ParseLegacyAchievementDeleted(packet.Body)
			if err != nil {
				s.log.Warn("parse achievement-deleted failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGAchievementDeleted, Body: modernworld.EncodeAchievementDeleted(achievementID)}); err != nil {
				return
			}
		case legacyworld.SMSGRespondInspectAchievements:
			legacyOwner, data, err := modernworld.ParseLegacyInspectAchievements(packet.Body)
			if err != nil {
				s.log.Warn("parse inspect-achievements failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			owner := session.modernGUIDForLegacyLocked(legacyOwner)
			realmAddress := realm.Address(session.selectedRealm.ID)
			session.worldMu.Unlock()
			body := modernworld.EncodeRespondInspectAchievements(data, owner, realmAddress)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGRespondInspectAchievements, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGInspectTalent:
			inspect, err := modernworld.ParseLegacyInspectResult(packet.Body)
			if err != nil {
				s.log.Warn("parse inspect result failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			guid := session.modernGUIDForLegacyLocked(inspect.Target)
			name := session.playerNames[inspect.Target]
			if name == "" {
				if member, ok := session.partyMembers[inspect.Target]; ok {
					name = member.Name
				}
			}
			if name == "" {
				name = session.knownCharacterInfo[inspect.Target].Name
			}
			metadata := modernworld.InspectMetadataFromFields(guid, name, session.objectFields[inspect.Target])
			if identity, ok := session.playerIdentities[inspect.Target]; ok {
				if metadata.Name == "" {
					metadata.Name = identity.Name
				}
				if metadata.Race == 0 {
					metadata.Race = identity.Race
					metadata.Class = identity.Class
					metadata.Sex = identity.Sex
				}
			}
			body := modernworld.EncodeInspectResult(inspect, metadata, session.modernGUIDForLegacyLocked)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGInspectResult, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGTradeStatus:
			status, err := modernworld.ParseLegacyTradeStatus(packet.Body)
			if err != nil {
				s.log.Warn("parse trade status failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			partner := session.modernGUIDForLegacyLocked(status.Partner)
			if status.Status == 1 || status.Status == 2 {
				session.tradeActive = true
			}
			if status.Status == 2 {
				session.tradeID = status.TradeID
			}
			tradeDone := status.Status != 1 && status.Status != 2 && status.Status != 4 && status.Status != 7 && status.Status != 9 && status.Status != 22
			if tradeDone {
				session.tradeActive = false
			}
			session.worldMu.Unlock()
			partnerAccount := s.socialWowAccountGUID(session, status.Partner)
			body := modernworld.EncodeTradeStatus(status, partner, partnerAccount)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGTradeStatus, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGTradeStatusExtended:
			update, err := modernworld.ParseLegacyTradeUpdate(packet.Body)
			if err != nil {
				s.log.Warn("parse trade update failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			session.tradeServerState++
			body := modernworld.EncodeTradeUpdated(update, session.tradeID, session.tradeClientState,
				session.tradeServerState, session.modernGUIDForLegacyLocked)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGTradeUpdated, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGAttackStart:
			attacker, victim, err := modernworld.ParseLegacyUnpackedGUIDPair(packet.Body)
			if err != nil {
				s.log.Warn("parse attack-start failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			attGUID := session.modernGUIDForLegacyLocked(attacker)
			vicGUID := session.modernGUIDForLegacyLocked(victim)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGAttackStart, Body: modernworld.EncodeAttackStart(attGUID, vicGUID)}); err != nil {
				return
			}
		case legacyworld.SMSGAttackStop:
			attacker, victim, nowDead, err := modernworld.ParseLegacyAttackStop(packet.Body)
			if err != nil {
				s.log.Warn("parse attack-stop failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			attGUID := session.modernGUIDForLegacyLocked(attacker)
			vicGUID := session.modernGUIDForLegacyLocked(victim)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGAttackStop, Body: modernworld.EncodeAttackStop(attGUID, vicGUID, nowDead)}); err != nil {
				return
			}
		case legacyworld.SMSGAIReaction:
			legacyGUID, reaction, err := modernworld.ParseLegacyAIReaction(packet.Body)
			if err != nil {
				s.log.Warn("parse AI reaction failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			modernGUID := session.modernGUIDForLegacyLocked(legacyGUID)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGAIReaction, Body: modernworld.EncodeAIReaction(modernGUID, reaction)}); err != nil {
				return
			}
		case legacyworld.SMSGCancelCombat:
			if len(packet.Body) != 0 {
				s.log.Warn("legacy cancel-combat has trailing bytes", "account", session.legacy.Username, "bytes", len(packet.Body))
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGCancelCombat}); err != nil {
				return
			}
		case legacyworld.SMSGAttackSwingNotInRange, legacyworld.SMSGAttackSwingBadFacing, legacyworld.SMSGAttackSwingDeadTarget, legacyworld.SMSGAttackSwingCantAttack:
			reason := modernworld.AttackSwingCantAttack
			switch packet.Opcode {
			case legacyworld.SMSGAttackSwingNotInRange:
				reason = modernworld.AttackSwingNotInRange
			case legacyworld.SMSGAttackSwingBadFacing:
				reason = modernworld.AttackSwingBadFacing
			case legacyworld.SMSGAttackSwingDeadTarget:
				reason = modernworld.AttackSwingDeadTarget
			}
			body, err := modernworld.EncodeAttackSwingError(reason)
			if err != nil {
				s.log.Warn("encode attack-swing error failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGAttackSwingError, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGAttackerStateUpdate:
			update, err := modernworld.ParseLegacyAttackerStateUpdate(packet.Body)
			if err != nil {
				s.log.Warn("parse attacker-state-update failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			attGUID := session.modernGUIDForLegacyLocked(update.Attacker)
			vicGUID := session.modernGUIDForLegacyLocked(update.Victim)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGAttackerStateUpdate, Body: modernworld.EncodeAttackerStateUpdate(attGUID, vicGUID, update)}); err != nil {
				return
			}
		case legacyworld.SMSGThreatUpdate:
			unit, entries, err := modernworld.ParseLegacyThreatUpdate(packet.Body)
			if err != nil {
				s.log.Warn("parse threat-update failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			unitGUID := session.modernGUIDForLegacyLocked(unit)
			targets := make([]modernworld.GUID128, len(entries))
			for index, entry := range entries {
				targets[index] = session.modernGUIDForLegacyLocked(entry.Unit)
			}
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGThreatUpdate, Body: modernworld.EncodeThreatUpdate(unitGUID, entries, targets)}); err != nil {
				return
			}
		case legacyworld.SMSGHighestThreatUpdate:
			unit, victim, entries, err := modernworld.ParseLegacyHighestThreatUpdate(packet.Body)
			if err != nil {
				s.log.Warn("parse highest-threat-update failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			unitGUID := session.modernGUIDForLegacyLocked(unit)
			victimGUID := session.modernGUIDForLegacyLocked(victim)
			targets := make([]modernworld.GUID128, len(entries))
			for index, entry := range entries {
				targets[index] = session.modernGUIDForLegacyLocked(entry.Unit)
			}
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGHighestThreatUpdate, Body: modernworld.EncodeHighestThreatUpdate(unitGUID, victimGUID, entries, targets)}); err != nil {
				return
			}
		case legacyworld.SMSGThreatRemove:
			unit, about, err := modernworld.ParseLegacyPackedGUIDPair(packet.Body)
			if err != nil {
				s.log.Warn("parse threat-remove failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			unitGUID := session.modernGUIDForLegacyLocked(unit)
			aboutGUID := session.modernGUIDForLegacyLocked(about)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGThreatRemove, Body: modernworld.EncodeThreatRemove(unitGUID, aboutGUID)}); err != nil {
				return
			}
		case legacyworld.SMSGThreatClear:
			unit, err := modernworld.ParseLegacyPackedGUID(packet.Body)
			if err != nil {
				s.log.Warn("parse threat-clear failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			unitGUID := session.modernGUIDForLegacyLocked(unit)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGThreatClear, Body: modernworld.EncodeThreatClear(unitGUID)}); err != nil {
				return
			}
		case legacyworld.SMSGPartyKillLog:
			var (
				player, victim uint64
				err            error
			)
			if len(packet.Body) == 16 {
				player, victim, err = modernworld.ParseLegacyUnpackedGUIDPair(packet.Body)
			} else {
				player, victim, err = modernworld.ParseLegacyPackedGUIDPair(packet.Body)
			}
			if err != nil {
				s.log.Warn("parse party-kill-log failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			playerGUID := session.modernGUIDForLegacyLocked(player)
			victimGUID := session.modernGUIDForLegacyLocked(victim)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGPartyKillLog, Body: modernworld.EncodePartyKillLog(playerGUID, victimGUID)}); err != nil {
				return
			}
		case legacyworld.SMSGSpellMissLog:
			log, err := modernworld.ParseLegacySpellMissLog(packet.Body)
			if err != nil {
				s.log.Warn("parse spell-miss-log failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			body := modernworld.EncodeSpellMissLog(log, session.modernGUIDForLegacyLocked)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSpellMissLog, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGSpellExecuteLog:
			log, err := modernworld.ParseLegacySpellExecuteLog(packet.Body)
			if err != nil {
				s.log.Warn("parse spell-execute-log failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			body := modernworld.EncodeSpellExecuteLog(log, session.modernGUIDForLegacyLocked)
			interrupts := modernworld.EncodeSpellInterruptLogs(log, session.modernGUIDForLegacyLocked)
			session.worldMu.Unlock()
			for _, p := range interrupts {
				if err := session.sendInstance(p); err != nil {
					return
				}
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSpellExecuteLog, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGSpellNonMeleeDamageLog:
			log, err := modernworld.ParseLegacySpellNonMeleeDamageLog(packet.Body)
			if err != nil {
				s.log.Warn("parse non-melee damage log failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			target := session.modernGUIDForLegacyLocked(log.Target)
			caster := session.modernGUIDForLegacyLocked(log.Caster)
			castID := modernworld.ModernCastGUID(session.currentMapID, log.SpellID, uint64(log.SpellID)+caster.Low)
			if session.currentCharacter != 0 && log.Caster == session.currentCharacter {
				// Peek the completed SPELL_GO CastID. Consuming pendingCasts here
				// steals the next Lightning Bolt's START/GO pairing because the
				// projectile combat log arrives ~1s after SPELL_GO.
				castID = session.playerCombatLogCastIDLocked(log.SpellID, castID)
			}
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSpellNonMeleeDamageLog, Body: modernworld.EncodeSpellNonMeleeDamageLog(target, caster, castID, log)}); err != nil {
				return
			}
		case legacyworld.SMSGSpellHealLog:
			log, err := modernworld.ParseLegacySpellHealLog(packet.Body)
			if err != nil {
				s.log.Warn("parse heal-log failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			target := session.modernGUIDForLegacyLocked(log.Target)
			caster := session.modernGUIDForLegacyLocked(log.Caster)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSpellHealLog, Body: modernworld.EncodeSpellHealLog(target, caster, log)}); err != nil {
				return
			}
		case legacyworld.SMSGSpellPeriodicAuraLog:
			log, err := modernworld.ParseLegacySpellPeriodicAuraLog(packet.Body)
			if err != nil {
				s.log.Warn("parse periodic-aura log failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			target := session.modernGUIDForLegacyLocked(log.Target)
			caster := session.modernGUIDForLegacyLocked(log.Caster)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSpellPeriodicAuraLog, Body: modernworld.EncodeSpellPeriodicAuraLog(target, caster, log)}); err != nil {
				return
			}
		case legacyworld.SMSGSpellDelayed:
			delayed, err := modernworld.ParseLegacySpellDelayed(packet.Body)
			if err != nil {
				s.log.Warn("parse spell-delayed failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			caster := session.modernGUIDForLegacyLocked(delayed.Caster)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSpellDelayed, Body: modernworld.EncodeSpellDelayed(caster, delayed)}); err != nil {
				return
			}
		case legacyworld.SMSGSpellEnergizeLog:
			log, err := modernworld.ParseLegacySpellEnergizeLog(packet.Body)
			if err != nil {
				s.log.Warn("parse energize-log failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			target := session.modernGUIDForLegacyLocked(log.Target)
			caster := session.modernGUIDForLegacyLocked(log.Caster)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSpellEnergizeLog, Body: modernworld.EncodeSpellEnergizeLog(target, caster, log)}); err != nil {
				return
			}
		case legacyworld.SMSGLogXPGain:
			log, err := modernworld.ParseLegacyLogXPGain(packet.Body)
			if err != nil {
				s.log.Warn("parse xp-gain failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			victim := session.modernGUIDForLegacyLocked(log.Victim)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGLogXPGain, Body: modernworld.EncodeLogXPGain(victim, log)}); err != nil {
				return
			}
		case legacyworld.SMSGLevelUpInfo:
			info, err := modernworld.ParseLegacyLevelUpInfo(packet.Body)
			if err != nil {
				s.log.Warn("parse level-up info failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			playerFields := session.objectFields[session.currentCharacter]
			previousLevel := modernworld.PlayerLevelFromFields(playerFields)
			class := modernworld.PlayerClassFromFields(playerFields)
			modernworld.SetLegacyPlayerLevel(playerFields, info.Level)
			session.worldMu.Unlock()
			modernworld.SetLevelUpTalentDelta(&info, previousLevel, class)
			s.log.Debug("level-up info", "account", session.legacy.Username, "previous", previousLevel,
				"level", info.Level, "class", class, "new_talents", info.NumNewTalents)
			// LevelUpInfo is one of build 54261's realm-connection gameplay packets.
			if err := session.sendRealm(modernworld.Packet{Opcode: modernworld.SMSGLevelUpInfo, Body: modernworld.EncodeLevelUpInfo(info)}); err != nil {
				return
			}
		case legacyworld.SMSGPetSpells:
			spells, err := modernworld.ParseLegacyPetSpells(packet.Body)
			if err != nil {
				s.log.Warn("parse pet-spells failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if spells.PetGUID == 0 {
				if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGPetClearSpells}); err != nil {
					return
				}
				s.log.Debug("clear pet spells", "account", session.legacy.Username)
				continue
			}
			session.worldMu.Lock()
			pet := session.modernGUIDForLegacyLocked(spells.PetGUID)
			session.worldMu.Unlock()
			body := modernworld.EncodePetSpells(pet, spells)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGPetSpells, Body: body}); err != nil {
				return
			}
			s.log.Debug("pet spells", "account", session.legacy.Username, "pet", fmt.Sprintf("0x%x", spells.PetGUID), "actions", len(spells.Actions), "cooldowns", len(spells.Cooldowns))
		case legacyworld.SMSGPetTameFailure:
			if len(packet.Body) != 1 {
				s.log.Warn("pet-tame-failure malformed", "account", session.legacy.Username, "bytes", len(packet.Body))
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGPetTameFailure, Body: append([]byte(nil), packet.Body...)}); err != nil {
				return
			}
		case legacyworld.SMSGListStabledPets:
			list, err := modernworld.ParseLegacyStabledPets(packet.Body)
			if err != nil {
				s.log.Warn("parse stabled-pets failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := s.sendStabledPets(session, list); err != nil {
				return
			}
		case legacyworld.SMSGPetStableResult:
			result, err := modernworld.ParseLegacyPetStableResult(packet.Body)
			if err != nil {
				s.log.Warn("parse pet-stable-result failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGPetStableResult, Body: modernworld.EncodePetStableResult(result)}); err != nil {
				return
			}
			s.log.Debug("pet stable result", "account", session.legacy.Username, "result", result)
			if !modernworld.PetStableResultSucceeded(result) {
				continue
			}
			session.worldMu.Lock()
			master := session.lastStableMaster
			legacyConn := session.legacyWorld
			session.worldMu.Unlock()
			if master == 0 || legacyConn == nil {
				continue
			}
			s.log.Debug("reload stabled pets after result", "account", session.legacy.Username, "legacy", fmt.Sprintf("0x%x", master), "result", result)
			if err := legacyConn.WritePacket(uint32(legacyworld.MSGListStabledPets), modernworld.EncodeLegacyStableMaster(master)); err != nil {
				return
			}
		case legacyworld.SMSGPetActionSound:
			sound, err := modernworld.ParseLegacyPetActionSound(packet.Body)
			if err != nil {
				s.log.Warn("parse pet-action-sound failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			unit := session.modernGUIDForLegacyLocked(sound.UnitGUID)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGPetActionSound, Body: modernworld.EncodePetActionSound(unit, sound.Action)}); err != nil {
				return
			}
			s.log.Debug("pet action sound", "account", session.legacy.Username, "unit", fmt.Sprintf("0x%x", sound.UnitGUID), "action", sound.Action)
		case legacyworld.SMSGSpellFailure:
			// Hermes consumes WotLK SPELL_FAILURE and synthesizes the 3.4.3
			// SpellFailure from SPELL_FAILED_OTHER so both modern packets share
			// one CastID/visual. Forwarding only SPELL_FAILURE left the caster
			// pose raised after an out-of-range Fireball.
			if _, err := modernworld.ParseLegacySpellFailure(packet.Body, false); err != nil {
				s.log.Warn("parse spell-failure failed", "account", session.legacy.Username, "error", err)
			}
		case legacyworld.SMSGSpellFailedOther:
			info, err := modernworld.ParseLegacySpellFailure(packet.Body, true)
			if err != nil {
				s.log.Warn("parse spell-failed-other failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			caster := session.modernGUIDForLegacyLocked(info.Caster)
			castID, visual := session.spellFailureCastLocked(info.SpellID, caster)
			session.worldMu.Unlock()
			failure := modernworld.EncodeSpellFailure(caster, castID, info, visual)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSpellFailure, Body: failure}); err != nil {
				return
			}
			other := modernworld.EncodeSpellFailedOther(caster, castID, info, visual)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSpellFailedOther, Body: other}); err != nil {
				return
			}
		case legacyworld.SMSGCastFailed:
			info, err := modernworld.ParseLegacyCastFailed(packet.Body)
			if err != nil {
				s.log.Warn("parse cast-failed failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			if session.swallowCastFailedLocked(info.SpellID, info.Reason) {
				retryBody := session.spellQueueRetryBodyLocked()
				legacyConn := session.legacyWorld
				account := session.legacy.Username
				session.worldMu.Unlock()
				s.log.Debug("spell queue swallowed cast-failed", "account", account, "spell", info.SpellID, "legacy_reason", info.Reason)
				if retryBody != nil && legacyConn != nil {
					if err := legacyConn.WritePacket(legacyworld.CMSGCastSpell, retryBody); err != nil {
						return
					}
				}
				continue
			}
			if info.Reason == spellFailedSpellInProgress {
				s.log.Debug("spell-in-progress not queued", "account", session.legacy.Username, "spell", info.SpellID, "legacy_reason", info.Reason)
			}
			pending, found := session.matchPlayerCastFailureLocked(info.SpellID, true)
			var heldBody []byte
			if found {
				heldBody = session.failSpellQueueLocked(pending)
			}
			castID := modernworld.ModernCastGUID(session.currentMapID, info.SpellID, uint64(info.SpellID))
			visual := uint32(0)
			started := false
			if found {
				castID = pending.ServerCastID
				visual = pending.VisualID
				started = pending.Started
			} else if id, ok := session.completedCastIDs[info.SpellID]; ok {
				castID = id
				visual = session.spellVisuals[info.SpellID]
				started = true
			}
			clientCastID := pending.ClientCastID
			legacyConn := session.legacyWorld
			session.worldMu.Unlock()
			if found && !started {
				if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSpellPrepare, Body: modernworld.EncodeSpellPrepare(clientCastID, pending.ServerCastID)}); err != nil {
					return
				}
			}
			reason := modernworld.ConvertSpellCastResult343(info.Reason)
			s.log.Debug("cast failed", "account", session.legacy.Username, "spell", info.SpellID, "legacy_reason", info.Reason, "reason", reason, "visual", visual)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGCastFailed, Body: modernworld.EncodeCastFailed(castID, info, visual)}); err != nil {
				return
			}
			if len(heldBody) > 0 && legacyConn != nil {
				if err := legacyConn.WritePacket(legacyworld.CMSGCastSpell, heldBody); err != nil {
					return
				}
			}
		case legacyworld.SMSGPetCastFailed:
			failure, err := modernworld.ParseLegacyPetCastFailed(packet.Body)
			if err != nil {
				s.log.Warn("parse pet-cast-failed failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			pending, found := session.matchPetCastFailureLocked(failure.SpellID)
			castID := modernworld.ModernCastGUID(session.currentMapID, failure.SpellID, uint64(failure.SpellID))
			if found {
				castID = pending.ClientCastID
			}
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGPetCastFailed, Body: modernworld.EncodePetCastFailed(castID, failure)}); err != nil {
				return
			}
		case legacyworld.SMSGCancelAutoRepeat:
			target, err := modernworld.ParseLegacyCancelAutoRepeat(packet.Body)
			if err != nil {
				s.log.Warn("parse cancel-auto-repeat failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			modernTarget := session.modernGUIDForLegacyLocked(target)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGCancelAutoRepeat, Body: modernworld.EncodeCancelAutoRepeat(modernTarget)}); err != nil {
				return
			}
		case legacyworld.SMSGPlaySpellVisual, 0x01f7: // SMSG_PLAY_SPELL_IMPACT uses the same GUID + kit layout
			caster, kitID, err := modernworld.ParseLegacyPlaySpellVisual(packet.Body)
			if err != nil {
				s.log.Warn("parse play-spell-visual failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			modernCaster := session.modernGUIDForLegacyLocked(caster)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGPlaySpellVisualKit, Body: modernworld.EncodePlaySpellVisualKit(modernCaster, kitID)}); err != nil {
				return
			}
		case legacyworld.SMSGEnvironmentalDamageLog:
			damage, err := modernworld.ParseLegacyEnvironmentalDamage(packet.Body)
			if err != nil {
				s.log.Warn("parse environmental-damage failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			victim := session.modernGUIDForLegacyLocked(damage.Victim)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGEnvironmentalDamageLog, Body: modernworld.EncodeEnvironmentalDamage(damage, victim)}); err != nil {
				return
			}
		case legacyworld.SMSGSpellDamageShield:
			shield, err := modernworld.ParseLegacySpellDamageShield(packet.Body)
			if err != nil {
				s.log.Warn("parse damage-shield failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			victim := session.modernGUIDForLegacyLocked(shield.Victim)
			caster := session.modernGUIDForLegacyLocked(shield.Caster)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSpellDamageShield, Body: modernworld.EncodeSpellDamageShield(shield, victim, caster)}); err != nil {
				return
			}
		case legacyworld.SMSGSpellDispellLog:
			log, err := modernworld.ParseLegacySpellDispellLog(packet.Body)
			if err != nil {
				s.log.Warn("parse dispell-log failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			target := session.modernGUIDForLegacyLocked(log.Target)
			caster := session.modernGUIDForLegacyLocked(log.Caster)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSpellDispellLog, Body: modernworld.EncodeSpellDispellLog(log, target, caster)}); err != nil {
				return
			}
		case legacyworld.SMSGSpellInstakillLog:
			kill, err := modernworld.ParseLegacySpellInstakill(packet.Body)
			if err != nil {
				s.log.Warn("parse instakill failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			caster := session.modernGUIDForLegacyLocked(kill.Caster)
			target := session.modernGUIDForLegacyLocked(kill.Target)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSpellInstakillLog, Body: modernworld.EncodeSpellInstakill(kill, caster, target)}); err != nil {
				return
			}
		case legacyworld.SMSGTotemCreated:
			// TotemFrame timers use SMSG_TOTEM_CREATED. The extra shaman
			// action bar (MultiCastActionBarFrame, next to stealth) does not:
			// it only updates on PLAYER_ENTERING_WORLD / UPDATE_MULTI_CAST_ACTIONBAR.
			// Send the totem packet on both sockets (legacy proxy uses instance,
			// Hermes defaults to realm) then re-push action buttons so the
			// multicast bar can unhide now that GetTotemInfo is true.
			totem, err := modernworld.ParseLegacyTotemCreated(packet.Body)
			if err != nil {
				s.log.Warn("parse totem-created failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			modernTotem := session.modernGUIDForLegacyLocked(totem.Totem)
			session.worldMu.Unlock()
			s.log.Debug("totem created",
				"account", session.legacy.Username,
				"slot", totem.Slot,
				"legacy", fmt.Sprintf("0x%x", totem.Totem),
				"spell", totem.SpellID,
				"duration", totem.Duration)
			body := modernworld.EncodeTotemCreated(totem, modernTotem)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGTotemCreated, Body: body}); err != nil {
				return
			}
			if err := session.sendRealm(modernworld.Packet{Opcode: modernworld.SMSGTotemCreated, Body: append([]byte(nil), body...)}); err != nil {
				s.log.Warn("totem-created realm send failed", "account", session.legacy.Username, "error", err)
			}
			if err := refreshMultiCastActionBar(session); err != nil {
				return
			}
		case legacyworld.SMSGSpellStart:
			cast, err := modernworld.ParseLegacySpellStartOrGo(packet.Body, false)
			if err != nil {
				s.log.Warn("parse spell-start failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			waitGUID, wait := session.unknownSpellObjectLocked(cast)
			queued := wait && session.queueObjectSpellLocked(cast, true)
			session.worldMu.Unlock()
			if wait {
				if !queued {
					s.log.Warn("dropping spell-start because object queue is full", "account", session.legacy.Username, "spell", cast.SpellID, "caster", fmt.Sprintf("0x%x", waitGUID))
				} else {
					s.log.Debug("queue spell-start until caster object create", "account", session.legacy.Username, "spell", cast.SpellID, "caster", fmt.Sprintf("0x%x", waitGUID))
				}
				continue
			}
			if err := s.forwardLegacySpell(session, cast, true); err != nil {
				return
			}
		case legacyworld.SMSGSpellGo:
			cast, err := modernworld.ParseLegacySpellStartOrGo(packet.Body, true)
			if err != nil {
				s.log.Warn("parse spell-go failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			modernworld.AdvanceRuneCooldowns(cast.RemainingRunes, &session.runeCooldownStarted, time.Now())
			waitGUID, wait := session.unknownSpellObjectLocked(cast)
			queued := wait && session.queueObjectSpellLocked(cast, false)
			session.worldMu.Unlock()
			if wait {
				if !queued {
					s.log.Warn("dropping spell-go because object queue is full", "account", session.legacy.Username, "spell", cast.SpellID, "caster", fmt.Sprintf("0x%x", waitGUID))
				} else {
					s.log.Debug("queue spell-go until caster object create", "account", session.legacy.Username, "spell", cast.SpellID, "caster", fmt.Sprintf("0x%x", waitGUID))
				}
				continue
			}
			if err := s.forwardLegacySpell(session, cast, false); err != nil {
				return
			}
		case legacyworld.MSGChannelStart:
			channel, err := modernworld.ParseLegacySpellChannelStart(packet.Body)
			if err != nil {
				s.log.Warn("parse channel-start failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			caster := session.modernGUIDForLegacyLocked(channel.Caster)
			visual := session.visualIDForSpellLocked(channel.SpellID)
			objectType := session.objectTypes[channel.Caster]
			channelObject := session.channelObjects[channel.Caster]
			if cached := modernworld.LegacyChannelObject(session.objectFields[channel.Caster]); cached != 0 {
				channelObject = cached
			}
			if session.objectFields[channel.Caster] == nil {
				session.objectFields[channel.Caster] = make(map[int]uint32)
			}
			modernworld.ApplyLegacyChannelState(session.objectFields[channel.Caster], channel.SpellID, channelObject)
			mapID := session.currentMapID
			if channel.Caster == session.currentCharacter {
				session.currentChanneledSpell = channel.SpellID
			}
			session.worldMu.Unlock()
			body := modernworld.EncodeSpellChannelStart(caster, channel.SpellID, visual, channel.Duration)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSpellChannelStart, Body: body}); err != nil {
				return
			}
			if caster != (modernworld.GUID128{}) && (objectType == 3 || objectType == 4) {
				values, valuesErr := modernworld.EncodeUnitChannelValuesUpdate(caster, mapID, objectType, channel.SpellID, channelObject)
				if valuesErr != nil {
					s.log.Warn("encode channel values failed", "account", session.legacy.Username, "error", valuesErr)
				} else if len(values) != 0 {
					if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGUpdateObject, Body: values}); err != nil {
						return
					}
				}
			}
			s.log.Debug("spell channel start", "account", session.legacy.Username, "spell", channel.SpellID, "visual", visual, "duration", channel.Duration, "channel_object", fmt.Sprintf("0x%x", channelObject))
		case legacyworld.MSGChannelUpdate:
			channel, err := modernworld.ParseLegacySpellChannelUpdate(packet.Body)
			if err != nil {
				s.log.Warn("parse channel-update failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			caster := session.modernGUIDForLegacyLocked(channel.Caster)
			objectType := session.objectTypes[channel.Caster]
			mapID := session.currentMapID
			clearChannel := channel.TimeRemaining == 0
			if clearChannel {
				delete(session.channelObjects, channel.Caster)
				if session.objectFields[channel.Caster] != nil {
					modernworld.ApplyLegacyChannelState(session.objectFields[channel.Caster], 0, 0)
				}
			}
			if channel.Caster == session.currentCharacter && channel.TimeRemaining == 0 {
				session.currentChanneledSpell = 0
			}
			session.worldMu.Unlock()
			body := modernworld.EncodeSpellChannelUpdate(caster, channel.TimeRemaining)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSpellChannelUpdate, Body: body}); err != nil {
				return
			}
			if clearChannel && caster != (modernworld.GUID128{}) && (objectType == 3 || objectType == 4) {
				values, valuesErr := modernworld.EncodeUnitChannelValuesUpdate(caster, mapID, objectType, 0, 0)
				if valuesErr != nil {
					s.log.Warn("encode channel-clear values failed", "account", session.legacy.Username, "error", valuesErr)
				} else if len(values) != 0 {
					if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGUpdateObject, Body: values}); err != nil {
						return
					}
				}
			}
		case legacyworld.SMSGSetFlatSpellModifier, legacyworld.SMSGSetPctSpellModifier:
			modifier, err := modernworld.ParseLegacySpellModifier(packet.Body)
			if err != nil {
				s.log.Warn("parse spell-modifier failed", "account", session.legacy.Username, "opcode", packet.Opcode, "error", err)
				continue
			}
			opcode := modernworld.SMSGSetFlatSpellModifier
			if packet.Opcode == legacyworld.SMSGSetPctSpellModifier {
				opcode = modernworld.SMSGSetPctSpellModifier
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: opcode, Body: modernworld.EncodeSpellModifier(modifier)}); err != nil {
				return
			}
			s.log.Debug("spell modifier", "account", session.legacy.Username, "opcode", packet.Opcode, "class", modifier.ClassIndex, "mod", modifier.ModIndex, "value", modifier.Value)
		case legacyworld.SMSGResurrectRequest:
			request, err := modernworld.ParseLegacyResurrectRequest(packet.Body)
			if err != nil {
				s.log.Warn("parse resurrect-request failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			caster := session.modernGUIDForLegacyLocked(request.Caster)
			realmAddress := realm.Address(session.selectedRealm.ID)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGResurrectRequest, Body: modernworld.EncodeResurrectRequest(caster, realmAddress, request)}); err != nil {
				return
			}
		case legacyworld.SMSGPreResurrect:
			// Modern 3.4.3 has no PRE_RESURRECT opcode. Validate and consume the
			// legacy prelude; the following aura/movement packets carry the state.
			if _, err := modernworld.ParseLegacyPreResurrect(packet.Body); err != nil {
				s.log.Warn("parse pre-resurrect failed", "account", session.legacy.Username, "error", err)
			}
		case legacyworld.SMSGCreatureQueryResponse:
			if entry, display, err := modernworld.LegacyCreatureQueryFirstDisplay(packet.Body); err == nil && display != 0 {
				session.worldMu.Lock()
				if session.creatureDisplay == nil {
					session.creatureDisplay = make(map[uint32]uint32)
				}
				session.creatureDisplay[entry] = display
				session.worldMu.Unlock()
			}
			body, err := modernworld.TranslateCreatureQueryResponse(packet.Body)
			if err != nil {
				s.log.Warn("translate creature query failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGQueryCreature, Body: body}); err != nil {
				return
			}
			s.log.Debug("creature query response", "account", session.legacy.Username, "bytes", len(body))
		case legacyworld.SMSGMirrorImageData:
			session.worldMu.Lock()
			resolve := func(guid uint64) modernworld.GUID128 {
				return session.modernGUIDForLegacyLocked(guid)
			}
			opcode, body, err := modernworld.TranslateLegacyMirrorImageData(packet.Body, resolve)
			session.worldMu.Unlock()
			if err != nil {
				s.log.Warn("translate mirror-image data failed", "account", session.legacy.Username, "error", err)
				continue
			}
			s.log.Debug("mirror-image data", "account", session.legacy.Username, "opcode", opcode, "bytes", len(body))
			// legacy proxy SendPacketToClient walks every ModernConn (capture had 6
			// 0x2C14 for 3 clones). 54261 applies the packet as a CMSG reply
			// on whichever connection issued 0x3297; instance-only delivery
			// left the mesh white.
			if err := session.sendInstance(modernworld.Packet{Opcode: opcode, Body: body}); err != nil {
				return
			}
			if err := session.sendRealm(modernworld.Packet{Opcode: opcode, Body: append([]byte(nil), body...)}); err != nil && !errors.Is(err, net.ErrClosed) {
				return
			}
		case legacyworld.SMSGLootStartRoll:
			roll, err := modernworld.ParseLegacyLootStartRoll(packet.Body)
			if err != nil {
				s.log.Warn("parse loot-start-roll failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			reference := session.rememberLootRollLocked(roll.Item.RollKey(), roll.GUID)
			session.worldMu.Unlock()
			body := modernworld.EncodeStartLootRoll(reference.modernGUID, roll)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGStartLootRoll, Body: body}); err != nil {
				return
			}
			s.log.Debug("loot roll started", "account", session.legacy.Username, "slot", roll.Item.Slot,
				"item", roll.Item.ItemID, "valid", roll.ValidRolls, "milliseconds", roll.RollTime,
				"legacy_guid", fmt.Sprintf("0x%x", roll.GUID), "modern_guid", reference.modernGUID)
		case legacyworld.SMSGLootRoll:
			roll, err := modernworld.ParseLegacyLootRollBroadcast(packet.Body)
			if err != nil {
				s.log.Warn("parse loot-roll broadcast failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			reference, found := session.resolveLootRollLocked(roll.Item.RollKey(), roll.GUID)
			player := session.modernGUIDForLegacyLocked(roll.Player)
			session.worldMu.Unlock()
			if !found {
				s.log.Warn("loot-roll broadcast has no matching start", "account", session.legacy.Username,
					"slot", roll.Item.Slot, "item", roll.Item.ItemID)
				continue
			}
			body := modernworld.EncodeLootRollBroadcast(reference.modernGUID, player, roll)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGLootRoll, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGLootRollWon:
			roll, err := modernworld.ParseLegacyLootRollWon(packet.Body)
			if err != nil {
				s.log.Warn("parse loot-roll-won failed", "account", session.legacy.Username, "error", err)
				continue
			}
			key := roll.Item.RollKey()
			session.worldMu.Lock()
			reference, found := session.resolveLootRollLocked(key, roll.GUID)
			winner := session.modernGUIDForLegacyLocked(roll.Winner)
			if found {
				session.completeLootRollLocked(reference)
			}
			session.worldMu.Unlock()
			if !found {
				s.log.Warn("loot-roll-won has no matching start", "account", session.legacy.Username,
					"slot", roll.Item.Slot, "item", roll.Item.ItemID)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGLootRollWon,
				Body: modernworld.EncodeLootRollWon(reference.modernGUID, winner, roll)}); err != nil {
				return
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGLootRollsComplete,
				Body: modernworld.EncodeLootRollsComplete(reference.modernGUID, roll.Item.Slot)}); err != nil {
				return
			}
		case legacyworld.SMSGLootAllPassed:
			roll, err := modernworld.ParseLegacyLootAllPassed(packet.Body)
			if err != nil {
				s.log.Warn("parse loot-all-passed failed", "account", session.legacy.Username, "error", err)
				continue
			}
			key := roll.Item.RollKey()
			session.worldMu.Lock()
			reference, found := session.resolveLootRollLocked(key, roll.GUID)
			if found {
				session.completeLootRollLocked(reference)
			}
			session.worldMu.Unlock()
			if !found {
				s.log.Warn("loot-all-passed has no matching start", "account", session.legacy.Username,
					"slot", roll.Item.Slot, "item", roll.Item.ItemID)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGLootAllPassed,
				Body: modernworld.EncodeLootAllPassed(reference.modernGUID, roll)}); err != nil {
				return
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGLootRollsComplete,
				Body: modernworld.EncodeLootRollsComplete(reference.modernGUID, roll.Item.Slot)}); err != nil {
				return
			}
		case legacyworld.SMSGLootMasterList:
			candidates, err := modernworld.ParseLegacyMasterLootCandidates(packet.Body)
			if err != nil {
				s.log.Warn("parse master-loot candidates failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			session.masterLootCandidates = append(session.masterLootCandidates[:0], candidates...)
			// AzerothCore can emit the candidate list either immediately before
			// or immediately after LOOT_RESPONSE.  Keep it pending until the loot
			// object is known, and send the modern loot-list as soon as both pieces
			// are available (the ordering used by HermesProxy).
			session.masterLootListPending = true
			lootGUID := session.lootLegacyGUID
			lootObj := session.lootObjModern
			if lootObj.Low == 0 && lootObj.High == 0 && lootGUID != 0 {
				lootObj = modernworld.ModernLootGUID(lootGUID, session.currentMapID)
				session.lootObjModern = lootObj
			}
			sendMasterList := lootGUID != 0 && (lootObj.Low != 0 || lootObj.High != 0) && session.lastMasterLootSentLegacy != lootGUID
			var owner, master modernworld.GUID128
			candidateGUIDs := make([]modernworld.GUID128, 0, len(candidates))
			if sendMasterList {
				owner = session.modernGUIDForLegacyLocked(lootGUID)
				master = session.modernGUIDForLegacyLocked(session.partyLootMaster)
				if master.Low == 0 && master.High == 0 {
					master = session.modernGUIDForLegacyLocked(session.currentCharacter)
				}
				for _, candidate := range candidates {
					candidateGUIDs = append(candidateGUIDs, session.modernGUIDForLegacyLocked(candidate))
				}
				if len(candidateGUIDs) == 0 {
					for candidate := range session.partyMembers {
						candidateGUIDs = append(candidateGUIDs, session.modernGUIDForLegacyLocked(candidate))
					}
				}
				session.lastMasterLootSentLegacy = lootGUID
				session.masterLootListPending = false
			}
			session.worldMu.Unlock()
			if sendMasterList {
				if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGLootList,
					Body: modernworld.EncodeMasterLootList(owner, lootObj, master)}); err != nil {
					return
				}
				if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGMasterLootCandidateList,
					Body: modernworld.EncodeMasterLootCandidateList(lootObj, candidateGUIDs)}); err != nil {
					return
				}
			}
		case legacyworld.SMSGLootResponse:
			loot, err := modernworld.ParseLegacyLootResponse(packet.Body)
			if err != nil {
				s.log.Warn("parse loot-response failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			session.lootLegacyGUID = loot.GUID
			session.lastLootTargetLegacy = loot.GUID
			session.lootObjModern = modernworld.ModernLootGUID(loot.GUID, session.currentMapID)
			// LootResponse.Owner is the creature/game-object being looted, not
			// the local player.  Using the player GUID makes the modern loot
			// window bind to the wrong object and the server rejects subsequent
			// master-loot operations as ineligible.
			owner := session.modernGUIDForLegacyLocked(loot.GUID)
			lootObj := session.lootObjModern
			lootMethod := session.partyLootMethod
			lootThreshold := session.partyLootThreshold
			masterLegacy := session.partyLootMaster
			master := session.modernGUIDForLegacyLocked(masterLegacy)
			isMaster := loot.FailureReason == 0 && lootMethod == 2 && masterLegacy == session.currentCharacter
			sendMasterList := isMaster && session.lastMasterLootSentLegacy != loot.GUID
			if session.masterLootListPending && isMaster {
				sendMasterList = true
			}
			candidateGUIDs := make([]modernworld.GUID128, 0, len(session.masterLootCandidates))
			if sendMasterList {
				for _, candidate := range session.masterLootCandidates {
					candidateGUIDs = append(candidateGUIDs, session.modernGUIDForLegacyLocked(candidate))
				}
				if len(candidateGUIDs) == 0 {
					for candidate := range session.partyMembers {
						candidateGUIDs = append(candidateGUIDs, session.modernGUIDForLegacyLocked(candidate))
					}
				}
				session.lastMasterLootSentLegacy = loot.GUID
				session.masterLootListPending = false
			}
			session.worldMu.Unlock()
			if sendMasterList {
				if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGLootList,
					Body: modernworld.EncodeMasterLootList(owner, lootObj, master)}); err != nil {
					return
				}
				if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGMasterLootCandidateList,
					Body: modernworld.EncodeMasterLootCandidateList(lootObj, candidateGUIDs)}); err != nil {
					return
				}
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGLootResponse,
				Body: modernworld.EncodeLootResponseWithSettings(owner, lootObj, loot, lootMethod, lootThreshold)}); err != nil {
				return
			}
			s.log.Debug("loot response", "account", session.legacy.Username, "guid", fmt.Sprintf("0x%x", loot.GUID), "items", len(loot.Items), "coins", loot.Coins, "failure", loot.FailureReason)
		case legacyworld.SMSGLootList:
			list, err := modernworld.ParseLegacyLootList(packet.Body)
			if err != nil {
				s.log.Warn("parse loot-list failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			owner := session.modernGUIDForLegacyLocked(list.Owner)
			lootObject := modernworld.ModernLootGUID(list.Owner, session.currentMapID)
			master := session.modernGUIDForLegacyLocked(list.Master)
			winner := session.modernGUIDForLegacyLocked(list.RoundRobinWinner)
			// Keep the source GUID from LOOT_LIST as a fallback for servers that
			// send the group-loot notification before LOOT_RESPONSE.
			session.lastLootTargetLegacy = list.Owner
			if session.lootLegacyGUID == 0 {
				session.lootLegacyGUID = list.Owner
				session.lootObjModern = lootObject
			}
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGLootList, Body: modernworld.EncodeLootList(owner, lootObject, master, winner)}); err != nil {
				return
			}
		case legacyworld.SMSGLootRelease:
			legacyGUID, err := modernworld.ParseLegacyLootRelease(packet.Body)
			if err != nil {
				s.log.Warn("parse loot-release failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			lootObj := session.lootObjModern
			if lootObj.Low == 0 && lootObj.High == 0 {
				lootObj = modernworld.ModernLootGUID(legacyGUID, session.currentMapID)
			}
			session.lootLegacyGUID = 0
			session.lootObjModern = modernworld.GUID128{}
			session.masterLootCandidates = nil
			session.masterLootListPending = false
			session.lastMasterLootSentLegacy = 0
			owner := session.modernGUIDForLegacyLocked(legacyGUID)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGLootRelease, Body: modernworld.EncodeLootRelease(lootObj, owner)}); err != nil {
				return
			}
		case legacyworld.SMSGLootRemoved:
			slot, err := modernworld.ParseLegacyLootRemoved(packet.Body)
			if err != nil {
				s.log.Warn("parse loot-removed failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			ownerLegacy := session.lootLegacyGUID
			if ownerLegacy == 0 {
				ownerLegacy = session.lastLootTargetLegacy
			}
			owner := session.modernGUIDForLegacyLocked(ownerLegacy)
			lootObj := session.lootObjModern
			if lootObj.Low == 0 && lootObj.High == 0 {
				lootObj = modernworld.ModernLootGUID(ownerLegacy, session.currentMapID)
			}
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGLootRemoved, Body: modernworld.EncodeLootRemoved(owner, lootObj, slot)}); err != nil {
				return
			}
		case legacyworld.SMSGLootClearMoney:
			session.worldMu.Lock()
			lootObj := session.lootObjModern
			if lootObj.Low == 0 && lootObj.High == 0 {
				lootObj = modernworld.ModernLootGUID(session.lootLegacyGUID, session.currentMapID)
			}
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGCoinRemoved, Body: modernworld.EncodeCoinRemoved(lootObj)}); err != nil {
				return
			}
		case legacyworld.SMSGLootMoneyNotify:
			money, sole, err := modernworld.ParseLegacyLootMoneyNotify(packet.Body)
			if err != nil {
				s.log.Warn("parse loot-money-notify failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGLootMoneyNotify, Body: modernworld.EncodeLootMoneyNotify(money, sole)}); err != nil {
				return
			}
		case legacyworld.SMSGItemPushResult:
			push, err := modernworld.ParseLegacyItemPushResult(packet.Body)
			if err != nil {
				s.log.Warn("parse item-push failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			player := session.modernGUIDForLegacyLocked(push.Player)
			if player.Low == 0 && player.High == 0 {
				player = session.modernGUIDForLegacyLocked(session.currentCharacter)
			}
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGItemPushResult, Body: modernworld.EncodeItemPushResult(player, push)}); err != nil {
				return
			}
			s.log.Debug("item push", "account", session.legacy.Username, "item", push.ItemID, "quantity", push.Quantity, "inventory", push.InventoryCount, "received", push.Received, "created", push.Created)
			if err := s.sendQuestItemProgressForItem(session, push.ItemID, push.InventoryCount); err != nil {
				return
			}
		case legacyworld.SMSGInventoryChangeFailure:
			failure, err := modernworld.ParseLegacyInventoryChangeFailure(packet.Body)
			if err != nil {
				s.log.Warn("parse inventory-change-failure failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if failure.Result == 0 {
				continue
			}
			s.log.Debug("inventory change failure", "account", session.legacy.Username, "legacy_result", failure.Result, "modern_result", modernworld.ModernInventoryResult(failure.Result))
			session.worldMu.Lock()
			itemA := session.modernGUIDForLegacyLocked(failure.ItemA)
			itemB := session.modernGUIDForLegacyLocked(failure.ItemB)
			srcContainer := session.modernGUIDForLegacyLocked(failure.SrcContainer)
			dstContainer := session.modernGUIDForLegacyLocked(failure.DstContainer)
			session.worldMu.Unlock()
			body := modernworld.EncodeInventoryChangeFailure(failure, itemA, itemB, srcContainer, dstContainer)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGInventoryChangeFailure, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGVendorInventory:
			vendor, err := modernworld.ParseLegacyVendorInventory(packet.Body)
			if err != nil {
				s.log.Warn("parse vendor inventory failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			guid := session.modernGUIDForLegacyLocked(vendor.Vendor)
			if session.vendorBuyCounts == nil {
				session.vendorBuyCounts = make(map[uint32]uint32)
			}
			for _, item := range vendor.Items {
				session.vendorBuyCounts[item.ItemID] = item.BuyCount
			}
			session.worldMu.Unlock()
			s.log.Debug("vendor inventory", "account", session.legacy.Username, "vendor", fmt.Sprintf("%#x", vendor.Vendor), "items", vendor.Items)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGVendorInventory, Body: modernworld.EncodeVendorInventory(guid, vendor)}); err != nil {
				return
			}
		case legacyworld.SMSGBuySucceeded:
			response, err := modernworld.ParseLegacyBuySucceeded(packet.Body)
			if err != nil {
				s.log.Warn("parse buy-success failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			guid := session.modernGUIDForLegacyLocked(response.Vendor)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGBuySucceeded, Body: modernworld.EncodeBuySucceeded(guid, response)}); err != nil {
				return
			}
		case legacyworld.SMSGBuyFailed:
			response, err := modernworld.ParseLegacyBuyFailed(packet.Body)
			if err != nil {
				s.log.Warn("parse buy-failed failed", "account", session.legacy.Username, "error", err)
				continue
			}
			s.log.Debug("buy failed", "account", session.legacy.Username, "vendor", fmt.Sprintf("%#x", response.Vendor), "item", response.Muid, "reason", response.Reason)
			session.worldMu.Lock()
			guid := session.modernGUIDForLegacyLocked(response.Vendor)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGBuyFailed, Body: modernworld.EncodeBuyFailed(guid, response)}); err != nil {
				return
			}
		case legacyworld.SMSGSellResponse:
			response, err := modernworld.ParseLegacySellResponse(packet.Body)
			if err != nil {
				s.log.Warn("parse sell-response failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			vendor := session.modernGUIDForLegacyLocked(response.Vendor)
			item := session.modernGUIDForLegacyLocked(response.Item)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSellResponse, Body: modernworld.EncodeSellResponse(vendor, item, response)}); err != nil {
				return
			}
		case legacyworld.SMSGTrainerList:
			trainer, err := modernworld.ParseLegacyTrainerList(packet.Body)
			if err != nil {
				s.log.Warn("parse trainer-list failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			guid := session.modernGUIDForLegacyLocked(trainer.Trainer)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGTrainerList, Body: modernworld.EncodeTrainerList(guid, trainer)}); err != nil {
				return
			}
		case legacyworld.MSGTalentWipeConfirm:
			confirm, err := modernworld.ParseLegacyTalentWipeConfirm(packet.Body)
			if err != nil {
				s.log.Warn("parse talent-wipe-confirm failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			guid := session.modernGUIDForLegacyLocked(confirm.Trainer)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGRespecWipeConfirm, Body: modernworld.EncodeRespecWipeConfirm(guid, confirm)}); err != nil {
				return
			}
		case legacyworld.SMSGTrainerBuyFailed:
			response, err := modernworld.ParseLegacyTrainerBuyFailed(packet.Body)
			if err != nil {
				s.log.Warn("parse trainer-buy-failed failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			guid := session.modernGUIDForLegacyLocked(response.Trainer)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGTrainerBuyFailed, Body: modernworld.EncodeTrainerBuyFailed(guid, response)}); err != nil {
				return
			}
		case legacyworld.SMSGTrainerBuySucceeded:
			// 3.4.3 has no equivalent opcode. AzerothCore follows this with
			// SMSG_LEARNED_SPELL, which is already translated and updates the UI.
			continue
		case legacyworld.SMSGUpdateTalentData:
			data, err := modernworld.ParseLegacyTalentData(packet.Body)
			if err != nil {
				s.log.Warn("parse talent-data failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGUpdateTalentData, Body: modernworld.EncodeTalentData(data)}); err != nil {
				return
			}
		case legacyworld.SMSGUpdateComboPoints, legacyworld.SMSGPetUpdateComboPoints:
			var (
				target uint64
				count  uint8
				err    error
			)
			if packet.Opcode == legacyworld.SMSGPetUpdateComboPoints {
				_, target, count, err = modernworld.ParseLegacyPetUpdateComboPoints(packet.Body)
			} else {
				target, count, err = modernworld.ParseLegacyUpdateComboPoints(packet.Body)
			}
			if err != nil {
				s.log.Warn("parse combo-points failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			player := session.modernGUIDForLegacyLocked(session.currentCharacter)
			targetGUID := session.modernGUIDForLegacyLocked(target)
			class := modernworld.PlayerClassFromFields(session.objectFields[session.currentCharacter])
			mapID := session.currentMapID
			session.worldMu.Unlock()
			if player.Low == 0 && player.High == 0 {
				continue
			}
			s.log.Debug("combo-points", "account", session.legacy.Username, "class", class, "count", count, "target", fmt.Sprintf("0x%x", target), "modern_low", fmt.Sprintf("0x%x", targetGUID.Low), "modern_high", fmt.Sprintf("0x%x", targetGUID.High))
			powerBody, err := modernworld.EncodeComboPoints(player, count)
			if err != nil {
				s.log.Warn("encode combo-points power failed", "account", session.legacy.Username, "error", err)
			} else {
				if err := session.sendRealm(modernworld.Packet{Opcode: modernworld.SMSGPowerUpdate, Body: powerBody}); err != nil {
					return
				}
				if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGPowerUpdate, Body: powerBody}); err != nil {
					return
				}
			}
			body, err := modernworld.EncodeComboPointValues(player, targetGUID, count, class, mapID)
			if err != nil {
				s.log.Warn("encode combo-points failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGUpdateObject, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGGossipPOI, legacyworld.SMSGUpdateLastInstance:
			out, err := modernworld.TranslateLegacyWorldNotification(packet.Opcode, packet.Body)
			if err != nil {
				s.log.Warn("translate world notification failed", "account", session.legacy.Username, "opcode", packet.Opcode, "error", err)
				continue
			}
			if err := session.sendInstance(out); err != nil {
				return
			}
		case legacyworld.SMSGGossipMessage:
			message, err := modernworld.ParseLegacyGossipMessage(packet.Body)
			if err != nil {
				s.log.Warn("parse gossip message failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			giver := session.modernGUIDForLegacyLocked(message.Giver)
			session.worldMu.Unlock()
			s.log.Debug("gossip message", "account", session.legacy.Username, "menu", message.GossipID, "options", len(message.Options), "quests", len(message.Quests))
			for _, quest := range message.Quests {
				s.log.Debug("gossip quest entry", "account", session.legacy.Username, "quest", quest.QuestID, "icon", quest.QuestType, "flags", quest.QuestFlags, "repeatable", quest.Repeatable)
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGGossipMessage, Body: modernworld.EncodeGossipMessage(giver, message)}); err != nil {
				return
			}
		case legacyworld.SMSGGossipComplete:
			if len(packet.Body) != 0 {
				s.log.Warn("legacy gossip-complete has trailing data", "account", session.legacy.Username, "bytes", len(packet.Body))
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGGossipComplete, Body: modernworld.EncodeGossipComplete()}); err != nil {
				return
			}
		case legacyworld.SMSGQuestGiverQuestList:
			list, err := modernworld.ParseLegacyQuestGiverQuestList(packet.Body)
			if err != nil {
				s.log.Warn("parse quest-giver list failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			giver := session.modernGUIDForLegacyLocked(list.Giver)
			session.worldMu.Unlock()
			s.log.Debug("quest-giver list", "account", session.legacy.Username, "quests", len(list.Quests))
			for _, quest := range list.Quests {
				s.log.Debug("quest-list entry", "account", session.legacy.Username, "quest", quest.QuestID, "icon", quest.QuestType, "flags", quest.QuestFlags, "repeatable", quest.Repeatable)
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGQuestGiverQuestList, Body: modernworld.EncodeQuestGiverQuestList(giver, list)}); err != nil {
				return
			}
		case legacyworld.SMSGQuestGiverQuestDetails:
			quest, err := modernworld.ParseLegacyQuestGiverQuestDetails(packet.Body)
			if err != nil {
				s.log.Warn("parse quest-giver details failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			giver := session.modernGUIDForLegacyLocked(quest.Giver)
			informUnit := session.modernGUIDForLegacyLocked(quest.InformUnit)
			if session.questRewardChoices == nil {
				session.questRewardChoices = make(map[uint32][6]uint32)
			}
			session.questRewardChoices[quest.QuestID] = modernworld.QuestRewardChoices(quest.Rewards)
			session.worldMu.Unlock()
			s.log.Debug("quest-giver details", "account", session.legacy.Username, "quest", quest.QuestID, "auto", quest.AutoLaunched)
			body := modernworld.EncodeQuestGiverQuestDetails(giver, informUnit, quest)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGQuestGiverQuestDetails, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGNpcTextUpdate:
			body, records, err := modernworld.TranslateNpcTextResponseWithBroadcasts(packet.Body)
			if err != nil {
				s.log.Warn("translate npc-text failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			if session.broadcastTexts == nil {
				session.broadcastTexts = make(map[uint32]modernworld.BroadcastTextRecord)
			}
			for _, record := range records {
				session.broadcastTexts[record.ID] = record
			}
			session.worldMu.Unlock()
			s.log.Debug("npc text response", "account", session.legacy.Username, "broadcasts", len(records))
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGQueryNpcText, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGQuestQueryResponse:
			body, questID, choices, err := modernworld.TranslateQuestQueryResponseWithChoices(packet.Body)
			if err != nil {
				s.log.Warn("translate quest query failed", "account", session.legacy.Username, "error", err)
				continue
			}
			itemObjectives, objectiveErr := modernworld.ParseLegacyQuestItemObjectives(packet.Body)
			if objectiveErr != nil {
				s.log.Warn("parse quest item objectives failed", "account", session.legacy.Username, "quest", questID, "error", objectiveErr)
				continue
			}
			stateOnly := false
			if stateQuest, stateObjectiveOnly, stateErr := modernworld.HasLegacyQuestStateObjective(packet.Body); stateErr != nil {
				s.log.Warn("parse quest state-only objective failed", "account", session.legacy.Username, "quest", questID, "error", stateErr)
			} else {
				stateOnly = stateObjectiveOnly
				// Quests with a state objective (including mixed exploration)
				// must mark their synthesized StorageIndex 0
				// AreaTrigger objective via QuestLog StateFlags bit 0x100 once the
				// slot is COMPLETE; remember them so the log descriptor writer can
				// apply that bit.
				session.worldMu.Lock()
				if stateObjectiveOnly {
					if session.questStateOnly == nil {
						session.questStateOnly = make(map[uint32]struct{})
					}
					session.questStateOnly[stateQuest] = struct{}{}
				} else {
					delete(session.questStateOnly, stateQuest)
				}
				session.worldMu.Unlock()
			}
			session.worldMu.Lock()
			if session.questRewardChoices == nil {
				session.questRewardChoices = make(map[uint32][6]uint32)
			}
			if session.questItemObjectives == nil {
				session.questItemObjectives = make(map[uint32][]modernworld.QuestItemObjectiveInfo)
			}
			session.questRewardChoices[questID] = choices
			session.questItemObjectives[questID] = append([]modernworld.QuestItemObjectiveInfo(nil), itemObjectives...)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGQueryQuestInfoResponse, Body: body}); err != nil {
				return
			}
			// The ActivePlayer create predates the client's quest-template
			// queries, so a state-only quest that was already COMPLETE at login
			// never carried the objective bit. Now that this template is known,
			// push a corrected slot so the log row checks itself.
			if stateOnly {
				if err := s.repushQuestLogObjectiveFlag(session, questID); err != nil {
					s.log.Warn("repush quest-log objective flag failed", "account", session.legacy.Username, "quest", questID, "error", err)
					return
				}
			}
			s.log.Debug("quest template objective mode", "account", session.legacy.Username, "quest", questID, "synthetic_area_trigger", stateOnly)
		case legacyworld.SMSGQuestPOIQueryResponse:
			quests, err := modernworld.ParseLegacyQuestPOIResponse(packet.Body)
			if err != nil {
				s.log.Warn("parse quest POI response failed", "account", session.legacy.Username, "error", err)
				continue
			}
			s.log.Debug("quest POI response", "account", session.legacy.Username, "quests", len(quests))
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGQuestPOIQueryResponse, Body: modernworld.EncodeQuestPOIResponse(quests)}); err != nil {
				return
			}
		case legacyworld.SMSGChat, legacyworld.SMSGGMMessageChat:
			var (
				message modernworld.LegacyChatMessage
				err     error
			)
			if packet.Opcode == legacyworld.SMSGGMMessageChat {
				message, err = modernworld.ParseLegacyGMChatMessage(packet.Body)
			} else {
				message, err = modernworld.ParseLegacyChatMessage(packet.Body)
			}
			if err != nil {
				s.log.Warn("parse legacy chat failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			sender := session.modernGUIDForLegacyLocked(message.Sender)
			receiver := session.modernGUIDForLegacyLocked(message.Receiver)
			addonAllowed := modernworld.PrepareLegacyAddonMessage(&message, session.addonPrefixes)
			if message.SenderName == "" {
				message.SenderName = session.playerNames[message.Sender]
			}
			if message.ReceiverName == "" {
				message.ReceiverName = session.playerNames[message.Receiver]
				if message.ReceiverName == "" && message.Type == 9 { // CHAT_MSG_WHISPER_INFORM
					message.ReceiverName = session.lastWhisperTarget
				}
			}
			realmAddress := realm.Address(session.selectedRealm.ID)
			needsName := addonAllowed && message.AddonPrefix == "" && message.Type == 7 &&
				message.Sender != 0 && uint16(message.Sender>>48) == 0 && message.SenderName == ""
			queryName := false
			legacyConnForName := session.legacyWorld
			if needsName {
				queryName = session.queueNamedChatLocked(message)
			}
			session.worldMu.Unlock()
			if !addonAllowed {
				continue
			}
			if needsName {
				if queryName && legacyConnForName != nil {
					if err := legacyConnForName.WritePacket(legacyworld.CMSGNameQuery, modernworld.EncodeLegacyNameQuery(message.Sender)); err != nil {
						return
					}
				}
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGChat, Body: modernworld.EncodeChatMessage(sender, receiver, message, realmAddress)}); err != nil {
				return
			}
		case legacyworld.SMSGAreaTriggerMessage:
			body, err := modernworld.TranslateLegacyAreaTriggerMessage(packet.Body)
			if err != nil {
				s.log.Warn("translate legacy area-trigger-message failed", "account", session.legacy.Username, "error", err)
				continue
			}
			s.log.Info("area trigger message", "account", session.legacy.Username, "text", string(packet.Body[4:len(packet.Body)-1]))
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGPrintNotification, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGPrintNotification:
			body, err := modernworld.TranslateLegacyPrintNotification(packet.Body)
			if err != nil {
				s.log.Warn("translate legacy print-notification failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGPrintNotification, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGPhaseShiftChange:
			session.worldMu.Lock()
			player := session.currentCharacter
			session.worldMu.Unlock()
			body, err := modernworld.TranslateLegacyPhaseShiftChange(packet.Body, player)
			if err != nil {
				s.log.Warn("translate legacy phase-shift change failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGPhaseShiftChange, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGChatPlayerNotFound:
			name, err := modernworld.ParseLegacyChatPlayerNotFound(packet.Body)
			if err != nil {
				s.log.Warn("parse legacy player-not-found failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			addonWhisper := session.consumeAddonWhisperFailureLocked(name, time.Now())
			session.worldMu.Unlock()
			if addonWhisper {
				s.log.Debug("suppress addon whisper player-not-found", "account", session.legacy.Username, "name", name)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGChatPlayerNotFound, Body: modernworld.EncodeChatPlayerNotFound(name)}); err != nil {
				return
			}
		case legacyworld.SMSGChannelList:
			list, err := modernworld.ParseLegacyChannelList(packet.Body)
			if err != nil {
				s.log.Warn("parse legacy channel list failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			body, err := modernworld.EncodeChannelList(list, realm.Address(session.selectedRealm.ID), session.modernGUIDForLegacyLocked)
			session.worldMu.Unlock()
			if err != nil {
				s.log.Warn("encode channel list failed", "account", session.legacy.Username, "error", err)
				continue
			}
			s.log.Debug("channel list", "account", session.legacy.Username, "name", list.Name, "display", list.Display, "members", len(list.Members), "bytes", len(body))
			if err := session.sendRealm(modernworld.Packet{Opcode: modernworld.SMSGChannelList, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGChannelNotify:
			session.worldMu.Lock()
			mapID := session.currentMapID
			zoneID := session.currentZoneID
			opcode, body, handled, err := modernworld.TranslateLegacyChannelNotify(packet.Body, mapID, zoneID, modernworld.ChannelPlayerLookup{
				GUID: session.modernGUIDForLegacyLocked,
				Name: func(id uint64) string { return session.playerNames[id] },
			})
			session.worldMu.Unlock()
			if err != nil {
				s.log.Warn("translate legacy channel-notify failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if opcode == 0 {
				// Remaining ChatNotify types stay dropped. Owner and the two
				// announcement results are forwarded above as SMSG_CHANNEL_NOTIFY.
				continue
			}
			if !handled {
				s.log.Debug("legacy channel-notify type pending translation", "account", session.legacy.Username, "bytes", len(packet.Body))
				continue
			}
			// Match Hermes ChannelNotifyJoined/Left's default realm connection.
			// This alone does not establish why a client hides the join notice.
			s.log.Debug("channel notify", "account", session.legacy.Username, "opcode", opcode, "map", mapID, "zone", zoneID, "bytes", len(body), "legacy_hex", fmt.Sprintf("%x", packet.Body), "modern_hex", fmt.Sprintf("%x", body))
			if err := session.sendRealm(modernworld.Packet{Opcode: opcode, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGQuestGiverRequestItems:
			quest, err := modernworld.ParseLegacyQuestGiverRequestItems(packet.Body)
			if err != nil {
				s.log.Warn("parse quest request-items failed", "account", session.legacy.Username, "error", err)
				continue
			}
			s.log.Debug("quest request-items status", "account", session.legacy.Username, "quest", quest.QuestID, "status_flags", quest.StatusFlags, "collect_count", len(quest.Collect), "money", quest.MoneyToGet)
			session.worldMu.Lock()
			giver := session.modernGUIDForLegacyLocked(quest.Giver)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGQuestGiverRequestItems, Body: modernworld.EncodeQuestGiverRequestItems(giver, quest)}); err != nil {
				return
			}
		case legacyworld.SMSGQuestGiverOfferReward:
			quest, err := modernworld.ParseLegacyQuestGiverOfferReward(packet.Body)
			if err != nil {
				s.log.Warn("parse quest offer-reward failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			giver := session.modernGUIDForLegacyLocked(quest.Giver)
			if session.questRewardChoices == nil {
				session.questRewardChoices = make(map[uint32][6]uint32)
			}
			session.questRewardChoices[quest.QuestID] = modernworld.QuestRewardChoices(quest)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGQuestGiverOfferReward, Body: modernworld.EncodeQuestGiverOfferReward(giver, quest)}); err != nil {
				return
			}
		case legacyworld.SMSGQuestGiverQuestComplete:
			quest, err := modernworld.ParseLegacyQuestGiverQuestComplete(packet.Body)
			if err != nil {
				s.log.Warn("parse quest complete failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			delete(session.questRewardChoices, quest.QuestID)
			session.worldMu.Unlock()
			s.log.Debug("quest complete", "account", session.legacy.Username, "quest", quest.QuestID, "xp", quest.XP, "money", quest.Money, "reopen", false)
			if err := s.markQuestCompleted(session, quest.QuestID); err != nil {
				s.log.Warn("update completed-quest mark failed", "account", session.legacy.Username, "quest", quest.QuestID, "error", err)
				return
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGQuestGiverQuestComplete, Body: modernworld.EncodeQuestGiverQuestComplete(quest)}); err != nil {
				return
			}
		case legacyworld.SMSGQuestGiverStatus:
			legacyGUID, status, err := modernworld.TranslateQuestGiverStatus(packet.Body)
			if err != nil {
				s.log.Warn("translate quest-giver status failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			guid := session.modernGUIDForLegacyLocked(legacyGUID)
			session.worldMu.Unlock()
			s.log.Debug("quest-giver status", "account", session.legacy.Username, "legacy", fmt.Sprintf("0x%x", legacyGUID), "status", status)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGQuestGiverStatus, Body: modernworld.EncodeQuestGiverStatus(guid, status)}); err != nil {
				return
			}
		case legacyworld.SMSGQuestGiverStatusMultiple:
			legacyGUIDs, statuses, err := modernworld.TranslateQuestGiverStatusMultiple(packet.Body)
			if err != nil {
				s.log.Warn("translate quest-giver status multiple failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			guids := make([]modernworld.GUID128, len(legacyGUIDs))
			for index, legacyGUID := range legacyGUIDs {
				guids[index] = session.modernGUIDForLegacyLocked(legacyGUID)
			}
			session.worldMu.Unlock()
			s.log.Debug("quest-giver status multiple", "account", session.legacy.Username, "entries", len(guids))
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGQuestGiverStatusMultiple, Body: modernworld.EncodeQuestGiverStatusMultiple(guids, statuses)}); err != nil {
				return
			}
		case legacyworld.SMSGQueryQuestsCompletedResponse:
			count, err := s.syncCompletedQuests(session, packet.Body)
			if err != nil {
				s.log.Warn("parse completed-quests response failed", "account", session.legacy.Username, "error", err)
				continue
			}
			s.log.Debug("completed quests synchronized", "account", session.legacy.Username, "quests", count)
		case legacyworld.SMSGQuestGiverQuestFailed:
			questID, reason, err := modernworld.ParseLegacyQuestGiverQuestFailed(packet.Body)
			if err != nil {
				s.log.Warn("parse quest-giver-failed failed", "account", session.legacy.Username, "error", err)
				continue
			}
			s.log.Debug("quest giver failed", "account", session.legacy.Username, "quest", questID, "reason", reason)
			body := modernworld.EncodeQuestGiverQuestFailed(questID, reason)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGQuestGiverQuestFailed, Body: body}); err != nil {
				return
			}
		case legacyworld.MSGQuestPushResult:
			sender, result, err := modernworld.ParseLegacyQuestPushResult(packet.Body)
			if err != nil {
				s.log.Warn("parse quest-push-result failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			guid := session.modernGUIDForLegacyLocked(sender)
			session.worldMu.Unlock()
			modernResult := modernworld.ModernQuestPushReason(result)
			s.log.Debug("quest push result", "account", session.legacy.Username, "sender", fmt.Sprintf("0x%x", sender),
				"legacy_result", result, "modern_result", modernResult)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGQuestPushResult, Body: modernworld.EncodeQuestPushResult(guid, result)}); err != nil {
				return
			}
		case legacyworld.SMSGQuestConfirmAccept:
			questID, title, initiator, err := modernworld.ParseLegacyQuestConfirmAccept(packet.Body)
			if err != nil {
				s.log.Warn("parse quest-confirm-accept failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			guid := session.modernGUIDForLegacyLocked(initiator)
			session.worldMu.Unlock()
			s.log.Debug("quest confirm accept", "account", session.legacy.Username, "quest", questID, "initiator", fmt.Sprintf("0x%x", initiator))
			body := modernworld.EncodeQuestConfirmAccept(questID, guid, title)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGQuestConfirmAccept, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGQuestGiverInvalidQuest:
			reason, err := modernworld.ParseLegacyQuestGiverInvalidQuest(packet.Body)
			if err != nil {
				s.log.Warn("parse quest-giver-invalid failed", "account", session.legacy.Username, "error", err)
				continue
			}
			s.log.Debug("quest giver invalid", "account", session.legacy.Username, "reason", reason)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGQuestGiverInvalidQuest, Body: modernworld.EncodeQuestGiverInvalidQuest(reason)}); err != nil {
				return
			}
		case legacyworld.SMSGQuestLogFull:
			if err := modernworld.ParseLegacyQuestLogFull(packet.Body); err != nil {
				s.log.Warn("parse quest-log-full failed", "account", session.legacy.Username, "error", err)
				continue
			}
			s.log.Debug("quest log full", "account", session.legacy.Username)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGQuestLogFull}); err != nil {
				return
			}
		case legacyworld.SMSGQuestUpdateComplete, legacyworld.SMSGQuestUpdateFailed, legacyworld.SMSGQuestUpdateFailedTimer:
			questID, err := modernworld.ParseLegacyQuestUpdateID(packet.Body)
			if err != nil {
				s.log.Warn("parse quest-update failed", "account", session.legacy.Username, "opcode", packet.Opcode, "error", err)
				continue
			}
			opcode := modernworld.SMSGQuestUpdateComplete
			switch packet.Opcode {
			case legacyworld.SMSGQuestUpdateFailed:
				opcode = modernworld.SMSGQuestUpdateFailed
			case legacyworld.SMSGQuestUpdateFailedTimer:
				opcode = modernworld.SMSGQuestUpdateFailedTimer
			}
			s.log.Debug("quest update", "account", session.legacy.Username, "quest", questID, "legacy_opcode", packet.Opcode)
			if err := session.sendInstance(modernworld.Packet{Opcode: opcode, Body: modernworld.EncodeQuestUpdateID(questID)}); err != nil {
				return
			}
		case legacyworld.SMSGQuestUpdateAddKill:
			credit, err := modernworld.ParseLegacyQuestUpdateAddKill(packet.Body)
			if err != nil {
				s.log.Warn("parse quest-add-kill failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			victim := session.modernGUIDForLegacyLocked(credit.Victim)
			session.worldMu.Unlock()
			s.log.Debug("quest add-kill", "account", session.legacy.Username, "quest", credit.QuestID, "object", credit.ObjectID, "count", credit.Count, "required", credit.Required)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGQuestUpdateAddCredit, Body: modernworld.EncodeQuestUpdateAddCredit(victim, credit)}); err != nil {
				return
			}
		case legacyworld.SMSGQuestUpdateAddItem:
			if len(packet.Body) != 0 {
				s.log.Warn("legacy quest-add-item has unexpected payload", "account", session.legacy.Username, "bytes", len(packet.Body))
			}
			session.worldMu.Lock()
			session.questItemSyncPending = true
			session.worldMu.Unlock()
		case legacyworld.SMSGWeather:
			body, err := modernworld.TranslateWeather(packet.Body)
			if err != nil {
				s.log.Warn("translate weather failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGWeather, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGSetProficiency:
			body, err := modernworld.TranslateSetProficiency(packet.Body)
			if err != nil {
				s.log.Warn("translate set-proficiency failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSetProficiency, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGGameObjectQueryResponse:
			entry := uint32(0)
			if len(packet.Body) >= 4 {
				rawEntry := binary.LittleEndian.Uint32(packet.Body[:4])
				entry = rawEntry &^ 0x80000000
			}
			session.worldMu.Lock()
			guid := session.goQueryGUIDs[entry]
			delete(session.goQueryGUIDs, entry)
			session.worldMu.Unlock()
			body, err := modernworld.TranslateGameObjectQueryResponse(packet.Body, guid)
			if err != nil {
				s.log.Warn("translate game-object query failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGQueryGameObject, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGNameQueryResponse:
			identity, found, err := modernworld.ParseLegacyNameIdentity(packet.Body)
			if err != nil {
				s.log.Warn("read name-query identity failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			realmAddress := realm.Address(session.selectedRealm.ID)
			override, hasOverride := session.pendingPlayerNameModern[identity.GUID]
			if hasOverride {
				delete(session.pendingPlayerNameModern, identity.GUID)
			} else {
				override = session.modernGUIDForLegacyLocked(identity.GUID)
			}
			if found {
				session.rememberPlayerNameLocked(identity.GUID, identity.Name)
				if session.playerIdentities == nil {
					session.playerIdentities = make(map[uint64]modernworld.LegacyNameIdentity)
				}
				session.playerIdentities[identity.GUID] = identity
			}
			pendingChats := session.takeNamedChatsLocked(identity.GUID)
			session.worldMu.Unlock()
			body, err := modernworld.TranslateNameQueryResponseForGUID(packet.Body, realmAddress, override,
				func(guid uint64) modernworld.GUID128 { return s.socialWowAccountGUID(session, guid) },
				func(guid uint64) modernworld.GUID128 { return s.socialBNetAccountGUID(session, guid) })
			if err != nil {
				s.log.Warn("translate name query failed", "account", session.legacy.Username, "error", err)
				continue
			}
			name := identity.Name
			s.log.Debug("player-name query response", "account", session.legacy.Username, "legacy_guid", identity.GUID, "modern_guid", override, "found", found, "name", name, "legacy_hex", hex.EncodeToString(packet.Body), "modern_hex", hex.EncodeToString(body))
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGQueryPlayerNames, Body: body}); err != nil {
				return
			}
			for _, pending := range pendingChats {
				if found {
					pending.SenderName = name
				}
				session.worldMu.Lock()
				sender := session.modernGUIDForLegacyLocked(pending.Sender)
				receiver := session.modernGUIDForLegacyLocked(pending.Receiver)
				if pending.ReceiverName == "" {
					pending.ReceiverName = session.playerNames[pending.Receiver]
				}
				session.worldMu.Unlock()
				if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGChat, Body: modernworld.EncodeChatMessage(sender, receiver, pending, realmAddress)}); err != nil {
					return
				}
			}
		case legacyworld.SMSGPetNameQueryResponse:
			response, err := modernworld.ParseLegacyPetNameResponse(packet.Body)
			if err != nil {
				s.log.Warn("translate pet-name query failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			guids := session.pendingPetNameGUIDs[response.PetNumber]
			delete(session.pendingPetNameGUIDs, response.PetNumber)
			session.worldMu.Unlock()
			if len(guids) == 0 {
				s.log.Warn("pet-name response has no pending GUID", "account", session.legacy.Username, "number", response.PetNumber)
				continue
			}
			s.log.Debug("pet-name response", "account", session.legacy.Username, "number", response.PetNumber, "name", response.Name, "queries", len(guids))
			for _, guid := range guids {
				body, encodeErr := modernworld.EncodeQueryPetNameResponse(guid, response)
				if encodeErr != nil {
					s.log.Warn("encode pet-name query failed", "account", session.legacy.Username, "error", encodeErr)
					continue
				}
				if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGQueryPetName, Body: body}); err != nil {
					return
				}
			}
		case legacyworld.SMSGEmote:
			legacyGUID, emoteID, err := modernworld.ParseLegacyEmote(packet.Body)
			if err != nil {
				s.log.Warn("parse emote failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			guid := session.modernGUIDForLegacyLocked(legacyGUID)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGEmote, Body: modernworld.EncodeEmote(guid, emoteID)}); err != nil {
				return
			}
		case legacyworld.SMSGTextEmote:
			legacyGUID, emoteID, sound, err := modernworld.ParseLegacyTextEmote(packet.Body)
			if err != nil {
				s.log.Warn("parse text-emote failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			source := session.modernGUIDForLegacyLocked(legacyGUID)
			session.worldMu.Unlock()
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGTextEmote, Body: modernworld.EncodeTextEmote(source, emoteID, sound)}); err != nil {
				return
			}
		case legacyworld.SMSGStandStateUpdate:
			body, err := modernworld.TranslateStandStateUpdate(packet.Body)
			if err != nil {
				s.log.Warn("translate stand-state failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGStandStateUpdate, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGAuraUpdateAll, legacyworld.SMSGAuraUpdate:
			legacyGUID, auras, err := modernworld.ParseLegacyAuraUpdate(packet.Body, packet.Opcode == legacyworld.SMSGAuraUpdateAll)
			if err != nil {
				s.log.Warn("parse legacy aura-update failed", "account", session.legacy.Username, "opcode", packet.Opcode, "error", err)
				continue
			}
			session.worldMu.Lock()
			if packet.Opcode == legacyworld.SMSGAuraUpdateAll && legacyGUID == session.currentCharacter {
				session.playerAuras = append(session.playerAuras[:0], auras...)
			}
			if legacyGUID == session.currentCharacter {
				modernworld.MarkLocalPlayerAuraFlags(auras)
			}
			modernworld.RemapMountSpeedAuras(auras)
			for index := range auras {
				if auras[index].CastUnit.Low != 0 {
					auras[index].CastUnit = session.modernGUIDForLegacyLocked(auras[index].CastUnit.Low)
				}
				if auras[index].HasData && auras[index].VisualID == 0 {
					if visual := session.visualIDForSpellLocked(auras[index].SpellID); visual != 0 {
						auras[index].VisualID = visual
					}
				}
			}
			guid := session.modernGUIDForLegacyLocked(legacyGUID)
			created := session.activePlayerCreated
			current := session.currentCharacter
			mapID := session.currentMapID
			var stealthRemoved []uint32
			if legacyGUID == current {
				stealthRemoved = session.noteAuraSlotsLocked(auras, packet.Opcode == legacyworld.SMSGAuraUpdateAll)
			}
			_, _, _, ghost := modernworld.LegacyPlayerVitalState(session.objectFields[current])
			session.worldMu.Unlock()
			// 3.3.5 broadcasts SMSG_AURA_UPDATE for auras that land on any unit
			// the player can see, so a debuff such as Hunter's Mark on a creature
			// arrives targeted at that creature. legacy proxy forwards those to the
			// modern client; forwarding only the local player's own auras left the
			// creature's debuff icon invisible on the target frame.
			if !created {
				continue
			}
			if len(auras) == 0 && packet.Opcode != legacyworld.SMSGAuraUpdateAll {
				continue
			}
			spellIDs := make([]uint32, 0, len(auras))
			auraFlags := make([]uint16, 0, len(auras))
			for _, aura := range auras {
				if aura.HasData {
					spellIDs = append(spellIDs, aura.SpellID)
					auraFlags = append(auraFlags, aura.Flags)
				}
			}
			s.log.Debug("forward aura-update", "account", session.legacy.Username, "player", legacyGUID == current, "update_all", packet.Opcode == legacyworld.SMSGAuraUpdateAll, "map", mapID, "slots", len(auras), "spells", spellIDs, "flags", auraFlags, "ghost", ghost, "bytes", len(packet.Body))
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGAuraUpdate, Body: modernworld.EncodeAuraUpdate(guid, mapID, packet.Opcode == legacyworld.SMSGAuraUpdateAll, auras)}); err != nil {
				return
			}
			if len(stealthRemoved) > 0 {
				cooldowns := make([]modernworld.SpellCooldown, 0, len(stealthRemoved))
				for _, spellID := range stealthRemoved {
					cooldowns = append(cooldowns, modernworld.SpellCooldown{SpellID: spellID, ForcedCooldown: modernworld.StealthCooldownMS})
				}
				s.log.Debug("stealth cooldown", "account", session.legacy.Username, "spells", stealthRemoved, "duration", modernworld.StealthCooldownMS)
				if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSpellCooldown, Body: modernworld.EncodeSpellCooldown(guid, 0, cooldowns)}); err != nil {
					return
				}
			}
		case uint16(legacyworld.MSGCorpseQuery):
			loc, err := modernworld.ParseLegacyCorpseQuery(packet.Body)
			if err != nil {
				s.log.Warn("parse legacy corpse-query failed", "account", session.legacy.Username, "error", err)
				continue
			}
			s.log.Debug("send corpse location", "account", session.legacy.Username, "valid", loc.Valid, "map", loc.MapID, "actual_map", loc.ActualMapID, "x", loc.X, "y", loc.Y, "z", loc.Z)
			if err := session.sendCorpseLocation(loc); err != nil && err != net.ErrClosed {
				return
			}
		case legacyworld.SMSGDeathReleaseLoc:
			body, err := modernworld.TranslateDeathReleaseLoc(packet.Body)
			if err != nil {
				s.log.Warn("translate death-release-loc failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendRealm(modernworld.Packet{Opcode: modernworld.SMSGDeathReleaseLoc, Body: body}); err != nil && err != net.ErrClosed {
				return
			}
		case legacyworld.SMSGCorpseReclaimDelay:
			s.log.Debug("legacy corpse-reclaim-delay", "account", session.legacy.Username, "bytes", len(packet.Body), "hex", hex.EncodeToString(packet.Body))
			session.worldMu.Lock()
			created := session.activePlayerCreated
			session.worldMu.Unlock()
			if !created {
				s.log.Debug("defer corpse-reclaim-delay until active player exists", "account", session.legacy.Username)
				continue
			}
			body, err := modernworld.TranslateCorpseReclaimDelay(packet.Body)
			if err != nil {
				s.log.Warn("translate corpse-reclaim-delay failed", "account", session.legacy.Username, "error", err)
				continue
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGCorpseReclaimDelay, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGSpiritHealerConfirm:
			legacyHealer, err := modernworld.ParseLegacyPackedGUID(packet.Body)
			if err != nil {
				s.log.Warn("parse legacy spirit-healer confirm failed", "account", session.legacy.Username, "error", err)
				continue
			}
			// Build 54261 no longer accepts the legacy confirmation packet. legacy proxy's
			// WotLK Classic path acknowledges it directly to the legacy server so
			// selecting the gossip option completes resurrection without exposing
			// the incompatible packet to the modern client.
			s.log.Debug("legacy spirit-healer confirm; auto activate", "account", session.legacy.Username,
				"legacy", fmt.Sprintf("0x%x", legacyHealer))
			if err := legacyConn.WritePacket(legacyworld.CMSGSpiritHealerActivate, modernworld.EncodeLegacyUnpackedGUID(legacyHealer)); err != nil {
				return
			}
		case legacyworld.SMSGTransferPending:
			session.worldMu.Lock()
			waitingAck := session.waitingForWorldPortAck
			session.worldMu.Unlock()
			if waitingAck {
				s.log.Debug("skipping transfer-pending because a world-port ack is already outstanding", "account", session.legacy.Username)
				continue
			}
			pending, err := modernworld.ParseLegacyTransferPending(packet.Body)
			if err != nil {
				s.log.Warn("parse legacy transfer-pending failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			session.waitingForNewWorld = true
			session.worldMu.Unlock()
			if err := session.sendRealm(modernworld.Packet{Opcode: modernworld.SMSGTransferPending, Body: modernworld.EncodeTransferPending(pending)}); err != nil {
				return
			}
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSuspendToken, Body: modernworld.EncodeSuspendToken(modernworld.DefaultSuspendToken())}); err != nil {
				return
			}
		case legacyworld.SMSGTransferAborted:
			aborted, err := modernworld.ParseLegacyTransferAborted(packet.Body)
			if err != nil {
				s.log.Warn("parse legacy transfer-aborted failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			session.waitingForNewWorld = false
			session.worldMu.Unlock()
			if err := session.sendRealm(modernworld.Packet{Opcode: modernworld.SMSGTransferAborted, Body: modernworld.EncodeTransferAborted(aborted)}); err != nil {
				return
			}
		case legacyworld.SMSGNewWorld:
			worldInfo, err := modernworld.ParseLegacyNewWorld(packet.Body)
			if err != nil {
				s.log.Warn("parse legacy new-world failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			waiting := session.waitingForNewWorld
			if waiting {
				session.resetWorldObjectsLocked(uint16(worldInfo.MapID))
				session.waitingForNewWorld = false
				session.waitingForWorldPortAck = true
			} else {
				session.currentMapID = uint16(worldInfo.MapID)
			}
			session.worldMu.Unlock()
			if !waiting {
				s.log.Debug("skipping new-world because no transfer-pending is outstanding", "account", session.legacy.Username, "map", worldInfo.MapID)
				continue
			}
			if err := session.sendRealm(modernworld.Packet{Opcode: modernworld.SMSGNewWorld, Body: modernworld.EncodeNewWorld(worldInfo)}); err != nil {
				return
			}
			if worldInfo.MapID > 1 {
				if modernworld.IsLegacyInstanceMap(worldInfo.MapID) {
					if err := session.sendRealm(modernworld.Packet{Opcode: modernworld.SMSGUpdateLastInstance, Body: modernworld.EncodeUpdateLastInstance(worldInfo.MapID)}); err != nil {
						return
					}
				}
				if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGResumeToken, Body: modernworld.EncodeSuspendToken(modernworld.DefaultSuspendToken())}); err != nil {
					return
				}
			}
			if err := session.sendMapDifficulty(); err != nil {
				return
			}
		case uint16(legacyworld.MSGMoveTeleportAck):
			legacyGUID, counter, move, err := modernworld.ParseLegacyMoveTeleportAck(packet.Body)
			if err != nil {
				s.log.Warn("parse legacy move-teleport-ack failed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			mapID := session.currentMapID
			mover, moverKnown := session.objectGUIDs[legacyGUID]
			transport, transportKnown := session.objectGUIDs[move.TransportGUID]
			session.worldMu.Unlock()
			if !moverKnown {
				mover = modernworld.ModernGUIDForLegacy(legacyGUID, mapID)
			}
			if move.TransportGUID != 0 && !transportKnown {
				transport = modernworld.ModernGUIDForLegacy(move.TransportGUID, mapID)
			}
			body := modernworld.EncodeMoveTeleport(modernworld.MoveTeleportFromLegacy(mover, counter, move, transport))
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGMoveTeleport, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGUpdateObject, legacyworld.SMSGCompressedUpdateObject:
			batch, err := modernworld.DecodeLegacyUpdateObject(packet.Opcode, packet.Body)
			if err != nil {
				s.log.Warn("parse legacy update-object failed", "account", session.legacy.Username, "opcode", packet.Opcode, "error", err)
				continue
			}
			session.worldMu.Lock()
			currentCharacter := session.currentCharacter
			mapID := session.currentMapID
			gameAccountID := session.gameAccountID
			hasActionButtons := session.hasActionButtons
			completedQuestBlocks := append([]uint64(nil), session.completedQuestBlocks...)
			stateOnlyQuests := make(map[uint32]struct{}, len(session.questStateOnly))
			for stateQuest := range session.questStateOnly {
				stateOnlyQuests[stateQuest] = struct{}{}
			}
			initWorldStates := append([]byte(nil), session.initWorldStates...)
			virtualRealm := realm.Address(session.selectedRealm.ID)
			session.worldMu.Unlock()
			createCount := 0
			playerCreateCount := 0
			playerCreateSent := false
			publicCreateSent := 0
			valuesFieldsSent := 0
			valuesFieldsDeferred := 0
			valuesUpdatesPending := 0
			createTime := time.Now()
			outOfRange := make([]modernworld.GUID128, 0)
			updateBodies := make([][]byte, 0, len(batch.Updates))
			visibleAfterSend := make([]uint64, 0, len(batch.Updates))
			creatureQueriesAfterSend := make([]uint32, 0)
			mirrorImageAfterSend := make([]uint64, 0)
			flushObjectUpdates := func() error {
				if len(updateBodies) == 0 && len(mirrorImageAfterSend) == 0 {
					return nil
				}
				if len(updateBodies) != 0 {
					merged, mergeErr := modernworld.MergeUpdateObjectBodies(mapID, updateBodies...)
					if mergeErr != nil {
						s.log.Warn("cannot merge translated object updates; sending individually",
							"account", session.legacy.Username,
							"count", len(updateBodies),
							"error", mergeErr)
						for _, body := range updateBodies {
							if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGUpdateObject, Body: body}); err != nil {
								return err
							}
						}
					} else {
						s.log.Debug("send translated object batch as one update",
							"account", session.legacy.Username,
							"count", len(updateBodies),
							"bytes", len(merged))
						if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGUpdateObject, Body: merged}); err != nil {
							return err
						}
					}
					session.worldMu.Lock()
					for _, legacyGUID := range visibleAfterSend {
						session.markObjectVisibleLocked(legacyGUID)
					}
					session.worldMu.Unlock()
					if err := session.flushEncounterFrames(); err != nil {
						return err
					}
					if err := s.flushReadyObjectSpells(session); err != nil {
						return err
					}
					for _, entry := range creatureQueriesAfterSend {
						if err := s.queryCreatureTemplate(session, entry); err != nil {
							return err
						}
					}
					updateBodies = nil
					visibleAfterSend = nil
					creatureQueriesAfterSend = nil
				}
				for _, guid := range mirrorImageAfterSend {
					if err := s.requestMirrorImageData(session, guid); err != nil {
						return err
					}
				}
				mirrorImageAfterSend = nil
				return nil
			}
			for _, update := range batch.Updates {
				if update.Type != modernworld.LegacyUpdateFarObjects {
					continue
				}
				for _, legacyGUID := range update.GUIDs {
					if legacyGUID == currentCharacter {
						continue
					}
					session.worldMu.Lock()
					modernGUID, known := session.objectGUIDs[legacyGUID]
					delete(session.objectGUIDs, legacyGUID)
					session.forgetObjectVisibleLocked(legacyGUID)
					delete(session.objectTypes, legacyGUID)
					delete(session.objectFields, legacyGUID)
					delete(session.objectPositions, legacyGUID)
					delete(session.queriedMirrorImages, legacyGUID)
					session.worldMu.Unlock()
					if !known {
						modernGUID = modernworld.ModernGUIDForLegacy(legacyGUID, mapID)
					}
					outOfRange = append(outOfRange, modernGUID)
				}
			}
			if len(outOfRange) != 0 {
				body := modernworld.EncodeObjectRemovals(mapID, nil, outOfRange)
				if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGUpdateObject, Body: body}); err != nil {
					return
				}
			}
			orderedUpdates := make([]modernworld.LegacyObjectUpdate, 0, len(batch.Updates))
			for _, update := range batch.Updates {
				if (update.Type == modernworld.LegacyUpdateCreateObject1 || update.Type == modernworld.LegacyUpdateCreateObject2) &&
					update.ObjectType == 4 && update.GUID == currentCharacter {
					orderedUpdates = append([]modernworld.LegacyObjectUpdate{update}, orderedUpdates...)
					continue
				}
				orderedUpdates = append(orderedUpdates, update)
			}
			for _, update := range orderedUpdates {
				if update.Type == modernworld.LegacyUpdateMovement {
					if err := flushObjectUpdates(); err != nil {
						return
					}
					session.worldMu.Lock()
					objectType, known := session.objectTypes[update.GUID]
					var outputs []modernworld.Packet
					var movementErr error
					if known {
						outputs, movementErr = modernworld.EncodeObjectMovement(update, objectType, session.modernGUIDForLegacyLocked)
					}
					if known && movementErr == nil && update.Movement != nil {
						if session.objectPositions == nil {
							session.objectPositions = make(map[uint64][3]float32)
						}
						session.objectPositions[update.GUID] = [3]float32{update.Movement.X, update.Movement.Y, update.Movement.Z}
					}
					session.worldMu.Unlock()
					if !known || movementErr != nil {
						s.log.Debug("standalone movement deferred", "guid", update.GUID, "object_type", objectType, "known", known, "error", movementErr)
					} else {
						for _, out := range outputs {
							if err := session.sendInstance(out); err != nil {
								return
							}
						}
					}
					continue
				}
				if update.Type == modernworld.LegacyUpdateValues {
					valuesFieldsDeferred += len(update.Values.Fields)
					diedInEncounter := false
					encounterFrames := 0
					session.worldMu.Lock()
					modernGUID, knownGUID := session.objectGUIDs[update.GUID]
					objectType, knownType := session.objectTypes[update.GUID]
					cachedFields := session.objectFields[update.GUID]
					if cachedFields == nil {
						cachedFields = make(map[int]uint32)
						session.objectFields[update.GUID] = cachedFields
					}
					var clearedArenaSlots []uint32
					if update.GUID == currentCharacter {
						for slot := uint32(0); slot < modernworld.ArenaSlotCount; slot++ {
							field := modernworld.LegacyPlayerArenaTeamInfo + int(slot)*modernworld.ArenaTeamInfoStride
							next, present := update.Values.Fields[field]
							if !present || next != 0 || cachedFields[field] == 0 {
								continue
							}
							delete(session.arenaTeams, cachedFields[field])
							clearedArenaSlots = append(clearedArenaSlots, slot)
						}
					}
					for field, value := range update.Values.Fields {
						cachedFields[field] = value
					}
					mergedFields := make(map[int]uint32, len(cachedFields))
					for field, value := range cachedFields {
						mergedFields[field] = value
					}
					if objectType == 5 {
						session.transportSync.Observe(update.GUID, mergedFields, time.Now())
					}
					if update.GUID == currentCharacter && modernworld.LegacyHealthDroppedToZero(update.Values.Fields) && session.encounterInProgress {
						diedInEncounter = true
						encounterFrames = len(session.encounterFrames)
					}
					session.worldMu.Unlock()
					for _, slot := range clearedArenaSlots {
						if err := session.sendInstance(modernworld.Packet{
							Opcode: modernworld.SMSGArenaTeamRoster,
							Body:   modernworld.EncodeEmptyArenaTeamRoster(slot),
						}); err != nil {
							return
						}
					}
					if diedInEncounter {
						s.log.Info("player died during instance encounter; server has not disengaged",
							"account", session.legacy.Username, "frames", encounterFrames)
					}
					if !knownGUID || !knownType {
						valuesUpdatesPending++
						continue
					}
					if update.GUID == currentCharacter {
						s.log.Debug("player values update", "account", session.legacy.Username, "fields", modernworld.FormatLegacyValuesFields(update.Values.Fields))
						powers := modernworld.PowerUpdatesFromValues(update.Values.Fields)
						if len(powers) != 0 {
							powerBody, powerErr := modernworld.EncodePowerUpdate(modernGUID, powers)
							if powerErr != nil {
								s.log.Warn("encode Values power update failed", "account", session.legacy.Username, "error", powerErr)
							} else if err := session.sendRealm(modernworld.Packet{Opcode: modernworld.SMSGPowerUpdate, Body: powerBody}); err != nil {
								return
							}
						}
					}
					if _, changed := modernworld.LegacyUnitTargetValue(update.Values.Fields); changed {
						target, _ := modernworld.LegacyUnitTargetValue(mergedFields)
						s.log.Debug("unit target values update",
							"account", session.legacy.Username,
							"unit", fmt.Sprintf("0x%x", update.GUID),
							"target", fmt.Sprintf("0x%x", target),
							"object_type", objectType)
					}
					body, translated, err := modernworld.EncodeValuesUpdate(update, modernworld.ValuesUpdateOptions{
						MapID: mapID, ObjectType: objectType, GUID: modernGUID,
						Active: update.GUID == currentCharacter, Fields: mergedFields,
						StateOnlyQuests: stateOnlyQuests,
					})
					if err != nil {
						s.log.Warn("translate object Values update failed", "account", session.legacy.Username,
							"guid", update.GUID, "object_type", objectType, "error", err)
						continue
					}
					if objectType == 3 && modernworld.IsLegacyMirrorImageCreature(3, update.Values.Fields) {
						mirrorImageAfterSend = append(mirrorImageAfterSend, update.GUID)
					}
					if len(body) == 0 {
						valuesUpdatesPending++
						continue
					}
					updateBodies = append(updateBodies, body)
					if objectType == 5 {
						s.log.Debug("game-object values update",
							"account", session.legacy.Username,
							"guid", fmt.Sprintf("0x%x", update.GUID),
							"changed", modernworld.FormatLegacyValuesFields(update.Values.Fields),
							"translated", translated,
							"bytes", len(body),
							"hex", fmt.Sprintf("%x", body))
					}
					valuesFieldsSent += translated
					valuesFieldsDeferred -= translated
					continue
				}
				if update.Type != modernworld.LegacyUpdateCreateObject1 && update.Type != modernworld.LegacyUpdateCreateObject2 {
					continue
				}
				createCount++
				if modernworld.IsRedundantInstancePortalSkull(update) {
					s.log.Debug("skip legacy portal skull; primary portal owns heroic attachment",
						"account", session.legacy.Username, "guid", fmt.Sprintf("0x%x", update.GUID))
					continue
				}
				if update.ObjectType == 7 {
					s.log.Debug("skip corpse create until 3.4.3 corpse layout is fixed",
						"account", session.legacy.Username,
						"guid", fmt.Sprintf("0x%x", update.GUID))
					continue
				}
				options := modernworld.ActivePlayerCreateOptions{
					MapID: mapID, VirtualRealm: virtualRealm, Now: createTime,
					GameAccountID: gameAccountID, OwnerGUID: currentCharacter, CompletedQuestBlocks: completedQuestBlocks,
					StateOnlyQuests: stateOnlyQuests,
				}
				session.worldMu.Lock()
				if update.GUID == currentCharacter && hasActionButtons {
					session.fillMultiCastButtonsLocked()
					options.ActionButtons = append([]int32(nil), session.actionButtons...)
				}
				if update.GUID == currentCharacter && session.hasRuneState {
					state := session.runeState
					options.Runes = &state
				}
				session.worldMu.Unlock()
				var (
					body []byte
					err  error
				)
				switch update.ObjectType {
				case 4:
					playerCreateCount++
					if update.GUID == currentCharacter {
						body, err = modernworld.EncodeActivePlayerCreate(update, options)
						health, unitFlags, playerFlags, ghost := modernworld.LegacyPlayerVitalState(update.Values.Fields)
						s.log.Debug("active player create", "account", session.legacy.Username, "health", health, "unit_flags", fmt.Sprintf("0x%x", unitFlags), "player_flags", fmt.Sprintf("0x%x", playerFlags), "local_flags", fmt.Sprintf("0x%x", modernworld.PlayerLocalFlags(update.Values.Fields)), "ghost", ghost)
						if ghost && err == nil {
							session.worldMu.Lock()
							session.corpseQueryPlayer = modernworld.ModernGUIDForCreate(update, mapID)
							session.worldMu.Unlock()
							s.log.Debug("ghost active player create, waiting for client corpse query", "account", session.legacy.Username)
						}
					} else {
						body, err = modernworld.EncodePlayerCreate(update, options)
					}
				case 3:
					if update.Movement != nil && update.Movement.Spline != nil {
						spline := update.Movement.Spline
						s.log.Debug("unit create spline",
							"account", session.legacy.Username,
							"guid", fmt.Sprintf("0x%x", update.GUID),
							"legacy_flags", fmt.Sprintf("0x%x", spline.Flags),
							"move_flags", fmt.Sprintf("0x%x", update.Movement.MoveFlags),
							"points", len(spline.Points),
							"time", spline.Time,
							"full_time", spline.FullTime,
							"mode", spline.Mode,
							"id", spline.ID,
							"vertical", spline.VerticalAccel,
							"fall_time", update.Movement.FallTime,
							"transport", fmt.Sprintf("0x%x", update.Movement.TransportGUID),
							"attack", fmt.Sprintf("0x%x", update.Movement.AttackTarget))
					}
					body, err = modernworld.EncodeUnitCreate(update, options)
				case 1:
					body, err = modernworld.EncodeItemCreate(update, options)
				case 2:
					body, err = modernworld.EncodeContainerCreate(update, options)
				case 5:
					if modernworld.IsLegacyTransportGameObject(update) {
						modernworld.StabilizeICCGunshipCreate(&update)
						// parent_rotation is the legacy GAMEOBJECT_ROTATION
						// quaternion the create forwards into
						// GameObjectData.ParentRotation, in raw field order.
						rotation, rotationPresent := modernworld.LegacyGameObjectRotation(update.Values.Fields)
						s.log.Debug("encode transport object create",
							"account", session.legacy.Username,
							"guid", fmt.Sprintf("0x%x", update.GUID),
							"update_flags", fmt.Sprintf("0x%03x", update.Movement.UpdateFlags),
							"parent_transport", fmt.Sprintf("0x%x", update.Movement.TransportGUID),
							"path_time", update.Movement.TransportPathTime,
							"entry", update.Values.Fields[3],
							"display_id", update.Values.Fields[8],
							"bytes_1", fmt.Sprintf("0x%08x", update.Values.Fields[17]),
							"rotation_present", rotationPresent,
							"parent_rotation", fmt.Sprintf("%08x %08x %08x %08x",
								rotation[0], rotation[1], rotation[2], rotation[3]))
					}
					body, err = modernworld.EncodeGameObjectCreate(update, options)
				case 6:
					body, err = modernworld.EncodeDynamicObjectCreate(update, options)
				case 7:
					body, err = modernworld.EncodeCorpseCreate(update, options)
				default:
					continue
				}
				if err != nil {
					s.log.Warn("translate public object create failed", "account", session.legacy.Username,
						"object_type", update.ObjectType, "guid", update.GUID, "error", err)
					continue
				}
				session.worldMu.Lock()
				session.rememberObjectCreateLocked(update, mapID)
				if update.ObjectType == 5 {
					session.transportSync.Observe(update.GUID, update.Values.Fields, createTime)
				}
				session.worldMu.Unlock()
				s.log.Debug("batch object create",
					"account", session.legacy.Username,
					"object_type", update.ObjectType,
					"guid", fmt.Sprintf("0x%x", update.GUID),
					"bytes", len(body),
					"self", update.GUID == currentCharacter)
				if update.ObjectType == 3 && update.Movement != nil && update.Movement.VehicleID != 0 {
					s.log.Debug("vehicle create",
						"account", session.legacy.Username,
						"guid", fmt.Sprintf("0x%x", update.GUID),
						"vehicle_id", update.Movement.VehicleID,
						"move_flags", fmt.Sprintf("0x%x", update.Movement.MoveFlags),
						"npc_flags", fmt.Sprintf("0x%x", modernworld.LegacyNPCFlags(update.Values.Fields)))
				}
				if update.ObjectType == 1 || update.ObjectType == 2 {
					if sock0, sock1, sock2 := modernworld.ItemSocketEnchantIDs(update.Values.Fields); sock0 != 0 || sock1 != 0 || sock2 != 0 {
						s.log.Debug("item create sockets",
							"account", session.legacy.Username,
							"guid", fmt.Sprintf("0x%x", update.GUID),
							"entry", update.Values.Fields[3],
							"sock", fmt.Sprintf("%d,%d,%d", sock0, sock1, sock2),
							"gems", fmt.Sprintf("%v", modernworld.ItemSocketGemItemIDs(update.Values.Fields)))
					}
				}
				if update.ObjectType == 5 {
					s.log.Debug("game-object create",
						"account", session.legacy.Username,
						"guid", fmt.Sprintf("0x%x", update.GUID),
						"bytes_1", fmt.Sprintf("0x%08x", update.Values.Fields[17]),
						"dynamic", fmt.Sprintf("0x%08x", update.Values.Fields[14]),
						"bytes", len(body),
						"hex", fmt.Sprintf("%x", body))
				}
				if update.ObjectType == 4 && update.GUID == currentCharacter {
					// Keep the complete translated ActivePlayer object for a direct
					// byte comparison with legacy proxy. This is intentionally scoped to the
					// single local-player create, not every object in the login batch.
					s.log.Debug("active-player create wire",
						"account", session.legacy.Username,
						"bytes", len(body),
						"hex", fmt.Sprintf("%x", body))
				}
				updateBodies = append(updateBodies, body)
				visibleAfterSend = append(visibleAfterSend, update.GUID)
				if update.ObjectType == 3 {
					entry := modernworld.CreatureEntryFromLegacy(update.GUID, update.Values.Fields)
					creatureQueriesAfterSend = append(creatureQueriesAfterSend, entry)
					if modernworld.IsLegacyMirrorImageCreature(3, update.Values.Fields) {
						mirrorImageAfterSend = append(mirrorImageAfterSend, update.GUID)
					}
				}
				publicCreateSent++
				if update.ObjectType == 4 && update.GUID == currentCharacter {
					playerCreateSent = true
				}
			}
			for _, body := range s.transportProgressReplays(session, mapID) {
				updateBodies = append(updateBodies, body)
			}
			if err := flushObjectUpdates(); err != nil {
				return
			}
			if err := session.sendCurrencyUpdate(); err != nil {
				return
			}
			if playerCreateSent {
				if err := s.finishActivePlayerLogin(session, mapID, currentCharacter, initWorldStates); err != nil {
					return
				}
			}
			s.log.Debug("legacy update-object decoded",
				"account", session.legacy.Username,
				"updates", len(batch.Updates),
				"creates", createCount,
				"out_of_range", len(outOfRange),
				"player_creates", playerCreateCount,
				"object_creates_sent", publicCreateSent,
				"values_fields_sent", valuesFieldsSent,
				"values_fields_deferred", valuesFieldsDeferred,
				"values_updates_pending", valuesUpdatesPending,
				"active_player_sent", playerCreateSent)
			if err := s.flushQuestItemProgress(session); err != nil {
				return
			}
		case legacyworld.SMSGItemEnchantTimeUpdate:
			update, err := modernworld.ParseLegacyItemEnchantTime(packet.Body)
			if err != nil {
				s.log.Warn("legacy item-enchant-time malformed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			item := session.modernGUIDForLegacyLocked(update.ItemGUID)
			owner := session.modernGUIDForLegacyLocked(update.OwnerGUID)
			session.worldMu.Unlock()
			s.log.Debug("item enchant time", "account", session.legacy.Username,
				"item", fmt.Sprintf("0x%x", update.ItemGUID), "slot", update.Slot, "duration", update.DurationLeft)
			body := modernworld.EncodeItemEnchantTime(update, item, owner)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGItemEnchantTimeUpdate, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGEnchantmentLog:
			log, err := modernworld.ParseLegacyEnchantmentLog(packet.Body)
			if err != nil {
				s.log.Warn("legacy enchantment-log malformed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			itemGUID := modernworld.FindLegacyItemGUIDByEntry(session.objectFields[session.currentCharacter], session.objectFields, log.ItemID)
			if itemGUID == 0 {
				session.worldMu.Unlock()
				s.log.Debug("enchantment log dropped, item GUID unknown", "account", session.legacy.Username, "item", log.ItemID, "enchant", log.Enchantment)
				continue
			}
			owner := session.modernGUIDForLegacyLocked(log.OwnerGUID)
			caster := session.modernGUIDForLegacyLocked(log.CasterGUID)
			item := session.modernGUIDForLegacyLocked(itemGUID)
			session.worldMu.Unlock()
			s.log.Debug("enchantment log", "account", session.legacy.Username, "item", log.ItemID, "enchant", log.Enchantment)
			body := modernworld.EncodeEnchantmentLog(log, owner, caster, item)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGEnchantmentLog, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGItemCooldown:
			cooldown, err := modernworld.ParseLegacyItemCooldown(packet.Body)
			if err != nil {
				s.log.Warn("legacy item-cooldown malformed", "account", session.legacy.Username, "error", err)
				continue
			}
			session.worldMu.Lock()
			item := session.modernGUIDForLegacyLocked(cooldown.ItemGUID)
			session.worldMu.Unlock()
			s.log.Debug("item cooldown", "account", session.legacy.Username, "item", fmt.Sprintf("0x%x", cooldown.ItemGUID), "spell", cooldown.SpellID)
			body := modernworld.EncodeItemCooldown(item, cooldown.SpellID, modernworld.DefaultItemCooldownMS)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGItemCooldown, Body: body}); err != nil {
				return
			}
		case legacyworld.SMSGDurabilityDamageDeath:
			if err := modernworld.ParseLegacyDurabilityDamageDeath(packet.Body); err != nil {
				s.log.Warn("legacy durability-damage-death malformed", "account", session.legacy.Username, "error", err)
				continue
			}
			s.log.Debug("durability damage death", "account", session.legacy.Username)
			if err := session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGDurabilityDamageDeath, Body: modernworld.EncodeDurabilityDamageDeath()}); err != nil {
				return
			}
		case legacyworld.SMSGSocketGems:
			// Hermes answers CMSG_SOCKET_GEMS with a local SMSG_SOCKET_GEMS_SUCCESS.
			// The leftover WotLK socket-result packet has no 54261 equivalent.
			continue
		default:
			handled, err := s.handleLegacyBattleground(session, packet)
			if err != nil {
				return
			}
			if handled {
				break
			}
			name := opcodes.Legacy12340[packet.Opcode]
			if name == "" {
				name = "UNKNOWN"
			}
			s.log.Debug("legacy world opcode pending translation", "account", session.legacy.Username, "opcode", packet.Opcode, "name", name, "bytes", len(packet.Body))
		}
	}
}

func (s *Server) saveDisconnectPackets(world *modernworld.PacketConn, source string) {
	file, err := os.CreateTemp(".", "disconnect-"+time.Now().Format("20060102-150405")+"-"+source+"-*.jsonl")
	if err != nil {
		s.log.Warn("save disconnect packets failed", "error", err)
		return
	}
	err = world.WriteRecentPackets(file)
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		s.log.Warn("save disconnect packets failed", "error", err)
		return
	}
	s.log.Warn("disconnect packets saved", "source", source, "path", file.Name())
}
