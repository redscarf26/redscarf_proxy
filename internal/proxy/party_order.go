package proxy

import "slices"

type partyOrderKey struct {
	realm uint32
	group uint64
}

// stablePartyOrder is shared by every connection to a group. Remember absent
// members too: reconnects and delayed snapshots must not move existing members.
// Only current members are returned, so leaving never creates a phantom row.
// History lasts for this proxy process; a new group GUID gets a fresh order.
func (s *Server) stablePartyOrder(key partyOrderKey, leader uint64, members []uint64) []uint64 {
	s.partyOrdersMu.Lock()
	defer s.partyOrdersMu.Unlock()
	if s.partyOrders == nil {
		s.partyOrders = make(map[partyOrderKey][]uint64)
	}
	history := s.partyOrders[key]
	present := make(map[uint64]bool, len(members))
	for _, guid := range members {
		present[guid] = true
	}
	if len(history) == 0 && present[leader] {
		history = append(history, leader)
	}
	// Sorting newcomers makes the initial result independent of which viewer's
	// leave-one-out legacy packet arrives first. Existing ranks never change.
	newcomers := slices.Clone(members)
	slices.Sort(newcomers)
	for _, guid := range newcomers {
		if !slices.Contains(history, guid) {
			history = append(history, guid)
		}
	}
	s.partyOrders[key] = history
	order := make([]uint64, 0, len(present))
	for _, guid := range history {
		if present[guid] {
			order = append(order, guid)
		}
	}
	return order
}
