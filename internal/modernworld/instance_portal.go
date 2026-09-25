package modernworld

// Hellfire Citadel uses separate portal and skull GameObjects in 12340.
// In 54261 GameObjectDiffAnimMap group 2 renders animation 214 normally,
// and animation 215 with attachment display 7149 on Heroic (difficulty 2).
// Only fold verified pairs: other type-31 models (including raid portals)
// need their own animation/attachment mapping.
func hellfirePortalPart(entry uint32) (mapID uint32, skull bool) {
	switch entry {
	case 184127, 184177:
		return 540, false
	case 184128, 184178:
		return 540, true
	case 184130, 184175:
		return 542, false
	case 184129, 184176:
		return 542, true
	case 184131, 184179:
		return 543, false
	case 184132, 184180:
		return 543, true
	}
	return 0, false
}

func translateInstancePortalStats(entry uint32, stats *GameObjectQueryStats) {
	mapID, skull := hellfirePortalPart(entry)
	if mapID == 0 || skull || stats.Type != 31 || stats.DisplayID != 7148 ||
		stats.Data[0] != int32(mapID) || stats.Data[1] != 0 {
		return
	}
	// Do not copy mapID/difficulty into the modern InstanceType/animation
	// fields. Difficulty selection stays entirely under the client's normal
	// dungeon/party difficulty handling, including changes while at the door.
	stats.Data = [legacyGameObjectDataFields]int32{}
	stats.Data[0] = 1    // InstanceType: Party Dungeon
	stats.Data[1] = 214  // DifficultyNormal
	stats.Data[2] = 215  // DifficultyHeroic
	stats.Data[5] = 7149 // HeroicAttachment
	stats.Data[7] = 2    // GameObjectDiffAnim group, including the heroic attachment
}

// IsRedundantInstancePortalSkull identifies only verified legacy companion
// skulls. The primary portal now creates/removes its own heroic attachment.
func IsRedundantInstancePortalSkull(update LegacyObjectUpdate) bool {
	if update.ObjectType != 5 {
		return false
	}
	fields := update.Values.Fields
	_, skull := hellfirePortalPart(fields[legacyObjectEntry])
	return skull && fields[legacyGameObjectDisplayID] == 7149 &&
		byte(fields[legacyGameObjectBytes1]>>8) == 31
}
