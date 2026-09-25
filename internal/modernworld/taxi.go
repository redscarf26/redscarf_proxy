package modernworld

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
)

const (
	CMSGTaxiNodeStatusQuery     = uint16(13480)
	CMSGEnableTaxiNode          = uint16(13481)
	CMSGTaxiQueryAvailableNodes = uint16(13482)
	CMSGActivateTaxi            = uint16(13483)
	CMSGMoveSplineDone          = uint16(14872)

	SMSGTaxiNodeStatus    = uint16(9852)
	SMSGActivateTaxiReply = uint16(9853)
	SMSGNewTaxiPath       = uint16(9854)
	SMSGShowTaxiNodes     = uint16(9933)

	modernTaxiMaskBytes = 56
)

type ActivateTaxiRequest struct {
	FlightMaster  GUID128
	Destination   uint32
	GroundMountID uint32
	FlyingMountID uint32
}

func ParseTaxiNPC(body []byte) (GUID128, error) {
	return ParsePackedGUID128Exact(body)
}

func ParseActivateTaxi(body []byte) (ActivateTaxiRequest, error) {
	var request ActivateTaxiRequest
	low, high, consumed, err := readPackedGUID128(body)
	if err != nil {
		return request, fmt.Errorf("read flight-master GUID: %w", err)
	}
	if len(body)-consumed != 12 {
		return request, fmt.Errorf("activate-taxi has %d bytes after GUID, want 12", len(body)-consumed)
	}
	request.FlightMaster = GUID128{Low: low, High: high}
	request.Destination = binary.LittleEndian.Uint32(body[consumed:])
	request.GroundMountID = binary.LittleEndian.Uint32(body[consumed+4:])
	request.FlyingMountID = binary.LittleEndian.Uint32(body[consumed+8:])
	return request, nil
}

func EncodeLegacyTaxiNPC(guid uint64) []byte {
	return binary.LittleEndian.AppendUint64(nil, guid)
}

func EncodeLegacyActivateTaxi(guid uint64, source, destination uint32) []byte {
	body := binary.LittleEndian.AppendUint64(nil, guid)
	body = binary.LittleEndian.AppendUint32(body, source)
	return binary.LittleEndian.AppendUint32(body, destination)
}

func EncodeLegacyActivateTaxiExpress(guid uint64, route []uint32) []byte {
	body := binary.LittleEndian.AppendUint64(nil, guid)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(route)))
	for _, node := range route {
		body = binary.LittleEndian.AppendUint32(body, node)
	}
	return body
}

type LegacyTaxiMenu struct {
	HasWindowInfo bool
	FlightMaster  uint64
	CurrentNode   uint32
	NodeMask      []byte
}

func ParseLegacyTaxiMenu(body []byte) (LegacyTaxiMenu, error) {
	var menu LegacyTaxiMenu
	if len(body) < 4 {
		return menu, fmt.Errorf("legacy show-taxi-nodes has %d bytes, want at least 4", len(body))
	}
	menu.HasWindowInfo = binary.LittleEndian.Uint32(body) != 0
	position := 4
	if menu.HasWindowInfo {
		if len(body) < 16 {
			return menu, fmt.Errorf("legacy show-taxi-nodes window is truncated")
		}
		menu.FlightMaster = binary.LittleEndian.Uint64(body[position:])
		menu.CurrentNode = binary.LittleEndian.Uint32(body[position+8:])
		position += 12
	}
	menu.NodeMask = append([]byte(nil), body[position:]...)
	return menu, nil
}

func EncodeShowTaxiNodes(menu LegacyTaxiMenu, flightMaster GUID128) []byte {
	bits := newBitWriter(nil)
	bits.writeBit(menu.HasWindowInfo)
	body := bits.flush()
	body = binary.LittleEndian.AppendUint32(body, modernTaxiMaskBytes/8)
	body = binary.LittleEndian.AppendUint32(body, modernTaxiMaskBytes/8)
	if menu.HasWindowInfo {
		body = appendPackedGUID128(body, flightMaster.Low, flightMaster.High)
		body = binary.LittleEndian.AppendUint32(body, menu.CurrentNode)
	}
	mask := make([]byte, modernTaxiMaskBytes)
	copy(mask, menu.NodeMask)
	body = append(body, mask...)
	return append(body, mask...)
}

