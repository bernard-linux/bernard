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

func TestWaitFindsLateTarget(t *testing.T) {
	probe, _ := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	port := probe.LocalAddr().(*net.UDPAddr).Port
	probe.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// La cible n'apparaît qu'après plusieurs fenêtres d'écoute vides.
	go func() {
		time.Sleep(7 * time.Second)
		Announce(ctx, Beacon{ID: "tard", Name: "cible-tardive", Port: 40001},
			&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	}()
	wctx, wcancel := context.WithTimeout(ctx, 20*time.Second)
	defer wcancel()
	targets, err := Wait(wctx, port, nil)
	if err != nil || len(targets) != 1 || targets[0].Name != "cible-tardive" {
		t.Fatalf("cible tardive non trouvée : %+v, %v", targets, err)
	}
}

func TestWaitStopsOnCancel(t *testing.T) {
	probe, _ := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	port := probe.LocalAddr().(*net.UDPAddr).Port
	probe.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	begin := time.Now()
	if _, err := Wait(ctx, port, nil); err == nil {
		t.Fatal("Wait doit renvoyer une erreur à l'annulation")
	}
	if time.Since(begin) > 5*time.Second {
		t.Fatal("Wait n'a pas réagi à l'annulation")
	}
}

func TestTrackerBetterRoute(t *testing.T) {
	tr := &Tracker{names: map[string]string{"x": "nouveau"}, seen: map[string]map[string]seenRoute{"x": {
		"192.168.1.20:51516": {Route{Addr: "192.168.1.20:51516", Link: LinkWifi}, time.Now()},
	}}}
	if _, ok := tr.BetterRoute("x", "192.168.1.20:51516"); ok {
		t.Fatal("aucune meilleure liaison tant que seul le Wi-Fi existe")
	}
	tr.seen["x"]["192.168.1.21:51516"] = seenRoute{Route{Addr: "192.168.1.21:51516", Link: LinkEthernet, Speed: 1000}, time.Now()}
	r, ok := tr.BetterRoute("x", "192.168.1.20:51516")
	if !ok || r.Addr != "192.168.1.21:51516" {
		t.Fatalf("le câble aurait dû être proposé : %+v %v", r, ok)
	}
	if _, ok := tr.BetterRoute("x", "192.168.1.21:51516"); ok {
		t.Fatal("déjà sur le câble : rien à proposer")
	}
	if _, ok := tr.BetterRoute("x", "10.0.0.5:51516"); ok {
		t.Fatal("adresse saisie à la main : pas de bascule")
	}
	// Liaison plus vue depuis longtemps : oubliée.
	tr.seen["x"]["192.168.1.21:51516"] = seenRoute{Route{Addr: "192.168.1.21:51516", Link: LinkEthernet}, time.Now().Add(-time.Minute)}
	if _, ok := tr.BetterRoute("x", "192.168.1.20:51516"); ok {
		t.Fatal("câble débranché : ne plus le proposer")
	}
}

func TestTrackerWaitsForLateTarget(t *testing.T) {
	probe, _ := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	port := probe.LocalAddr().(*net.UDPAddr).Port
	probe.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	tr, err := Track(ctx, port)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(2 * time.Second)
		Announce(ctx, Beacon{ID: "late", Name: "tardif", Port: 40002}, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	}()
	got, err := tr.Wait(ctx, nil)
	if err != nil || len(got) != 1 || got[0].Name != "tardif" {
		t.Fatalf("%+v %v", got, err)
	}
}
