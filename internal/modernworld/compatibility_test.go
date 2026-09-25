package modernworld

import "testing"

func TestValidateModernOnlyFeatureRequests(t *testing.T) {
	tests := []struct {
		opcode uint16
		body   []byte
	}{
		{CMSGGuildSetAchievementTracking, make([]byte, 4)},
		{CMSGViolenceLevel, []byte{2}},
		{CMSGRequestPVPRewards, nil},
		{CMSGGetItemPurchaseData, appendPackedGUID128(nil, 0x1234, 1)},
		{CMSGItemPurchaseRefund, appendPackedGUID128(nil, 0x1234, 1)},
		{CMSGAddToy, appendPackedGUID128(nil, 0x55, 1)},
		{CMSGQueuedMessagesEnd, make([]byte, 4)},
		{CMSGDiscardedTimeSyncAcks, make([]byte, 4)},
		{CMSGReportEnabledAddons, make([]byte, 4784)},
	}
	for _, test := range tests {
		if err := ValidateModernOnlyFeatureRequest(test.opcode, test.body); err != nil {
			t.Errorf("opcode 0x%04x: %v", test.opcode, err)
		}
	}
	if err := ValidateModernOnlyFeatureRequest(CMSGViolenceLevel, nil); err == nil {
		t.Fatal("expected malformed violence-level error")
	}
	itemGUID := appendPackedGUID128(nil, 0x12345678, 1)
	if err := ValidateModernOnlyFeatureRequest(CMSGGetItemPurchaseData, itemGUID); err != nil {
		t.Fatalf("variable-length item GUID rejected: %v", err)
	}
	if err := ValidateModernOnlyFeatureRequest(CMSGGetItemPurchaseData, append(itemGUID, 0)); err == nil {
		t.Fatal("expected malformed item GUID error")
	}
	if err := ValidateModernOnlyFeatureRequest(CMSGGetItemPurchaseData, nil); err == nil {
		t.Fatal("expected empty item GUID error")
	}
	if err := ValidateModernOnlyFeatureRequest(CMSGDFGetSystemInfo, []byte{0}); err == nil {
		t.Fatal("expected dungeon-finder system info to leave the swallowed-probe list")
	}
	if err := ValidateModernOnlyFeatureRequest(CMSGDFGetJoinStatus, nil); err == nil {
		t.Fatal("expected dungeon-finder join status to leave the swallowed-probe list")
	}
	if err := ValidateModernOnlyFeatureRequest(CMSGRequestBattlefieldStatus, nil); err == nil {
		t.Fatal("expected battlefield status request to leave the swallowed-probe list")
	}
	if err := ValidateModernOnlyFeatureRequest(0xffff, nil); err == nil {
		t.Fatal("expected unknown compatibility opcode error")
	}
}
