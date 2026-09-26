package system

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/bernard-linux/bernard/internal/sysexec"
)

// fake enregistre les commandes et simule un système minimal.
type fake struct {
	cmds      []sysexec.Cmd
	users     map[string]bool
	groups    map[string]bool
	installed map[string]bool
	failApt   map[string]bool
}

func (f *fake) exec(_ context.Context, c sysexec.Cmd) (string, error) {
	f.cmds = append(f.cmds, c)
	fail := errors.New("échec")
	switch c.Name {
	case "getent":
		if c.Args[0] == "passwd" && f.users[c.Args[1]] || c.Args[0] == "group" && f.groups[c.Args[1]] {
			return c.Args[1], nil
		}
		return "", fail
	case "dpkg-query":
		var b strings.Builder
		for p := range f.installed {
			b.WriteString(p + "\tii \n")
		}
		return b.String(), nil
	case "apt-get":
		if c.Args[0] != "install" {
			return "", nil
		}
		pkgs := c.Args[3:]
		for _, p := range pkgs {
			if f.failApt[p] {
				return "", fail
			}
		}
		for _, p := range pkgs {
			f.installed[p] = true
		}
	}
	return "", nil
}

func newFake() (*fake, *System) {
	f := &fake{users: map[string]bool{"root": true, "1000": true}, groups: map[string]bool{"sudo": true, "audio": true},
		installed: map[string]bool{"vlc": true}, failApt: map[string]bool{}}
	return f, &System{Exec: f.exec}
}

func (f *fake) find(name string) []sysexec.Cmd {
	var out []sysexec.Cmd
	for _, c := range f.cmds {
		if c.Name == name {
			out = append(out, c)
		}
	}
	return out
}

func TestCreateUserFromLinux(t *testing.T) {
	f, s := newFake()
	groups, err := s.CreateUser(context.Background(), UserSpec{
		Login: "arnaud", FullName: "Arnaud, bureau:1", UID: 1000,
		Groups:       []string{"sudo", "audio", "docker", "sshd"},
		PasswordHash: "$6$sel$abcdefABCDEF0123./",
	})
	if err != nil {
		t.Fatal(err)
	}
	ua := strings.Join(f.find("useradd")[0].Args, " ")
	if ua != "--create-home --shell /bin/bash --comment Arnaud bureau1 -- arnaud" {
		t.Errorf("useradd inattendu (UID 1000 déjà pris, virgule et deux-points retirés) : %s", ua)
	}
	if strings.Join(groups, ",") != "sudo,audio" {
		t.Errorf("groupes : seuls ceux autorisés ET existants doivent passer : %v", groups)
	}
	cp := f.find("chpasswd")[0]
	if strings.Join(cp.Args, " ") != "--encrypted" || cp.Stdin != "arnaud:$6$sel$abcdefABCDEF0123./\n" {
		t.Errorf("le hachage doit passer par l'entrée standard : %+v", cp)
	}
	for _, c := range f.cmds {
		if strings.Contains(strings.Join(c.Args, " "), "$6$") {
			t.Error("le hachage ne doit jamais apparaître dans les arguments")
		}
	}
}

func TestCreateUserRejectsDangerousInput(t *testing.T) {
	_, s := newFake()
	for _, u := range []UserSpec{
		{Login: "-oroot", Password: "x"},
		{Login: "Arnaud", Password: "x"},
		{Login: "a;rm -rf /", Password: "x"},
		{Login: "ok", PasswordHash: "pas un hachage"},
		{Login: "ok", Password: "deux\nlignes"},
		{Login: "root", Password: "x"},
		{Login: "ok"},
	} {
		if _, err := s.CreateUser(context.Background(), u); err == nil {
			t.Errorf("aurait dû être refusé : %+v", u)
		}
	}
}

func TestAptInstallIsolatesFailures(t *testing.T) {
	f, s := newFake()
	f.failApt["paquet-casse"] = true
	added, failed := s.AptInstall(context.Background(), []string{"gimp", "vlc", "paquet-casse", "rm -rf"})
	if strings.Join(added, ",") != "gimp" {
		t.Errorf("seul gimp est ajouté par Bernard (vlc était déjà là) : %v", added)
	}
	if failed["paquet-casse"] == nil || !errors.Is(failed["rm -rf"], ErrInvalid) {
		t.Errorf("échecs mal rapportés : %v", failed)
	}
}

