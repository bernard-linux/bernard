//go:build !linux

package discovery

import "syscall"

// linkInfo : détection non encore implémentée hors Linux (agents V1.1/V1.2).
func linkInfo(string) (string, int) { return LinkOther, 0 }

func bindToDevice(string) func(string, string, syscall.RawConn) error { return nil }
