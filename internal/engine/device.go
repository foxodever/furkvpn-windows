package engine

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"

	"furkvpn/internal/sysinfo"
)

// hashedMachineID turns Windows' id of this installation into the app's device id, so the panel
// never sees the id itself; a random one when Windows has none.
func hashedMachineID() string {
	if guid := sysinfo.MachineGUID(); guid != "" {
		sum := sha256.Sum256([]byte("furkvpn-hwid:" + guid))
		return hex.EncodeToString(sum[:16])
	}
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
