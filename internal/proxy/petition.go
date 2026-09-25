package proxy

import (
	"errors"
	"fmt"
	"math"

	"redscarf/internal/modernworld"
	"redscarf/internal/realm"
)

var errInvalidPetitionPacket = errors.New("invalid legacy petition packet")

func (s *Server) handlePetitionRequest(session *proxySession, p modernworld.Packet) error {
	session.worldMu.Lock()
	q, err := modernworld.ParsePetitionRequest(p.Opcode, p.Body, func(g modernworld.GUID128, kind modernworld.PetitionGUIDKind) (uint64, bool) {
		switch kind {
		case modernworld.PetitionItem:
			// Offered charters are not world objects in the signer's inventory.
			// Use the strict item inverse, not the generic current-character fallback.
			if g.Low > math.MaxUint32 {
				return 0, false
			}
			id := modernworld.LegacyItemGUIDFromModern(g)
			return id, id != 0
		case modernworld.PetitionPlayer:
			if g.Low > math.MaxUint32 {
				return 0, false
			}
			return modernworld.LegacyPlayerGUIDFromModern(g)
		case modernworld.PetitionNPC:
			for id, known := range session.objectGUIDs {
				if known == g && uint16(id>>48) == 0xF130 {
					return id, true
				}
			}
		}
		return 0, false
	})
	legacy := session.legacyWorld
	session.worldMu.Unlock()
	if err != nil {
		return err
	}
	if legacy == nil {
		return nil
	}
	return legacy.WritePacket(uint32(q.Opcode), q.Body)
}

func (s *Server) handleLegacyPetition(session *proxySession, op uint16, body []byte) error {
	session.worldMu.Lock()
	q, err := modernworld.TranslateLegacyPetition(op, body, realm.Address(session.selectedRealm.ID), session.modernGUIDForLegacyLocked,
		func(id uint64) modernworld.GUID128 {
			if _, own := session.knownCharacters[id]; own && session.gameAccountID != 0 {
				return modernworld.ModernWowAccountGUID(session.gameAccountID)
			}
			return modernworld.ModernWowAccountGUIDForLegacy(id)
		}, func(id uint64) string { return session.playerNames[id] })
	session.worldMu.Unlock()
	if err != nil {
		return fmt.Errorf("%w: %v", errInvalidPetitionPacket, err)
	}
	return session.sendInstance(q)
}
