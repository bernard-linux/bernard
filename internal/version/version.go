// Package version porte le numéro de version, injecté à la compilation :
//
//	go build -ldflags "-X github.com/bernard-linux/bernard/internal/version.Version=1.0.0"
package version

// Version est "dev" hors compilation officielle.
var Version = "dev"
