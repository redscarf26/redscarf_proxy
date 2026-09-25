package modernworld

import "testing"

func TestParseCancelTrade(t *testing.T) {
	if err := ParseCancelTrade(nil); err != nil {
		t.Fatal(err)
	}
	if err := ParseCancelTrade([]byte{0}); err == nil {
		t.Fatal("non-empty cancel-trade accepted")
	}
}
