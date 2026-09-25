package proxy

import (
	"encoding/binary"
	"errors"
	"fmt"

	"redscarf/internal/modernworld"
	"redscarf/internal/realm"
)

var errInvalidGuildPacket = errors.New("invalid legacy guild packet")

func (s *Server) handleGuildRequest(session *proxySession, p modernworld.Packet) error {
	session.worldMu.Lock()
	session.syncGuildMembershipLocked()
	var resolveErr error
	q, err := modernworld.ParseGuildRequest(p.Opcode, p.Body, func(g modernworld.GUID128) uint64 {
		id, ok := session.legacyGUIDForModernLocked(g)
		if !ok || id == 0 {
			resolveErr = fmt.Errorf("guild object GUID is not known")
		}
		return id
	})
	if err == nil {
		err = resolveErr
	}
	if err == nil && q.BlockInvites != nil {
		session.guild.BlockInvites = *q.BlockInvites
	}
	if err == nil && q.NeedsName {
		id, ok := session.legacySocialTargetLocked(q.Target)
		name := session.guild.Members[id].Name
		if name == "" {
			name = session.playerNames[id]
		}
		if !ok || name == "" {
			err = fmt.Errorf("guild member name not cached; refresh roster")
		} else {
			q.Body = append(append([]byte(name), 0), q.Body...)
		}
	}
	// WotLK deletes only the last rank. Never silently delete a different rank
	// when a modern client supplies another index.
	if err == nil && p.Opcode == modernworld.CMSGGuildDeleteRank {
		rank := binary.LittleEndian.Uint32(p.Body)
		if len(session.guild.Ranks) == 0 || int(rank) != len(session.guild.Ranks)-1 {
			err = fmt.Errorf("WotLK can delete only the last guild rank")
		}
	}
	legacy := session.legacyWorld
	guildID := session.guild.ID
	session.worldMu.Unlock()
	if err != nil {
		return err
	}
	s.log.Debug("guild request translated", "opcode", p.Opcode, "legacy_opcode", q.Opcode, "guild_id", guildID, "bytes", len(p.Body), "legacy_connected", legacy != nil)
	if q.BlockInvites != nil {
		return nil
	}
	if legacy == nil {
		return nil
	}
	if p.Opcode == modernworld.CMSGGuildGetRoster || p.Opcode == modernworld.CMSGGuildGetRanks {
		// Rank names are only in QUERY_RESPONSE; permissions are only in ROSTER.
		// Request both every refresh so rename/add/delete never retain old labels.
		if guildID != 0 {
			if err := legacy.WritePacket(0x54, binary.LittleEndian.AppendUint32(nil, guildID)); err != nil {
				return err
			}
		}
		if err := legacy.WritePacket(0x87, nil); err != nil {
			return err
		}
	}
	return legacy.WritePacket(uint32(q.Opcode), q.Body)
}

func (s *Server) handleLegacyGuild(session *proxySession, op uint16, body []byte) error {
	session.worldMu.Lock()
	// Player fields are authoritative after an invite is accepted or a guild is
	// left; character-enumeration membership is only the initial fallback.
	session.syncGuildMembershipLocked()
	packets, err := modernworld.TranslateLegacyGuild(op, body, &session.guild, realm.Address(session.selectedRealm.ID), session.modernGUIDForLegacyLocked(session.currentCharacter), session.modernGUIDForLegacyLocked, func(id uint64) byte {
		if identity, _, ok := session.playerNameIdentityLocked(id); ok && identity.Race != 0 {
			return identity.Race
		}
		// Legacy roster does not carry race. Match legacy proxy's local-player
		// fallback until this member's name-query identity is available.
		return session.knownCharacterInfo[session.currentCharacter].Race
	})
	blocked := err == nil && op == 0x83 && session.guild.BlockInvites
	legacy := session.legacyWorld
	guildID := session.guild.ID
	memberCount, rankCount := len(session.guild.Members), len(session.guild.Ranks)
	if err == nil && op == 0x8A {
		for id, m := range session.guild.Members {
			session.rememberPlayerNameLocked(id, m.Name)
		}
	}
	session.worldMu.Unlock()
	if err != nil {
		return fmt.Errorf("%w: %v", errInvalidGuildPacket, err)
	}
	s.log.Debug("guild response translated", "legacy_opcode", op, "guild_id", guildID, "bytes", len(body), "members", memberCount, "ranks", rankCount, "packets", len(packets))
	if blocked {
		if legacy != nil {
			return legacy.WritePacket(0x85, nil)
		}
		return nil
	}
	for _, p := range packets {
		if err := session.sendInstance(p); err != nil {
			return err
		}
		s.log.Debug("guild response sent or queued", "opcode", p.Opcode, "guild_id", guildID, "bytes", len(p.Body))
	}
	if op == 0x92 && len(body) > 0 && guildID != 0 && legacy != nil {
		switch body[0] {
		case 0, 1, 3, 4, 5, 7, 10, 11:
			if err := legacy.WritePacket(0x54, binary.LittleEndian.AppendUint32(nil, guildID)); err != nil {
				return err
			}
			return legacy.WritePacket(0x89, nil)
		}
	}
	return nil
}

func (session *proxySession) syncGuildMembershipLocked() {
	if id, ok := session.objectFields[session.currentCharacter][151]; ok && id != session.guild.ID {
		session.guild.ID = id
		session.guild.Members = nil
		session.guild.Ranks = nil
		session.guild.CreateDate = 0
		session.guild.NumAccounts = 0
	}
}
