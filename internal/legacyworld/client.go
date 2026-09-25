// Package legacyworld implements the 3.3.5a world authentication handshake.
package legacyworld

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rc4"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"redscarf/internal/legacyauth"
)

const (
	Build335a                         = uint32(12340)
	CMSGCreateCharacter               = uint32(0x0036)
	CMSGEnumCharacters                = uint32(0x0037)
	CMSGCharDelete                    = uint32(0x0038)
	CMSGCharacterRenameRequest        = uint32(0x02C7)
	CMSGPlayerLogin                   = uint32(0x003d)
	CMSGLogoutRequest                 = uint32(0x004b)
	CMSGLogoutCancel                  = uint32(0x004e)
	CMSGRequestPlayedTime             = uint32(0x01cc)
	CMSGDuelAccepted                  = uint32(0x016c)
	CMSGDuelCancelled                 = uint32(0x016d)
	CMSGSetActiveMover                = uint32(0x026a)
	CMSGTutorialFlag                  = uint32(0x00fe)
	CMSGTutorialClear                 = uint32(0x00ff)
	CMSGTutorialReset                 = uint32(0x0100)
	CMSGTimeSyncResponse              = uint32(0x0391)
	CMSGMoveTimeSkipped               = uint32(0x02ce)
	CMSGMoveSetCollisionHeightAck     = uint32(0x0517)
	CMSGNameQuery                     = uint32(0x0050)
	CMSGPetNameQuery                  = uint32(0x0052)
	CMSGWho                           = uint32(0x0062)
	CMSGAddFriend                     = uint32(0x0069)
	CMSGDelFriend                     = uint32(0x006A)
	CMSGSetContactNotes               = uint32(0x006B)
	CMSGAddIgnore                     = uint32(0x006C)
	CMSGDelIgnore                     = uint32(0x006D)
	CMSGGroupInvite                   = uint32(0x006E)
	CMSGGroupAccept                   = uint32(0x0072)
	CMSGGroupDecline                  = uint32(0x0073)
	CMSGGroupUninviteGUID             = uint32(0x0076)
	CMSGGroupSetLeader                = uint32(0x0078)
	CMSGSetLootMethod                 = uint32(0x007A)
	CMSGGroupDisband                  = uint32(0x007B)
	MSGMinimapPing                    = uint32(0x01D5)
	MSGRandomRoll                     = uint32(0x01FB)
	CMSGGroupChangeSubGroup           = uint32(0x027E)
	CMSGRequestPartyMemberStats       = uint32(0x027F)
	CMSGGroupSwapSubGroup             = uint32(0x0280)
	CMSGGroupRaidConvert              = uint32(0x028E)
	CMSGGroupAssistantLeader          = uint32(0x028F)
	MSGRaidReadyCheck                 = uint32(0x0322)
	CMSGLfgGetStatus                  = uint32(0x0296)
	CMSGLfgJoin                       = uint32(0x035C)
	CMSGLfgLeave                      = uint32(0x035D)
	CMSGLfgProposalResult             = uint32(0x0362)
	CMSGDFSetRoles                    = uint32(0x036A)
	CMSGLfgSetBootVote                = uint32(0x036C)
	CMSGLfdPlayerLockInfoRequest      = uint32(0x036E)
	CMSGLfgTeleport                   = uint32(0x0370)
	CMSGLfdPartyLockInfoRequest       = uint32(0x0371)
	MSGPartyAssignment                = uint32(0x038E)
	MSGRaidReadyCheckConfirm          = uint32(0x03AE)
	MSGRaidReadyCheckFinished         = uint32(0x03C6)
	CMSGOptOutOfLoot                  = uint32(0x0409)
	CMSGGameObjectQuery               = uint32(0x005e)
	CMSGGetMirrorImageData            = uint32(0x0401)
	CMSGCreatureQuery                 = uint32(0x0060)
	CMSGQuestQuery                    = uint32(0x005c)
	CMSGMessageChat                   = uint32(0x0095)
	CMSGFarSight                      = uint32(0x027A)
	CMSGJoinChannel                   = uint32(0x0097)
	CMSGLeaveChannel                  = uint32(0x0098)
	CMSGChannelList                   = uint32(0x009A)
	CMSGChannelOwner                  = uint32(0x009E)
	CMSGChannelAnnouncements          = uint32(0x00A7)
	CMSGChannelDisplayList            = uint32(0x03D2)
	CMSGChannelDeclineInvite          = uint32(0x0410)
	CMSGQuestGiverStatusQuery         = uint32(0x0182)
	CMSGQuestPOIQuery                 = uint32(0x01e3)
	CMSGQuestGiverStatusMultipleQuery = uint32(0x0417)
	CMSGQueryQuestsCompleted          = uint32(0x0500)
	CMSGStandStateChange              = uint32(0x0101)
	CMSGTextEmote                     = uint32(0x0104)
	CMSGSetSelection                  = uint32(0x013d)
	CMSGAttackSwing                   = uint32(0x0141)
	CMSGAttackStop                    = uint32(0x0142)
	CMSGSetActionButton               = uint32(0x0128)
	CMSGSetFactionAtWar               = uint32(0x0125)
	CMSGSetFactionInactive            = uint32(0x0317)
	CMSGSetWatchedFaction             = uint32(0x0318)
	CMSGUseItem                       = uint32(0x00ab)
	CMSGOpenItem                      = uint32(0x00ac)
	CMSGReadItem                      = uint32(0x00AD)
	CMSGQueryPageText                 = uint32(0x005A)
	CMSGCastSpell                     = uint32(0x012e)
	CMSGCancelCast                    = uint32(0x012f)
	CMSGUpdateMissileTrajectory       = uint32(0x0462)
	CMSGCancelAura                    = uint32(0x0136)
	CMSGCancelMountAura               = uint32(0x0375)
	CMSGRequestVehicleExit            = uint32(0x0476)
	CMSGPetAction                     = uint32(0x0175)
	CMSGPetSetAction                  = uint32(0x0174)
	CMSGPetAbandon                    = uint32(0x0176)
	CMSGPetRename                     = uint32(0x0177)
	CMSGPetStopAttack                 = uint32(0x02ea)
	CMSGPetCancelAura                 = uint32(0x026b)
	CMSGPetSpellAutocast              = uint32(0x02f3)
	CMSGRequestPetInfo                = uint32(0x0279)
	MSGListStabledPets                = uint32(0x026F)
	CMSGStablePet                     = uint32(0x0270)
	CMSGUnstablePet                   = uint32(0x0271)
	CMSGBuyStableSlot                 = uint32(0x0272)
	CMSGStableSwapPet                 = uint32(0x0275)
	CMSGSetAmmo                       = uint32(0x0268)
	CMSGSetTitle                      = uint32(0x0374)
	CMSGTogglePvP                     = uint32(0x0253)
	CMSGUnlearnSkill                  = uint32(0x0202)
	CMSGRemoveGlyph                   = uint32(0x048A)
	CMSGAutostoreLootItem             = uint32(0x0108)
	CMSGAutoEquipItem                 = uint32(0x010A)
	CMSGSwapItem                      = uint32(0x010C)
	CMSGSwapInvItem                   = uint32(0x010D)
	CMSGAutoEquipItemSlot             = uint32(0x010F)
	CMSGDestroyItem                   = uint32(0x0111)
	CMSGInspect                       = uint32(0x0114)
	CMSGInitiateTrade                 = uint32(0x0116)
	CMSGBeginTrade                    = uint32(0x0117)
	CMSGBusyTrade                     = uint32(0x0118)
	CMSGIgnoreTrade                   = uint32(0x0119)
	CMSGAcceptTrade                   = uint32(0x011A)
	CMSGUnacceptTrade                 = uint32(0x011B)
	CMSGCancelTrade                   = uint32(0x011c)
	CMSGSetTradeItem                  = uint32(0x011D)
	CMSGClearTradeItem                = uint32(0x011E)
	CMSGSetTradeGold                  = uint32(0x011F)
	CMSGGameObjectUse                 = uint32(0x00B1)
	CMSGAreaTrigger                   = uint32(0x00B4)
	CMSGGameObjectReportUse           = uint32(0x0481)
	CMSGLoot                          = uint32(0x015d)
	CMSGLootMoney                     = uint32(0x015e)
	CMSGLootRelease                   = uint32(0x015f)
	CMSGLootRoll                      = uint32(0x02a0)
	CMSGLootMasterGive                = uint32(0x02a3)
	CMSGPushQuestToParty              = uint32(0x019d)
	CMSGQueryInspectAchievements      = uint32(0x046b)
	CMSGNpcTextQuery                  = uint32(0x017f)
	CMSGSetActionBarToggles           = uint32(0x01bf)
	CMSGResurrectResponse             = uint32(0x015c)
	CMSGQueryTime                     = uint32(0x01ce)
	CMSGSetSheathed                   = uint32(0x01e0)
	CMSGRepopRequest                  = uint32(0x015a)
	CMSGReclaimCorpse                 = uint32(0x01d2)
	CMSGGossipHello                   = uint32(0x017b)
	CMSGGossipSelectOption            = uint32(0x017c)
	CMSGQuestGiverHello               = uint32(0x0184)
	CMSGQuestGiverQueryQuest          = uint32(0x0186)
	CMSGQuestGiverAcceptQuest         = uint32(0x0189)
	CMSGQuestGiverCompleteQuest       = uint32(0x018A)
	CMSGQuestGiverRequestReward       = uint32(0x018C)
	CMSGQuestGiverChooseReward        = uint32(0x018E)
	CMSGQuestLogRemoveQuest           = uint32(0x0194)
	CMSGQuestConfirmAccept            = uint32(0x019B)
	CMSGListInventory                 = uint32(0x019E)
	CMSGBuyBackItem                   = uint32(0x0290)
	CMSGSellItem                      = uint32(0x01A0)
	CMSGBuyItem                       = uint32(0x01A2)
	CMSGTrainerList                   = uint32(0x01B0)
	CMSGTrainerBuySpell               = uint32(0x01B2)
	CMSGBinderActivate                = uint32(0x01B5)
	CMSGBankerActivate                = uint32(0x01B7)
	SMSGShowBank                      = uint16(0x01B8)
	CMSGBuyBankSlot                   = uint32(0x01B9)
	CMSGAutobankItem                  = uint32(0x0283)
	CMSGAutostoreBankItem             = uint32(0x0282)
	CMSGAutostoreBagItem              = uint32(0x010B)
	CMSGSplitItem                     = uint32(0x010E)
	CMSGWrapItem                      = uint32(0x01D3)
	CMSGCancelTempEnchantment         = uint32(0x0379)
	CMSGCancelAutoRepeatSpell         = uint32(0x026D)
	CMSGCancelChannelling             = uint32(0x013B)
	CMSGPetCastSpell                  = uint32(0x01F0)
	CMSGPetLearnTalent                = uint32(0x047A)
	CMSGSelfRes                       = uint32(0x02B3)
	CMSGSpellClick                    = uint32(0x03F8)
	CMSGTotemDestroyed                = uint32(0x0414)
	CMSGSocketGems                    = uint32(0x0347)
	CMSGRepairItem                    = uint32(0x02A8)
	CMSGTaxiNodeStatusQuery           = uint32(0x01AA)
	CMSGTaxiQueryAvailableNodes       = uint32(0x01AC)
	CMSGActivateTaxi                  = uint32(0x01AD)
	CMSGActivateTaxiExpress           = uint32(0x0312)
	CMSGMoveSplineDone                = uint32(0x02C9)
	CMSGQueryNextMailTime             = uint32(0x0284)
	CMSGMailGetList                   = uint32(0x023A)
	CMSGSendMail                      = uint32(0x0238)
	CMSGMailTakeMoney                 = uint32(0x0245)
	CMSGMailTakeItem                  = uint32(0x0246)
	CMSGMailMarkAsRead                = uint32(0x0247)
	CMSGMailReturnToSender            = uint32(0x0248)
	CMSGMailDelete                    = uint32(0x0249)
	CMSGMailCreateTextItem            = uint32(0x024A)
	CMSGAuctionSellItem               = uint32(0x0256)
	CMSGAuctionRemoveItem             = uint32(0x0257)
	CMSGAuctionListItems              = uint32(0x0258)
	CMSGAuctionListOwnedItems         = uint32(0x0259)
	CMSGAuctionPlaceBid               = uint32(0x025A)
	CMSGAuctionListBidderItems        = uint32(0x0264)
	CMSGRequestRaidInfo               = uint32(0x02cd)
	CMSGResetInstances                = uint32(0x031d)
	CMSGSetDungeonDifficulty          = uint32(0x0329)
	CMSGSetRaidDifficulty             = uint32(0x04eb)
	CMSGInstanceLockResponse          = uint32(0x013f)
	SMSGSetRaidDifficulty             = uint16(0x04eb)
	SMSGInstanceSaveCreated           = uint16(0x02cb)
	SMSGRaidInstanceMessage           = uint16(0x02fa)
	SMSGInstanceLockWarningQuery      = uint16(0x0147)
	SMSGUpdateInstanceEncounterUnit   = uint16(0x0214)
	CMSGSummonResponse                = uint32(0x02ac)
	CMSGContactList                   = uint32(0x0066)
	CMSGLearnTalent                   = uint32(0x0251)
	CMSGPetUnlearn                    = uint32(0x02F0)
	MSGTalentWipeConfirm              = uint16(0x02AA)
	CMSGSpiritHealerActivate          = uint32(0x021c)
	MSGCorpseQuery                    = uint32(0x0216)
	SMSGSpiritHealerConfirm           = uint16(0x0222)
	MSGMoveTeleportAck                = uint32(0x00c7)
	MSGMoveWorldportAck               = uint32(0x00dc)
	SMSGNewWorld                      = uint16(0x003e)
	SMSGTransferPending               = uint16(0x003f)
	SMSGTransferAborted               = uint16(0x0040)
	SMSGEnumCharactersResult          = uint16(0x003b)
	SMSGCreateCharacter               = uint16(0x003a)
	SMSGDeleteCharacter               = uint16(0x003c)
	SMSGCharacterRenameResult         = uint16(0x02C8)
	MsgInspectHonorStats              = uint16(0x02D6)
	SMSGTitleEarned                   = uint16(0x0373)
	MsgInspectArenaTeams              = uint16(0x0377)
	CMSGArenaTeamQuery                = uint32(0x034B)
	CMSGArenaTeamRoster               = uint32(0x034D)
	CMSGArenaTeamAccept               = uint32(0x0351)
	CMSGArenaTeamRemove               = uint32(0x0354)
	CMSGArenaTeamDisband              = uint32(0x0355)
	CMSGBattlemasterJoinArena         = uint32(0x0358)
	CMSGBattlefieldList               = uint32(0x023C)
	SMSGBattlefieldList               = uint16(0x023D)
	SMSGPvpCredit                     = uint16(0x028C)
	SMSGPlayerSkinned                 = uint16(0x02BC)
	CMSGBattlefieldStatus             = uint32(0x02D3)
	SMSGBattlefieldStatus             = uint16(0x02D4)
	CMSGBattlefieldPort               = uint32(0x02D5)
	MsgPvpLogData                     = uint16(0x02E0)
	CMSGLeaveBattlefield              = uint32(0x02E1)
	CMSGAreaSpiritHealerQuery         = uint32(0x02E2)
	CMSGAreaSpiritHealerQueue         = uint32(0x02E3)
	SMSGAreaSpiritHealerTime          = uint16(0x02E4)
	SMSGGroupJoinedBattleground       = uint16(0x02E8)
	MsgBattlegroundPlayerPositions    = uint16(0x02E9)
	SMSGBattlegroundPlayerJoined      = uint16(0x02EC)
	SMSGBattlegroundPlayerLeft        = uint16(0x02ED)
	CMSGBattlemasterJoin              = uint32(0x02EE)
	SMSGArenaTeamCommandResult        = uint16(0x0349)
	SMSGArenaTeamQueryResponse        = uint16(0x034C)
	SMSGArenaTeamRoster               = uint16(0x034E)
	SMSGArenaTeamInvite               = uint16(0x0350)
	SMSGArenaTeamEvent                = uint16(0x0357)
	SMSGArenaTeamStats                = uint16(0x035B)
	SMSGCharacterLoginFailed          = uint16(0x0041)
	SMSGLogoutResponse                = uint16(0x004c)
	SMSGLogoutComplete                = uint16(0x004d)
	SMSGLogoutCancelAck               = uint16(0x004f)
	SMSGLoginVerifyWorld              = uint16(0x0236)
	SMSGAccountDataTimes              = uint16(0x0209)
	SMSGTutorialFlags                 = uint16(0x00fd)
	SMSGTimeSyncRequest               = uint16(0x0390)
	SMSGFeatureSystemStatus           = uint16(0x03c9)
	SMSGMOTD                          = uint16(0x033d)
	SMSGBindPointUpdate               = uint16(0x0155)
	SMSGPlayerBound                   = uint16(0x0158)
	SMSGBinderConfirm                 = uint16(0x02EB)
	SMSGShowTaxiNodes                 = uint16(0x01A9)
	SMSGTaxiNodeStatus                = uint16(0x01AB)
	SMSGActivateTaxiReply             = uint16(0x01AE)
	SMSGNewTaxiPath                   = uint16(0x01AF)
	SMSGInitializeFactions            = uint16(0x0122)
	SMSGSetFactionVisible             = uint16(0x0123)
	SMSGLoginSetTimeSpeed             = uint16(0x0042)
	SMSGPlayedTime                    = uint16(0x01cd)
	SMSGExplorationExperience         = uint16(0x01f8)
	SMSGDuelRequested                 = uint16(0x0167)
	SMSGDuelOutOfBounds               = uint16(0x0168)
	SMSGDuelInBounds                  = uint16(0x0169)
	SMSGDuelComplete                  = uint16(0x016a)
	SMSGDuelWinner                    = uint16(0x016b)
	SMSGDuelCountdown                 = uint16(0x02b7)
	SMSGSetForcedReactions            = uint16(0x02a5)
	SMSGInitWorldStates               = uint16(0x02c2)
	SMSGUpdateWorldState              = uint16(0x02c3)
	SMSGNameQueryResponse             = uint16(0x0051)
	SMSGPetNameQueryResponse          = uint16(0x0053)
	SMSGWho                           = uint16(0x0063)
	SMSGFriendStatus                  = uint16(0x0068)
	SMSGPartyInvite                   = uint16(0x006F)
	SMSGGroupDecline                  = uint16(0x0074)
	SMSGGroupUninvite                 = uint16(0x0077)
	SMSGGroupSetLeader                = uint16(0x0079)
	SMSGGroupDestroyed                = uint16(0x007C)
	SMSGGroupList                     = uint16(0x007D)
	SMSGPartyMemberPartialState       = uint16(0x007E)
	SMSGPartyCommandResult            = uint16(0x007F)
	SMSGPartyMemberFullState          = uint16(0x02F2)
	SMSGTradeStatus                   = uint16(0x0120)
	SMSGTradeStatusExtended           = uint16(0x0121)
	SMSGInspectTalent                 = uint16(0x03F4)
	SMSGRaidGroupOnly                 = uint16(0x0286)
	SMSGLFGPlayerReward               = uint16(0x01FF)
	SMSGLFGTeleportDenied             = uint16(0x0200)
	SMSGLFGOfferContinue              = uint16(0x0293)
	SMSGLFGRoleChosen                 = uint16(0x02BB)
	SMSGLFGProposalUpdate             = uint16(0x0361)
	SMSGLFGRoleCheckUpdate            = uint16(0x0363)
	SMSGLFGJoinResult                 = uint16(0x0364)
	SMSGLFGQueueStatus                = uint16(0x0365)
	SMSGLFGUpdatePlayer               = uint16(0x0367)
	SMSGLFGUpdateParty                = uint16(0x0368)
	SMSGLFGPlayerInfo                 = uint16(0x036F)
	SMSGLFGPartyInfo                  = uint16(0x0372)
	SMSGLFGDisabled                   = uint16(0x0398)
	MSGRaidTargetUpdate               = uint16(0x0321)
	MSGQuestPushResult                = uint16(0x0276)
	SMSGGameObjectQueryResponse       = uint16(0x005f)
	SMSGCreatureQueryResponse         = uint16(0x0061)
	SMSGMirrorImageData               = uint16(0x0402)
	SMSGQuestQueryResponse            = uint16(0x005d)
	SMSGChat                          = uint16(0x0096)
	SMSGGMMessageChat                 = uint16(0x03B3)
	SMSGPrintNotification             = uint16(0x01CB)
	SMSGAreaTriggerMessage            = uint16(0x02B8)
	SMSGPhaseShiftChange              = uint16(0x047C)
	SMSGChannelNotify                 = uint16(0x0099)
	SMSGChannelList                   = uint16(0x009B)
	SMSGChatPlayerNotFound            = uint16(0x02a9)
	SMSGQuestGiverStatus              = uint16(0x0183)
	SMSGQuestGiverQuestList           = uint16(0x0185)
	SMSGQuestGiverQuestDetails        = uint16(0x0188)
	SMSGQuestGiverRequestItems        = uint16(0x018B)
	SMSGQuestGiverOfferReward         = uint16(0x018D)
	SMSGQuestGiverInvalidQuest        = uint16(0x018F)
	SMSGQuestGiverQuestComplete       = uint16(0x0191)
	SMSGQuestGiverQuestFailed         = uint16(0x0192)
	SMSGQuestConfirmAccept            = uint16(0x019C)
	SMSGQuestLogFull                  = uint16(0x0195)
	SMSGQuestUpdateFailed             = uint16(0x0196)
	SMSGQuestUpdateFailedTimer        = uint16(0x0197)
	SMSGQuestUpdateComplete           = uint16(0x0198)
	SMSGQuestUpdateAddKill            = uint16(0x0199)
	SMSGQuestUpdateAddItem            = uint16(0x019A)
	SMSGQuestGiverStatusMultiple      = uint16(0x0418)
	SMSGQuestPOIQueryResponse         = uint16(0x01e4)
	SMSGQueryQuestsCompletedResponse  = uint16(0x0501)
	SMSGWeather                       = uint16(0x02f4)
	SMSGSetProficiency                = uint16(0x0127)
	SMSGInventoryChangeFailure        = uint16(0x0112)
	SMSGVendorInventory               = uint16(0x019F)
	SMSGBuySucceeded                  = uint16(0x01A4)
	SMSGBuyFailed                     = uint16(0x01A5)
	SMSGSellResponse                  = uint16(0x01A1)
	SMSGTrainerList                   = uint16(0x01B1)
	SMSGTrainerBuySucceeded           = uint16(0x01B3)
	SMSGTrainerBuyFailed              = uint16(0x01B4)
	SMSGUpdateTalentData              = uint16(0x04C0)
	SMSGCastFailed                    = uint16(0x0130)
	SMSGPetCastFailed                 = uint16(0x0138)
	SMSGSpellFailure                  = uint16(0x0133)
	SMSGSpellInstakillLog             = uint16(0x032F)
	SMSGSpellDispellLog               = uint16(0x027B)
	SMSGSpellDamageShield             = uint16(0x024F)
	SMSGEnvironmentalDamageLog        = uint16(0x01FC)
	SMSGCancelAutoRepeat              = uint16(0x029C)
	SMSGPlaySpellVisual               = uint16(0x01F3)
	SMSGTotemCreated                  = uint16(0x0413)
	SMSGSpellHealLog                  = uint16(0x0150)
	SMSGSpellEnergizeLog              = uint16(0x0151)
	SMSGLevelUpInfo                   = uint16(0x01d4)
	SMSGSpellDelayed                  = uint16(0x01e2)
	SMSGLogXPGain                     = uint16(0x01d0)
	SMSGStartMirrorTimer              = uint16(0x01d9)
	SMSGPauseMirrorTimer              = uint16(0x01DA)
	SMSGStopMirrorTimer               = uint16(0x01db)
	SMSGPlayMusic                     = uint16(0x0277)
	SMSGZoneUnderAttack               = uint16(0x0254)
	SMSGDefenseMessage                = uint16(0x033A)
	SMSGChatServerMessage             = uint16(0x0291)
	SMSGQueryItemTextResponse         = uint16(0x0244)
	SMSGInvalidatePlayer              = uint16(0x031C)
	SMSGSpecialMountAnim              = uint16(0x0172)
	SMSGPartyKillLog                  = uint16(0x01f5)
	SMSGGameObjectCustomAnim          = uint16(0x00b3)
	SMSGFishNotHooked                 = uint16(0x01c8)
	SMSGFishEscaped                   = uint16(0x01c9)
	SMSGGameObjectDespawn             = uint16(0x0215)
	SMSGGameObjectResetState          = uint16(0x02a7)
	SMSGPlaySound                     = uint16(0x02d2)
	SMSGLootList                      = uint16(0x03f9)
	SMSGCriteriaUpdate                = uint16(0x046a)
	SMSGAllAchievementData            = uint16(0x047d)
	SMSGAchievementEarned             = uint16(0x0468)
	SMSGAchievementDeleted            = uint16(0x049F)
	SMSGRespondInspectAchievements    = uint16(0x046c)
	SMSGSetFactionStanding            = uint16(0x0124)
	SMSGNextMailTime                  = uint16(0x0284)
	SMSGMailListResult                = uint16(0x023B)
	SMSGMailCommandResult             = uint16(0x0239)
	SMSGNotifyReceivedMail            = uint16(0x0285)
	MsgAuctionHello                   = uint16(0x0255)
	SMSGAuctionCommandResult          = uint16(0x025B)
	SMSGAuctionListItemsResult        = uint16(0x025C)
	SMSGAuctionListOwnedItemsResult   = uint16(0x025D)
	SMSGAuctionBidderNotification     = uint16(0x025E)
	SMSGAuctionOwnerNotification      = uint16(0x025F)
	SMSGAuctionListBidderItemsResult  = uint16(0x0265)
	SMSGReadItemResultOK              = uint16(0x00AE)
	SMSGReadItemResultFailed          = uint16(0x00AF)
	SMSGQueryPageTextResponse         = uint16(0x005B)
	SMSGRaidInstanceInfo              = uint16(0x02cc)
	SMSGInstanceReset                 = uint16(0x031e)
	SMSGInstanceResetFailed           = uint16(0x031f)
	SMSGResetFailedNotify             = uint16(0x0396)
	SMSGSummonRequest                 = uint16(0x02ab)
	SMSGUpdateInstanceOwnership       = uint16(0x032b)
	SMSGContactList                   = uint16(0x0067)
	SMSGSetDungeonDifficulty          = uint16(0x0329)
	SMSGInstanceDifficulty            = uint16(0x033b)
	SMSGLearnedDanceMoves             = uint16(0x0455)
	SMSGClientCacheVersion            = uint16(0x04ab)
	SMSGEquipmentSetList              = uint16(0x04bc)
	SMSGAddonInfo                     = uint16(0x02ef)
	SMSGLootResponse                  = uint16(0x0160)
	SMSGLootRelease                   = uint16(0x0161)
	SMSGLootRemoved                   = uint16(0x0162)
	SMSGLootMoneyNotify               = uint16(0x0163)
	SMSGLootClearMoney                = uint16(0x0165)
	SMSGLootAllPassed                 = uint16(0x029e)
	SMSGLootRollWon                   = uint16(0x029f)
	SMSGLootStartRoll                 = uint16(0x02a1)
	SMSGLootRoll                      = uint16(0x02a2)
	SMSGLootMasterList                = uint16(0x02a4)
	SMSGItemPushResult                = uint16(0x0166)
	SMSGItemEnchantTimeUpdate         = uint16(0x01EB)
	SMSGEnchantmentLog                = uint16(0x01D7)
	SMSGItemCooldown                  = uint16(0x00B0)
	SMSGDurabilityDamageDeath         = uint16(0x02BD)
	SMSGSocketGems                    = uint16(0x050B)
	SMSGGossipMessage                 = uint16(0x017D)
	SMSGGossipComplete                = uint16(0x017E)
	SMSGGossipPOI                     = uint16(0x0224)
	SMSGUpdateLastInstance            = uint16(0x0320)
	SMSGNpcTextUpdate                 = uint16(0x0180)
	SMSGUpdateComboPoints             = uint16(0x039d)
	SMSGPetUpdateComboPoints          = uint16(0x0492)
	SMSGSpellNonMeleeDamageLog        = uint16(0x0250)
	SMSGSpellMissLog                  = uint16(0x024b)
	SMSGSpellExecuteLog               = uint16(0x024c)
	SMSGSpellPeriodicAuraLog          = uint16(0x024e)
	SMSGSpellFailedOther              = uint16(0x02a6)
	SMSGPetTameFailure                = uint16(0x0173)
	SMSGPetSpells                     = uint16(0x0179)
	SMSGListStabledPets               = uint16(0x026F)
	SMSGPetStableResult               = uint16(0x0273)
	SMSGPetActionSound                = uint16(0x0324)
	SMSGHighestThreatUpdate           = uint16(0x0482)
	SMSGThreatUpdate                  = uint16(0x0483)
	SMSGThreatRemove                  = uint16(0x0484)
	SMSGThreatClear                   = uint16(0x0485)
	SMSGConvertRune                   = uint16(0x0486)
	SMSGResyncRunes                   = uint16(0x0487)
	SMSGAddRunePower                  = uint16(0x0488)
	SMSGEmote                         = uint16(0x0103)
	SMSGTextEmote                     = uint16(0x0105)
	SMSGAIReaction                    = uint16(0x013c)
	SMSGAttackStart                   = uint16(0x0143)
	SMSGAttackStop                    = uint16(0x0144)
	SMSGAttackSwingNotInRange         = uint16(0x0145)
	SMSGAttackSwingBadFacing          = uint16(0x0146)
	SMSGAttackSwingDeadTarget         = uint16(0x0148)
	SMSGAttackSwingCantAttack         = uint16(0x0149)
	SMSGAttackerStateUpdate           = uint16(0x014a)
	SMSGCancelCombat                  = uint16(0x014e)
	SMSGSpellStart                    = uint16(0x0131)
	SMSGSpellGo                       = uint16(0x0132)
	MSGChannelStart                   = uint16(0x0139)
	MSGChannelUpdate                  = uint16(0x013a)
	SMSGSetFlatSpellModifier          = uint16(0x0266)
	SMSGSetPctSpellModifier           = uint16(0x0267)
	SMSGResurrectRequest              = uint16(0x015b)
	SMSGQueryTimeResponse             = uint16(0x01cf)
	SMSGStandStateUpdate              = uint16(0x029d)
	SMSGDismount                      = uint16(0x03ac)
	SMSGUpdateActionButtons           = uint16(0x0129)
	SMSGSendKnownSpells               = uint16(0x012a)
	SMSGLearnedSpell                  = uint16(0x012b)
	SMSGSupercededSpells              = uint16(0x012c)
	SMSGSpellCooldown                 = uint16(0x0134)
	SMSGCooldownEvent                 = uint16(0x0135)
	SMSGClearCooldown                 = uint16(0x01de)
	SMSGUnlearnedSpells               = uint16(0x0203)
	SMSGSendUnlearnSpells             = uint16(0x041e)
	SMSGCorpseReclaimDelay            = uint16(0x0269)
	SMSGDeathReleaseLoc               = uint16(0x0378)
	SMSGPreResurrect                  = uint16(0x0494)
	SMSGAuraUpdateAll                 = uint16(0x0495)
	SMSGAuraUpdate                    = uint16(0x0496)
	SMSGUpdateObject                  = uint16(0x00a9)
	SMSGCompressedUpdateObject        = uint16(0x01f6)
	SMSGDestroyObject                 = uint16(0x00aa)
	SMSGOnMonsterMove                 = uint16(0x00dd)
	SMSGMonsterMoveTransport          = uint16(0x02ae)
	SMSGControlUpdate                 = uint16(0x0159)
	SMSGPowerUpdate                   = uint16(0x0480)
	SMSGHealthUpdate                  = uint16(0x047f)
	SMSGMoveSetCollisionHeight        = uint16(0x0516)
	opAuthSession                     = uint32(0x01ed)
	opAuthChallenge                   = uint16(0x01ec)
	opAuthResponse                    = uint16(0x01ee)
	authOK                            = byte(12)
	authWaitQueue                     = byte(27)
	maxHandshakeBody                  = 1 << 20
)

