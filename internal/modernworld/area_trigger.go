package modernworld

import (
	"encoding/binary"
	"fmt"
)

const CMSGAreaTrigger = uint16(0x31D6)

type AreaTriggerRequest struct {
	ID         uint32
	Entered    bool
	FromClient bool
}

func ParseAreaTrigger(body []byte) (AreaTriggerRequest, error) {
	if len(body) != 5 {
		return AreaTriggerRequest{}, fmt.Errorf("area-trigger has %d bytes, want 5", len(body))
	}
	return AreaTriggerRequest{
		ID:         binary.LittleEndian.Uint32(body[:4]),
		Entered:    body[4]&0x80 != 0,
		FromClient: body[4]&0x40 != 0,
	}, nil
}

func EncodeLegacyAreaTrigger(request AreaTriggerRequest) []byte {
	return binary.LittleEndian.AppendUint32(nil, request.ID)
}
