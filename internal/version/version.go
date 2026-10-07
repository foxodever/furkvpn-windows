// Package version is the app's version, set at build time:
//
//	-ldflags "-X furkvpn/internal/version.Name=1.0.0 -X furkvpn/internal/version.Code=1"
package version

import "strconv"

var (
	// Name is shown to people ("1.0.0").
	Name = "0.0.0-dev"
	// Code grows with every release; updates compare it, like Android's versionCode.
	Code = "0"
)

// CodeInt is Code as a number.
func CodeInt() int {
	n, _ := strconv.Atoi(Code)
	return n
}
