package proxy

import "redscarf/internal/modernworld"

func (session *proxySession) sendCurrencyUpdate() error {
	body := session.takeCurrencyPacket()
	if len(body) == 0 {
		return nil
	}
	return session.sendInstance(modernworld.Packet{Opcode: modernworld.SMSGSetupCurrency, Body: body})
}

// takeCurrencyPacket builds an SMSG_SETUP_CURRENCY body when cached
// currency-token items or honor/arena fields differ from the last packet.
// The caller must not hold worldMu; sendInstance locks it.
func (session *proxySession) takeCurrencyPacket() []byte {
	session.worldMu.Lock()
	defer session.worldMu.Unlock()
	items := make([]map[int]uint32, 0)
	for guid, objectType := range session.objectTypes {
		if objectType != 1 {
			continue
		}
		items = append(items, session.objectFields[guid])
	}
	next := modernworld.CollectCurrencies(session.currentCharacter, session.objectFields[session.currentCharacter], items)
	records := modernworld.DiffCurrencies(session.currencyQty, next)
	session.currencyQty = next
	return modernworld.EncodeSetupCurrency(records)
}
