package modernworld

// Descriptor bit order and payload order differ for parallel arrays. WPP's
// UpdateFieldsHandler343 reads all properties of element i before element i+1.
func interleavedOrder(bit, start, slots, arrays int) int {
	return start + (bit-start)%slots*arrays + (bit-start)/slots
}

func unitDeltaOrder(bit int) int {
	for _, g := range [][3]int{{117, 10, 5}, {175, 5, 3}, {191, 7, 3}, {213, 7, 2}} {
		if bit >= g[0] && bit < g[0]+g[1]*g[2] {
			return interleavedOrder(bit, g[0], g[1], g[2])
		}
	}
	return bit
}

func activeDeltaOrder(bit int) int {
	for _, g := range [][3]int{{270, 7, 4}, {550, 12, 2}, {1513, 6, 2}} {
		if bit >= g[0] && bit < g[0]+g[1]*g[2] {
			return interleavedOrder(bit, g[0], g[1], g[2])
		}
	}
	return bit
}

func updateObjectTypeForUnit(creature bool) uint8 {
	if creature {
		return 3
	}
	return 4
}
