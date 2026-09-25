package modernworld

import "testing"

func TestCurrencyIDForEmblemOfFrost(t *testing.T) {
	id, ok := CurrencyIDForItem(49426)
	if !ok || id != 341 {
		t.Fatalf("emblem of frost currency=%d ok=%v, want 341", id, ok)
	}
	if _, ok := CurrencyIDForItem(43308); ok {
		t.Fatalf("deprecated honor item must not become a currency")
	}
}

func TestCollectCurrenciesSumsTokenStacks(t *testing.T) {
	player := map[int]uint32{
		legacyPlayerHonorCurrency: 80,
		legacyPlayerArenaCurrency: 0,
	}
	items := []map[int]uint32{
		{legacyObjectEntry: 49426, legacyItemStackCount: 24},
		{legacyObjectEntry: 49426, legacyItemStackCount: 3},
		{legacyObjectEntry: 12345, legacyItemStackCount: 9},
	}
	got := CollectCurrencies(0, player, items)
	if got[341] != 27 || got[CurrencyHonor] != 80 || got[CurrencyArena] != 0 {
		t.Fatalf("currencies=%v", got)
	}
	if _, ok := got[12345]; ok {
		t.Fatalf("ordinary item leaked into currencies")
	}
	const owner = uint64(0x00000000000000ab)
	foreign := map[int]uint32{
		legacyObjectEntry: 49426, legacyItemStackCount: 5,
		legacyItemOwner: uint32(owner + 1),
	}
	owned := map[int]uint32{
		legacyObjectEntry: 49426, legacyItemStackCount: 2,
		legacyItemOwner: uint32(owner),
	}
	filtered := CollectCurrencies(owner, nil, []map[int]uint32{foreign, owned})
	if filtered[341] != 2 {
		t.Fatalf("owned frost=%d, want 2", filtered[341])
	}
}

func TestDiffCurrenciesClearsSpentToken(t *testing.T) {
	prev := map[uint32]uint32{341: 24, CurrencyHonor: 80}
	next := map[uint32]uint32{CurrencyHonor: 80}
	records := DiffCurrencies(prev, next)
	if len(records) != 1 || records[0].Type != 341 || records[0].Quantity != 0 {
		t.Fatalf("records=%v, want frost cleared", records)
	}
	if got := DiffCurrencies(nil, map[uint32]uint32{CurrencyArena: 0}); len(got) != 0 {
		t.Fatalf("initial zero arena was sent: %v", got)
	}
}

func TestEncodeSetupCurrencyFrost(t *testing.T) {
	body := EncodeSetupCurrency([]CurrencyRecord{{Type: 341, Quantity: 24}})
	want := []byte{
		1, 0, 0, 0,
		0x55, 0x01, 0, 0,
		24, 0, 0, 0,
		0, 0,
	}
	if len(body) != len(want) {
		t.Fatalf("len=%d body=%x", len(body), body)
	}
	for i := range want {
		if body[i] != want[i] {
			t.Fatalf("body=%x want=%x", body, want)
		}
	}
	if EncodeSetupCurrency(nil) != nil {
		t.Fatal("empty currency list must not produce a packet")
	}
}
