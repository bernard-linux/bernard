package discovery

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestBestRoutePrefersFastestLink(t *testing.T) {
	rs := []Route{
		{Addr: "wifi", Link: LinkWifi, Speed: 0},
		{Addr: "cable-100", Link: LinkEthernet, Speed: 100},
		{Addr: "cable-1000", Link: LinkEthernet, Speed: 1000},
		{Addr: "tb", Link: LinkThunderbolt, Speed: 0},
	}
	sortRoutes(rs)
	if rs[0].Addr != "tb" || rs[1].Addr != "cable-1000" || rs[len(rs)-1].Addr != "wifi" {
		t.Fatalf("ordre inattendu : %+v", rs)
	}
}

func TestAnnounceAndListen(t *testing.T) {
	// Port libre pour ne pas dépendre du port officiel dans les tests.
	probe, _ := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	port := probe.LocalAddr().(*net.UDPAddr).Port
	probe.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Announce(ctx, Beacon{ID: "abc", Name: "nouveau-pc", Port: 40000},
		&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})

	targets, err := Listen(context.Background(), port, 1500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].Name != "nouveau-pc" || targets[0].Best().Addr != "127.0.0.1:40000" {
		t.Fatalf("cible non trouvée : %+v", targets)
	}
}