var (
	clientToServerSeed = []byte{0xc2, 0xb3, 0x72, 0x3c, 0xc6, 0xae, 0xd9, 0xb5, 0x34, 0x3c, 0x53, 0xee, 0x2f, 0x43, 0x67, 0xce}
	serverToClientSeed = []byte{0xcc, 0x98, 0xae, 0x04, 0xe8, 0x97, 0xea, 0xca, 0x12, 0xdd, 0xc0, 0x93, 0x42, 0x91, 0x53, 0x57}
)

type Packet struct {
	Opcode uint16
	Body   []byte
}

type Conn struct {
	net.Conn
	sendMu sync.Mutex
	send   *rc4.Cipher
	recv   *rc4.Cipher
}

func Authenticate(ctx context.Context, realm legacyauth.Realm, session *legacyauth.Session) (*Conn, error) {
	if session == nil || strings.TrimSpace(session.Username) == "" {
		return nil, fmt.Errorf("legacy world session is empty")
	}
	address := net.JoinHostPort(realm.Address, fmt.Sprint(realm.Port))
	dialer := net.Dialer{Timeout: 5 * time.Second}
	raw, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("connect legacy world %s: %w", address, err)
	}
	connection := &Conn{Conn: raw}
	ok := false
	defer func() {
		if !ok {
			_ = raw.Close()
		}
	}()
	if deadline, hasDeadline := ctx.Deadline(); hasDeadline {
		_ = raw.SetDeadline(deadline)
	} else {
		_ = raw.SetDeadline(time.Now().Add(10 * time.Second))
	}

	challenge, err := connection.readPacket(false)
	if err != nil {
		return nil, fmt.Errorf("read legacy world challenge: %w", err)
	}
	if challenge.Opcode != opAuthChallenge || len(challenge.Body) < 40 {
		return nil, fmt.Errorf("unexpected legacy world challenge opcode=0x%04x bytes=%d", challenge.Opcode, len(challenge.Body))
	}
	serverSeed := binary.LittleEndian.Uint32(challenge.Body[4:8])
	clientSeedBytes := make([]byte, 4)
	if _, err := rand.Read(clientSeedBytes); err != nil {
		return nil, err
	}
	clientSeed := binary.LittleEndian.Uint32(clientSeedBytes)
	body, err := authSessionBody(session.Username, realm.ID, clientSeed, serverSeed, session.SessionKey)
	if err != nil {
		return nil, err
	}
	if err := connection.writeClientPacket(opAuthSession, body, false); err != nil {
		return nil, fmt.Errorf("send legacy world auth session: %w", err)
	}
	if err := connection.initializeCrypt(session.SessionKey[:]); err != nil {
		return nil, err
	}

	for {
		packet, err := connection.readPacket(true)
		if err != nil {
			return nil, fmt.Errorf("read legacy world auth response: %w", err)
		}
		if packet.Opcode != opAuthResponse {
			// AzerothCore may send Warden or addon metadata around authentication.
			continue
		}
		if len(packet.Body) == 0 || (packet.Body[0] != authOK && packet.Body[0] != authWaitQueue) {
			if len(packet.Body) == 0 {
				return nil, fmt.Errorf("legacy world returned an empty auth response")
			}
			return nil, fmt.Errorf("legacy world authentication failed (0x%02x)", packet.Body[0])
		}
		break
	}
	_ = raw.SetDeadline(time.Time{})
	ok = true
	return connection, nil
}

