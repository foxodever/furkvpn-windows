//go:build !windows

package engine

import "os"

func openShareFirewall(httpPort, socksPort int) {}

func closeShareFirewall() {}

// ResetFirewall does nothing on other systems.
func ResetFirewall() {}

// SecureDirs only creates the folders on other systems.
func SecureDirs(dataDir, updatesDir string) error {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	return os.MkdirAll(updatesDir, 0o755)
}
