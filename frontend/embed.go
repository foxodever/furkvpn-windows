// Package frontend holds the app's built interface (frontend/dist), embedded into FurkVPN.exe.
package frontend

import "embed"

// Dist is the output of "npm run build".
//
//go:embed all:dist
var Dist embed.FS
