package modernworld

// wotlkMountSpeedAuras maps AzerothCore spell_gen_mount speed ranks onto the
// 3.4.3 parent spell the client still has in Spell.dbc. WotLK replaces the
// spellbook mount (for example 54729) with a rank such as 54726; Classic
// deleted those ranks, so the aura must be rewritten before SMSG_AURA_UPDATE.
var wotlkMountSpeedAuras = map[uint32]uint32{
	42680: 47977, // Magic Broom 60%
	42683: 47977, // Magic Broom 100%
	42667: 47977, // Magic Broom 150%
	42668: 47977, // Magic Broom 280%
	51621: 48025, // Headless Horseman's Mount 60%
	48024: 48025, // Headless Horseman's Mount 100%
	51617: 48025, // Headless Horseman's Mount 150%
	48023: 48025, // Headless Horseman's Mount 280%
	54726: 54729, // Winged Steed of the Ebon Blade 150%
	54727: 54729, // Winged Steed of the Ebon Blade 280%
	71343: 71342, // Big Love Rocket 0%
	71344: 71342, // Big Love Rocket 60%
	71345: 71342, // Big Love Rocket 100%
	71346: 71342, // Big Love Rocket 150%
	71347: 71342, // Big Love Rocket 310%
	72281: 72286, // Invincible 60%
	72282: 72286, // Invincible 100%
	72283: 72286, // Invincible 150%
	72284: 72286, // Invincible 310%
	74854: 74856, // Blazing Hippogryph 150%
	74855: 74856, // Blazing Hippogryph 280%
	75619: 75614, // Celestial Steed 60%
	75620: 75614, // Celestial Steed 100%
	75617: 75614, // Celestial Steed 150%
	75618: 75614, // Celestial Steed 280%
	76153: 75614, // Celestial Steed 310%
	75957: 75973, // X-53 Touring Rocket 150%
	75972: 75973, // X-53 Touring Rocket 280%
	76154: 75973, // X-53 Touring Rocket 310%
	58997: 58983, // Big Blizzard Bear 60%
	58999: 58983, // Big Blizzard Bear 100%+
}

var wotlkMountSpeedParents = func() map[uint32]struct{} {
	parents := make(map[uint32]struct{}, 9)
	for _, parent := range wotlkMountSpeedAuras {
		parents[parent] = struct{}{}
	}
	return parents
}()

// wotlkMountSpeedRankPoints is ground then optional flight percent for each
// AzerothCore spell_gen_mount rank. 3.4.3 mount buff text substitutes these
// from aura Points; the parent spell's own description has no baked-in 150%.
var wotlkMountSpeedRankPoints = map[uint32][]float32{
	42680: {60},
	42683: {100},
	42667: {100, 150},
	42668: {100, 280},
	51621: {60},
	48024: {100},
	51617: {100, 150},
	48023: {100, 280},
	54726: {100, 150},
	54727: {100, 280},
	71343: {0},
	71344: {60},
	71345: {100},
	71346: {100, 150},
	71347: {100, 310},
	72281: {60},
	72282: {100},
	72283: {100, 150},
	72284: {100, 310},
	74854: {100, 150},
	74855: {100, 280},
	75619: {60},
	75620: {100},
	75617: {100, 150},
	75618: {100, 280},
	76153: {100, 310},
	75957: {100, 150},
	75972: {100, 280},
	76154: {100, 310},
	58997: {60},
	58999: {100},
}

func ModernMountAuraSpell(spellID uint32) uint32 {
	if parent, ok := wotlkMountSpeedAuras[spellID]; ok {
		return parent
	}
	return spellID
}

// RemapMountSpeedAuras rewrites WotLK mount speed ranks to their 3.4.3 parent
// and clears VisualID so the caller looks up the parent visual. Party/raid
// member stats keep the original IDs and the existing mount filter.
func RemapMountSpeedAuras(auras []AuraInfo) {
	for index := range auras {
		if !auras[index].HasData || auras[index].SpellID == 0 {
			continue
		}
		rank := auras[index].SpellID
		parent := ModernMountAuraSpell(rank)
		if parent == rank {
			continue
		}
		auras[index].SpellID = parent
		auras[index].VisualID = 0
		if points := wotlkMountSpeedRankPoints[rank]; len(points) > 0 {
			auras[index].Points = append([]float32(nil), points...)
			auras[index].EstimatedPoints = append([]float32(nil), points...)
		}
	}
}

// CancelAuraUsesMountOpcode reports whether a 3.4.3 CMSG_CANCEL_AURA for this
// spell is a remapped mount buff. The legacy server still holds the speed-rank
// aura, so the dedicated dismount opcode is the reliable reverse mapping.
func CancelAuraUsesMountOpcode(spellID uint32) bool {
	if _, ok := wotlkMountSpeedAuras[spellID]; ok {
		return true
	}
	_, ok := wotlkMountSpeedParents[spellID]
	return ok
}