func (c *Conn) ReadPacket() (Packet, error) {
	return c.readPacket(true)
}

func (c *Conn) WritePacket(opcode uint32, body []byte) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return c.writeClientPacket(opcode, body, true)
}

func (c *Conn) initializeCrypt(sessionKey []byte) error {
	var err error
	c.send, err = newDroppedRC4(clientToServerSeed, sessionKey)
	if err != nil {
		return err
	}
	c.recv, err = newDroppedRC4(serverToClientSeed, sessionKey)
	return err
}

func newDroppedRC4(seed, sessionKey []byte) (*rc4.Cipher, error) {
	mac := hmac.New(sha1.New, seed)
	_, _ = mac.Write(sessionKey)
	cipher, err := rc4.NewCipher(mac.Sum(nil))
	if err != nil {
		return nil, err
	}
	drop := make([]byte, 1024)
	cipher.XORKeyStream(drop, drop)
	return cipher, nil
}

func (c *Conn) readPacket(encrypted bool) (Packet, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(c.Conn, header); err != nil {
		return Packet{}, err
	}
	if encrypted {
		if c.recv == nil {
			return Packet{}, fmt.Errorf("legacy receive cipher is not initialized")
		}
		c.recv.XORKeyStream(header, header)
	}
	size := int(binary.BigEndian.Uint16(header[:2]))
	if size < 2 || size-2 > maxHandshakeBody {
		return Packet{}, fmt.Errorf("invalid legacy server packet size %d", size)
	}
	body := make([]byte, size-2)
	if _, err := io.ReadFull(c.Conn, body); err != nil {
		return Packet{}, err
	}
	return Packet{Opcode: binary.LittleEndian.Uint16(header[2:4]), Body: body}, nil
}

