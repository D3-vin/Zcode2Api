package gateway

import (
	_ "embed"
	"encoding/json"
	"sync"
)

// The system asset is EMBEDDED, not read from disk at runtime: the disk
// variant already bit us once — assets/zcode_system.json was referenced by
// code but never present in the repo, so the system injection silently
// no-oped and every request left without a system prompt.
//
// The blocks are the 23mac.har identity (mac_ping_system.json). The short
// zcode_system.json stub (Environment: unknown) 3012's on /messages.

//go:embed assets/mac_ping_system.json
var embeddedSystem []byte

var (
	zcodeSystemOnce sync.Once
	zcodeSystem     []json.RawMessage
	zcodeSystemErr  error
)

// loadZcodeSystem returns the official ZCode system identity blocks every
// chat request must carry.
func loadZcodeSystem() ([]json.RawMessage, error) {
	zcodeSystemOnce.Do(func() {
		zcodeSystemErr = json.Unmarshal(embeddedSystem, &zcodeSystem)
	})
	return zcodeSystem, zcodeSystemErr
}