func ParseLegacyTaxiNodeStatus(body []byte) (uint64, bool, error) {
	if len(body) != 9 {
		return 0, false, fmt.Errorf("legacy taxi-node-status has %d bytes, want 9", len(body))
	}
	if body[8] > 1 {
		return 0, false, fmt.Errorf("legacy taxi-node-status has invalid learned flag %d", body[8])
	}
	return binary.LittleEndian.Uint64(body), body[8] != 0, nil
}

func EncodeTaxiNodeStatus(flightMaster GUID128, learned bool) []byte {
	body := appendPackedGUID128(nil, flightMaster.Low, flightMaster.High)
	status := uint32(2) // Unlearned
	if learned {
		status = 1 // Learned
	}
	bits := newBitWriter(body)
	bits.writeBits(status, 2)
	return bits.flush()
}

func ParseLegacyActivateTaxiReply(body []byte) (uint32, error) {
	if len(body) != 4 {
		return 0, fmt.Errorf("legacy activate-taxi-reply has %d bytes, want 4", len(body))
	}
	reply := binary.LittleEndian.Uint32(body)
	if reply > 12 {
		return 0, fmt.Errorf("legacy activate-taxi-reply has invalid result %d", reply)
	}
	return reply, nil
}

func EncodeActivateTaxiReply(reply uint32) []byte {
	bits := newBitWriter(nil)
	bits.writeBits(reply, 4)
	return bits.flush()
}

type taxiEdge struct {
	to   uint32
	cost int
}

//go:embed taxi_paths_3.csv
var taxiPathsCSV []byte

var taxiPathGraph, taxiPathGraphErr = loadTaxiPathGraph(taxiPathsCSV)

func loadTaxiPathGraph(data []byte) (map[uint32][]taxiEdge, error) {
	reader := csv.NewReader(bytes.NewReader(data))
	if _, err := reader.Read(); err != nil {
		return nil, fmt.Errorf("read taxi-path header: %w", err)
	}
	graph := make(map[uint32][]taxiEdge)
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read taxi path: %w", err)
		}
		if len(record) != 4 {
			return nil, fmt.Errorf("taxi path has %d columns, want 4", len(record))
		}
		from, err1 := strconv.ParseUint(record[1], 10, 32)
		to, err2 := strconv.ParseUint(record[2], 10, 32)
		cost, err3 := strconv.Atoi(record[3])
		if err1 != nil || err2 != nil || err3 != nil {
			return nil, fmt.Errorf("parse taxi path %q", record)
		}
		if cost <= 0 {
			cost = 1
		}
		graph[uint32(from)] = append(graph[uint32(from)], taxiEdge{to: uint32(to), cost: cost})
	}
	return graph, nil
}

func taxiNodeUsable(mask []byte, node uint32) bool {
	if node == 0 {
		return false
	}
	index := (node - 1) / 8
	return index < uint32(len(mask)) && mask[index]&(1<<((node-1)%8)) != 0
}

// ShortestTaxiRoute returns a directed route containing source and destination.
// It uses the build-54261 taxi graph and excludes nodes absent from the server's
// usable-node mask.
func ShortestTaxiRoute(source, destination uint32, usable []byte) ([]uint32, error) {
	if taxiPathGraphErr != nil {
		return nil, taxiPathGraphErr
	}
	if source == 0 || destination == 0 || source == destination {
		return nil, fmt.Errorf("invalid taxi route %d -> %d", source, destination)
	}
	const infinity = int(^uint(0) >> 1)
	distance := map[uint32]int{source: 0}
	previous := make(map[uint32]uint32)
	visited := make(map[uint32]bool)
	for {
		current, best := uint32(0), infinity
		for node, value := range distance {
			if !visited[node] && value < best {
				current, best = node, value
			}
		}
		if current == 0 || current == destination {
			break
		}
		visited[current] = true
		for _, edge := range taxiPathGraph[current] {
			if edge.to != destination && !taxiNodeUsable(usable, edge.to) {
				continue
			}
			candidate := best + edge.cost
			if old, ok := distance[edge.to]; !ok || candidate < old {
				distance[edge.to] = candidate
				previous[edge.to] = current
			}
		}
	}
	if _, found := distance[destination]; !found {
		return nil, fmt.Errorf("no usable taxi route %d -> %d", source, destination)
	}
	route := []uint32{destination}
	for route[len(route)-1] != source {
		parent, found := previous[route[len(route)-1]]
		if !found {
			return nil, fmt.Errorf("incomplete taxi route %d -> %d", source, destination)
		}
		route = append(route, parent)
	}
	for left, right := 0, len(route)-1; left < right; left, right = left+1, right-1 {
		route[left], route[right] = route[right], route[left]
	}
	return route, nil
}
