package bnet

import "google.golang.org/protobuf/encoding/protowire"

const wowProgram = uint64(0x576f57)

// EncodeAccountStateResponse returns the privacy subset requested by the
// 3.4.3 glue screen. False values are deliberately present on the wire.
func EncodeAccountStateResponse() []byte {
	privacy := appendVarintField(nil, 3, 0) // is_using_rid
	privacy = appendVarintField(privacy, 4, 0)
	privacy = appendVarintField(privacy, 5, 1) // hidden from friend finder
	state := appendBytesField(nil, 2, privacy)

	tags := protowire.AppendTag(nil, 3, protowire.Fixed32Type)
	tags = protowire.AppendFixed32(tags, 0xd7ca834d)
	response := appendBytesField(nil, 1, state)
	return appendBytesField(response, 2, tags)
}

// EncodeGameAccountStateResponse supplies the WoW account name and an
// unbanned status. AzerothCore enforces the actual account state during SRP.
func EncodeGameAccountStateResponse(accountName string) []byte {
	level := appendBytesField(nil, 8, []byte(accountName))
	level = appendVarintField(level, 9, wowProgram)
	status := appendVarintField(nil, 4, 0)   // is_suspended
	status = appendVarintField(status, 5, 0) // is_banned
	status = appendVarintField(status, 6, 0) // suspension_expires
	status = appendVarintField(status, 7, wowProgram)
	state := appendBytesField(nil, 1, level)
	state = appendBytesField(state, 3, status)

	tags := protowire.AppendTag(nil, 2, protowire.Fixed32Type)
	tags = protowire.AppendFixed32(tags, 0x5c46d483)
	tags = protowire.AppendTag(tags, 4, protowire.Fixed32Type)
	tags = protowire.AppendFixed32(tags, 0x98b75f99)
	response := appendBytesField(nil, 1, state)
	return appendBytesField(response, 2, tags)
}