func (c *Conn) writeClientPacket(opcode uint32, body []byte, encrypted bool) error {
	if len(body) > int(^uint16(0))-4 {
		return fmt.Errorf("legacy client packet body is too large: %d", len(body))
	}
	header := make([]byte, 6)
	binary.BigEndian.PutUint16(header[:2], uint16(len(body)+4))
	binary.LittleEndian.PutUint32(header[2:], opcode)
	if encrypted {
		if c.send == nil {
			return fmt.Errorf("legacy send cipher is not initialized")
		}
		c.send.XORKeyStream(header, header)
	}
	if err := writeAll(c.Conn, header); err != nil {
		return err
	}
	return writeAll(c.Conn, body)
}

func authSessionBody(username string, realmID, clientSeed, serverSeed uint32, sessionKey [40]byte) ([]byte, error) {
	username = strings.ToUpper(username)
	var digestInput bytes.Buffer
	digestInput.WriteString(username)
	_ = binary.Write(&digestInput, binary.LittleEndian, uint32(0))
	_ = binary.Write(&digestInput, binary.LittleEndian, clientSeed)
	_ = binary.Write(&digestInput, binary.LittleEndian, serverSeed)
	digestInput.Write(sessionKey[:])
	digest := sha1.Sum(digestInput.Bytes())

	addon, err := emptyAddonInfo()
	if err != nil {
		return nil, err
	}
	var body bytes.Buffer
	_ = binary.Write(&body, binary.LittleEndian, Build335a)
	_ = binary.Write(&body, binary.LittleEndian, realmID)
	body.WriteString(username)
	body.WriteByte(0)
	_ = binary.Write(&body, binary.LittleEndian, uint32(0)) // login server type
	_ = binary.Write(&body, binary.LittleEndian, clientSeed)
	_ = binary.Write(&body, binary.LittleEndian, uint32(1)) // region
	_ = binary.Write(&body, binary.LittleEndian, uint32(1)) // battlegroup/site
	_ = binary.Write(&body, binary.LittleEndian, realmID)
	_ = binary.Write(&body, binary.LittleEndian, uint64(0)) // DOS response
	body.Write(digest[:])
	body.Write(addon)
	return body.Bytes(), nil
}

func emptyAddonInfo() ([]byte, error) {
	// AzerothCore WorldSession::ReadAddonsInfo inflates this blob then reads:
	//   uint32 addonsCount
	//   repeated addon entries
	//   uint32 currentTime
	// A 4-byte zero payload (count=0, no timestamp) overruns the Debug ByteBuffer
	// and aborts worldserver after "authenticated successfully".
	plain := make([]byte, 8)
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(plain); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	result := make([]byte, 4+compressed.Len())
	binary.LittleEndian.PutUint32(result[:4], uint32(len(plain)))
	copy(result[4:], compressed.Bytes())
	return result, nil
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := w.Write(data)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}
