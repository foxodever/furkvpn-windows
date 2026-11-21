//go:build !windows

package core

// ReinstallAdapterDriver is Windows' matter: elsewhere there is no adapter.
func ReinstallAdapterDriver() error { return nil }
