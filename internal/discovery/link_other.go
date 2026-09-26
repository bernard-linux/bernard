//go:build !linux

package discovery

// linkInfo : détection non encore implémentée hors Linux (agents V1.1/V1.2).
func linkInfo(string) (string, int) { return LinkOther, 0 }
