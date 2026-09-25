package modernworld

import "fmt"

const (
	SMSGLoadEquipmentSet                = uint16(0x270E)
	CMSGGuildSetAchievementTracking     = uint16(0x306F)
	CMSGGuildBankRemainingWithdrawQuery = uint16(0x3083)
	CMSGViolenceLevel                   = uint16(0x3187)
	CMSGRequestPVPRewards               = uint16(0x3196)
	CMSGQueryCountdownTimer             = uint16(0x31AA)
	CMSGRequestForcedReactions          = uint16(0x3205)
	CMSGRequestLFGListBlacklist         = uint16(0x32A4)
	CMSGRequestConquestFormulaConstants = uint16(0x32B4)
	CMSGOverrideScreenFlash             = uint16(0x351E)
	CMSGGetItemPurchaseData             = uint16(0x352F)
	CMSGItemPurchaseRefund              = uint16(0x3530)
	CMSGAddToy                          = uint16(0x3299)
	CMSGRequestBattlefieldStatus        = uint16(0x35DD)
	CMSGRequestRatedPVPInfo             = uint16(0x35E4)
	CMSGLFGListGetStatus                = uint16(0x360D)
	CMSGDFGetSystemInfo                 = uint16(0x3615)
	CMSGDFGetJoinStatus                 = uint16(0x3616)
	CMSGRequestBattlePetJournal         = uint16(0x3625)
	CMSGCalendarGetNumPending           = uint16(0x367C)
	CMSGGMTicketGetCaseStatus           = uint16(0x368F)
	CMSGGetAccountCharacterList         = uint16(0x36BF)
	CMSGBattlePayGetProductList         = uint16(0x36C4)
	CMSGBattlePayGetPurchaseList        = uint16(0x36C5)
	CMSGGetUndeleteCooldownStatus       = uint16(0x36E7)
	CMSGUpdateVASPurchaseStates         = uint16(0x36FB)
	CMSGBattlenetRequest                = uint16(0x36FD)
	CMSGReportEnabledAddons             = uint16(0x3706)
	CMSGReportClientVariables           = uint16(0x3707)
	CMSGReportKeybindingExecutionCounts = uint16(0x3708)
	CMSGSocialContractRequest           = uint16(0x374C)
	CMSGQueuedMessagesEnd               = uint16(0x376C)
	CMSGDiscardedTimeSyncAcks           = uint16(0x3A41)
)

// ValidateModernOnlyFeatureRequest recognizes modern-client probes for systems
// that do not exist in 3.3.5. FeatureSystemStatus advertises these systems as
// unavailable; consuming their startup probes prevents invalid legacy opcodes.
func ValidateModernOnlyFeatureRequest(opcode uint16, body []byte) error {
	expected := -1
	switch opcode {
	case CMSGGuildSetAchievementTracking, CMSGQueryCountdownTimer,
		CMSGQueuedMessagesEnd, CMSGDiscardedTimeSyncAcks:
		expected = 4
	case CMSGViolenceLevel, CMSGOverrideScreenFlash:
		expected = 1
	case CMSGGetItemPurchaseData, CMSGItemPurchaseRefund, CMSGAddToy:
		if len(body) == 0 {
			return fmt.Errorf("modern-only opcode 0x%04x has an empty item GUID", opcode)
		}
		if _, err := ParsePackedGUID128Exact(body); err != nil {
			return fmt.Errorf("modern-only opcode 0x%04x has invalid item GUID: %w", opcode, err)
		}
		return nil
	case CMSGGetAccountCharacterList:
		expected = 5
	case CMSGRequestPVPRewards,
		CMSGRequestLFGListBlacklist, CMSGRequestConquestFormulaConstants,
		CMSGRequestRatedPVPInfo,
		CMSGLFGListGetStatus, CMSGRequestBattlePetJournal,
		CMSGCalendarGetNumPending, CMSGGMTicketGetCaseStatus,
		CMSGBattlePayGetProductList, CMSGBattlePayGetPurchaseList,
		CMSGGetUndeleteCooldownStatus, CMSGUpdateVASPurchaseStates,
		CMSGSocialContractRequest:
		expected = 0
	case CMSGBattlenetRequest, CMSGReportEnabledAddons, CMSGReportClientVariables,
		CMSGReportKeybindingExecutionCounts:
		if len(body) > 1<<20 {
			return fmt.Errorf("modern-only opcode 0x%04x has %d bytes, maximum is %d", opcode, len(body), 1<<20)
		}
		return nil
	default:
		return fmt.Errorf("opcode 0x%04x is not a recognized modern-only feature request", opcode)
	}
	if len(body) != expected {
		return fmt.Errorf("modern-only opcode 0x%04x has %d bytes, want %d", opcode, len(body), expected)
	}
	return nil
}