func TestFlatpakRejectsBadIDs(t *testing.T) {
	_, s := newFake()
	for _, id := range []string{"firefox", "--user", "org.mozilla", "org.x.y;rm"} {
		if s.FlatpakInstall(context.Background(), id) == nil {
			t.Errorf("identifiant accepté à tort : %q", id)
		}
	}
	if err := s.FlatpakInstall(context.Background(), "org.mozilla.firefox"); err != nil {
		t.Error(err)
	}
}

// Test réel, uniquement en root et sur demande explicite :
//
//	sudo BERNARD_SYSTEM_TESTS=1 go test ./internal/system
func TestRealUserLifecycle(t *testing.T) {
	if os.Geteuid() != 0 || os.Getenv("BERNARD_SYSTEM_TESTS") != "1" {
		t.Skip("test système réel désactivé")
	}
	s := New()
	ctx := context.Background()
	const login = "bernardtest"
	hash := "$6$bernardsel$Wm3TQ0bYhYpwDDz3MuDO0M8sM0VNeeL9HCo1ZbFRtR3m3y2Xb0mBv0TtTnB0GmxvZ/3hLgE0dRPm0ydsK2A8/."
	if s.UserExists(ctx, login) {
		s.DeleteUser(ctx, login)
	}
	if _, err := s.CreateUser(ctx, UserSpec{Login: login, FullName: "Test Bernard", UID: 4242, Groups: []string{"audio"}, PasswordHash: hash}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		s.DeleteUser(ctx, login)
		os.RemoveAll("/home/" + login)
	}()
	uid, _, home, err := Owner(login)
	if err != nil || uid != 4242 || home != "/home/"+login {
		t.Fatalf("compte mal créé : uid=%d home=%s err=%v", uid, home, err)
	}
	shadow, _ := os.ReadFile("/etc/shadow")
	if !strings.Contains(string(shadow), login+":"+hash+":") {
		t.Fatal("le hachage n'a pas été repris tel quel")
	}
}

func TestRealAptInstallAndRemove(t *testing.T) {
	if os.Geteuid() != 0 || os.Getenv("BERNARD_SYSTEM_TESTS") != "1" {
		t.Skip("test système réel désactivé")
	}
	s := New()
	ctx := context.Background()
	// Un dépôt tiers cassé fait échouer « update » sans empêcher
	// l'installation depuis les dépôts officiels : on continue, comme Bernard.
	s.AptUpdate(ctx)
	added, failed := s.AptInstall(ctx, []string{"sl", "paquet-qui-nexiste-pas"})
	if len(added) != 1 || added[0] != "sl" || failed["paquet-qui-nexiste-pas"] == nil {
		t.Fatalf("installation : ajoutés=%v échecs=%v", added, failed)
	}
	if err := s.AptRemove(ctx, added); err != nil {
		t.Fatal(err)
	}
	if s.dpkgInstalled(ctx)["sl"] {
		t.Fatal("sl aurait dû être retiré")
	}
}

// Installation réelle depuis Flathub (lent, réseau requis) :
//
//	sudo BERNARD_FLATPAK_TESTS=1 go test -run RealFlatpak -timeout 30m ./internal/system
func TestRealFlatpak(t *testing.T) {
	if os.Geteuid() != 0 || os.Getenv("BERNARD_FLATPAK_TESTS") != "1" {
		t.Skip("test Flathub réel désactivé")
	}
	s := New()
	ctx := context.Background()
	s.AptUpdate(ctx)
	if _, _, err := s.FlatpakSetup(ctx); err != nil {
		t.Fatal(err)
	}
	const app = "com.github.tchx84.Flatseal" // petite application, runtime GNOME
	if err := s.FlatpakInstall(ctx, app); err != nil {
		t.Fatal(err)
	}
	if err := s.FlatpakUninstall(ctx, app); err != nil {
		t.Fatal(err)
	}
}

func TestRetriesWhenAccountFilesLocked(t *testing.T) {
	old := LockDelay
	LockDelay = 0
	defer func() { LockDelay = old }()
	calls := 0
	s := &System{Exec: func(_ context.Context, c sysexec.Cmd) (string, error) {
		switch c.Name {
		case "useradd":
			calls++
			if calls < 3 {
				return "", errors.New("useradd: cannot lock /etc/passwd; try again later.")
			}
		case "getent":
			return "", errors.New("absent")
		}
		return "", nil
	}}
	if _, err := s.CreateUser(context.Background(), UserSpec{Login: "alice", Password: "x"}); err != nil {
		t.Fatalf("aurait dû réussir au troisième essai : %v", err)
	}
	if calls != 3 {
		t.Errorf("essais : %d, attendu 3", calls)
	}
}
