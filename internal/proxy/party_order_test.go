package proxy

import (
	"slices"
	"sync"
	"testing"
)

func TestStablePartyOrderLifecycle(t *testing.T) {
	s := &Server{}
	key := partyOrderKey{realm: 1, group: 100}
	for _, tc := range []struct {
		name          string
		leader        uint64
		members, want []uint64
	}{
		{"leader precedes smaller GUID", 7, []uint64{3, 7}, []uint64{7, 3}},
		{"other viewer", 7, []uint64{7, 3}, []uint64{7, 3}},
		{"lower GUID joins", 7, []uint64{1, 3, 7}, []uint64{7, 3, 1}},
		{"leader changes", 3, []uint64{1, 3, 7}, []uint64{7, 3, 1}},
		{"member leaves", 3, []uint64{1, 3}, []uint64{3, 1}},
		{"delayed older snapshot", 7, []uint64{3, 7}, []uint64{7, 3}},
		{"member rejoins", 3, []uint64{1, 3, 7}, []uint64{7, 3, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.stablePartyOrder(key, tc.leader, tc.members); !slices.Equal(got, tc.want) {
				t.Fatalf("order=%v want=%v", got, tc.want)
			}
		})
	}
	for _, other := range []partyOrderKey{{realm: 2, group: 100}, {realm: 1, group: 101}} {
		if got := s.stablePartyOrder(other, 3, []uint64{7, 3}); !slices.Equal(got, []uint64{3, 7}) {
			t.Fatalf("independent group order=%v", got)
		}
	}
}

func TestStablePartyOrderConcurrentRecipients(t *testing.T) {
	s := &Server{}
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			members := []uint64{7, 3, 1}
			if i%2 == 0 {
				slices.Reverse(members)
			}
			got := s.stablePartyOrder(partyOrderKey{realm: 1, group: 100}, 7, members)
			if !slices.Equal(got, []uint64{7, 1, 3}) {
				t.Errorf("order=%v", got)
			}
		}(i)
	}
	wg.Wait()
}
