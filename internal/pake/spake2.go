// Package pake implémente SPAKE2 (inspiré de la RFC 9382) sur la courbe P-256.
//
// Il permet à deux machines qui partagent un code court (6 chiffres) d'établir
// une clé commune sans jamais l'envoyer. Un attaquant qui observe ou
// intercepte l'échange ne peut pas tester les codes hors ligne : chaque essai
// exige une nouvelle session, et la cible révoque le code après 3 échecs.
//
// Liaison de canal : le paramètre binding (exporté de la session TLS) entre
// dans la transcription. Un intermédiaire qui ouvrirait deux sessions TLS
// distinctes ferait échouer la confirmation.
package pake

import (
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"math/big"
)

// Rôles des deux participants.
const (
	RoleTarget = "bernard-target" // affiche le code (rôle A)
	RoleSource = "bernard-source" // saisit le code (rôle B)
)

// ErrBadCode signale un code erroné (ou une interception active).
var ErrBadCode = errors.New("code d'appairage incorrect")

var (
	curve  = elliptic.P256()
	params = curve.Params()
	mX, mY = hashToPoint("bernard-spake2-M-v1")
	nX, nY = hashToPoint("bernard-spake2-N-v1")
)

// hashToPoint dérive un point de la courbe dont personne ne connaît le
// logarithme discret (méthode « essai et incrément »), comme exigé pour M et N.
func hashToPoint(seed string) (*big.Int, *big.Int) {
	for ctr := uint32(0); ; ctr++ {
		h := sha256.New()
		h.Write([]byte(seed))
		binary.Write(h, binary.BigEndian, ctr)
		compressed := append([]byte{0x02}, h.Sum(nil)...)
		if x, y := elliptic.UnmarshalCompressed(curve, compressed); x != nil {
			return x, y
		}
	}
}

// State est l'état d'un participant pendant l'échange.
type State struct {
	role    string
	w       *big.Int
	x       *big.Int
	msg     []byte
	binding []byte
}

// Start commence l'échange et renvoie le message à envoyer à l'autre partie.
func Start(role, code string, binding []byte) (*State, []byte, error) {
	if role != RoleTarget && role != RoleSource {
		return nil, nil, errors.New("rôle inconnu")
	}
	w := passwordScalar(code, binding)
	x, err := randomScalar()
	if err != nil {
		return nil, nil, err
	}
	// Rôle A : X = x·G + w·M ; rôle B : Y = y·G + w·N.
	bx, by := curve.ScalarBaseMult(x.Bytes())
	px, py := mX, mY
	if role == RoleSource {
		px, py = nX, nY
	}
	wx, wy := curve.ScalarMult(px, py, w.Bytes())
	sx, sy := curve.Add(bx, by, wx, wy)
	msg := elliptic.Marshal(curve, sx, sy)
	return &State{role: role, w: w, x: x, msg: msg, binding: binding}, msg, nil
}

// Keys est le résultat d'un échange : la clé de session et les deux
// confirmations (celle à envoyer, celle à attendre).
type Keys struct {
	Session  []byte
	MyConf   []byte
	PeerConf []byte
}

// Finish traite le message de l'autre partie et dérive les clés.
func (s *State) Finish(peer []byte) (*Keys, error) {
	px, py := elliptic.Unmarshal(curve, peer)
	if px == nil {
		return nil, errors.New("point invalide reçu")
	}
	// Retirer w·(M ou N) du point reçu, puis multiplier par notre secret.
	ox, oy := nX, nY
	if s.role == RoleSource {
		ox, oy = mX, mY
	}
	wx, wy := curve.ScalarMult(ox, oy, s.w.Bytes())
	negY := new(big.Int).Sub(params.P, wy)
	tx, ty := curve.Add(px, py, wx, negY)
	if tx.Sign() == 0 && ty.Sign() == 0 {
		return nil, errors.New("point invalide reçu")
	}
	kx, ky := curve.ScalarMult(tx, ty, s.x.Bytes())
	k := elliptic.Marshal(curve, kx, ky)

	msgA, msgB := s.msg, peer
	if s.role == RoleSource {
		msgA, msgB = peer, s.msg
	}
	tt := transcript(msgA, msgB, k, s.w.Bytes(), s.binding)
	sum := sha256.Sum256(tt)
	ke, ka := sum[:16], sum[16:]
	kcA := hkdf(ka, "bernard confirmation A")
	kcB := hkdf(ka, "bernard confirmation B")
	confA, confB := mac(kcA, tt), mac(kcB, tt)
	keys := &Keys{Session: hkdf(ke, "bernard session")}
	if s.role == RoleTarget {
		keys.MyConf, keys.PeerConf = confA, confB
	} else {
		keys.MyConf, keys.PeerConf = confB, confA
	}
	return keys, nil
}

// Verify compare la confirmation reçue en temps constant.
func (k *Keys) Verify(received []byte) error {
	if subtle.ConstantTimeCompare(received, k.PeerConf) != 1 {
		return ErrBadCode
	}
	return nil
}

func transcript(parts ...[]byte) []byte {
	var out []byte
	for _, p := range append([][]byte{[]byte(RoleTarget), []byte(RoleSource)}, parts...) {
		out = binary.LittleEndian.AppendUint64(out, uint64(len(p)))
		out = append(out, p...)
	}
	return out
}

func passwordScalar(code string, binding []byte) *big.Int {
	h := hmac.New(sha256.New, []byte("bernard-spake2-password-v1"))
	h.Write(binding)
	h.Write([]byte(code))
	// 64 octets réduits modulo n : biais négligeable.
	h2 := sha256.Sum256(h.Sum(nil))
	wide := append(h.Sum(nil), h2[:]...)
	return new(big.Int).Mod(new(big.Int).SetBytes(wide), params.N)
}

func randomScalar() (*big.Int, error) {
	for {
		k, err := rand.Int(rand.Reader, params.N)
		if err != nil {
			return nil, err
		}
		if k.Sign() > 0 {
			return k, nil
		}
	}
}

func mac(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

// hkdf est une dérivation HKDF-SHA256 à un bloc (32 octets).
func hkdf(secret []byte, info string) []byte {
	prk := mac(make([]byte, sha256.Size), secret)
	return mac(prk, append([]byte(info), 1))
}

// NewCode tire un code à 6 chiffres uniformément.
func NewCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	s := n.String()
	for len(s) < 6 {
		s = "0" + s
	}
	return s, nil
}
