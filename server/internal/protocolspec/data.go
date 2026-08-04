package protocolspec

import _ "embed"

//go:embed protocol_specs.json
var embeddedData []byte

func Data() []byte {
	return append([]byte(nil), embeddedData...)
}
