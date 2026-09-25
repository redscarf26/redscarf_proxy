package modernworld

const (
	CMSGGameObjectUse       = uint16(0x34EE)
	CMSGGameObjectReportUse = uint16(0x34EF)
)

func ParseGameObjectUse(body []byte) (GUID128, error) {
	return ParsePackedGUID128Exact(body)
}
