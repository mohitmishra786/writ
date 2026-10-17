// Package token mints and verifies Mandate capabilities: PASETO v4.public
// tokens (Ed25519) that authorize exactly one money movement. PASETO fixes
// the algorithm per version, so there is no "alg" header to confuse.
//
// Verification here checks signature, issuer, audience and time. Single use
// is enforced by the ledger (UPDATE ... WHERE consumed_at IS NULL), because
// only the database can make consumption atomic across processes.
package token

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"aidanwoods.dev/go-paseto"

	"github.com/mohitmishra786/paypal-ai-hackathon/internal/money"
)

const (
	Issuer   = "mandate-kernel"
	Audience = "mandate-executor"
	MaxTTL   = 10 * time.Minute
)

// implicit is a PASETO implicit assertion: it is authenticated but not
// transmitted, so a token signed for another purpose with the same key
// cannot be replayed here.
var implicit = []byte("mandate-capability-v1")

type Op string

const (
	OpVaultCharge Op = "vault_charge"
	OpRefund      Op = "refund"
	OpPayout      Op = "payout"
)

// Capability is the authority a token carries.
type Capability struct {
	JTI        string
	Subject    string // agent id
	MandateID  string
	IntentID   string
	Op         Op
	PayeeRef   string
	Ceiling    money.Money
	PolicyHash string
	QuoteHash  string
	IssuedAt   time.Time
	Expires    time.Time
	KeyID      string
}

type Minter struct {
	key   paseto.V4AsymmetricSecretKey
	keyID string
	now   func() time.Time
}

// NewMinter derives the signing key from a 32-byte hex seed
// (MANDATE_SIGNING_SEED). keyID is published with the public key.
func NewMinter(seedHex, keyID string) (*Minter, error) {
	seed, err := hex.DecodeString(seedHex)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("token: signing seed must be 32 bytes of hex")
	}
	k, err := paseto.NewV4AsymmetricSecretKeyFromEd25519(ed25519.NewKeyFromSeed(seed))
	if err != nil {
		return nil, err
	}
	if keyID == "" {
		return nil, errors.New("token: key id required")
	}
	return &Minter{key: k, keyID: keyID, now: time.Now}, nil
}

func (m *Minter) PublicKey() ed25519.PublicKey {
	return ed25519.PublicKey(m.key.Public().ExportBytes())
}
func (m *Minter) KeyID() string { return m.keyID }

var ErrInvalidCapability = errors.New("token: invalid capability")

func (m *Minter) Mint(c Capability, ttl time.Duration) (string, Capability, error) {
	if ttl <= 0 || ttl > MaxTTL {
		return "", Capability{}, fmt.Errorf("%w: ttl must be in (0, %s]", ErrInvalidCapability, MaxTTL)
	}
	if c.JTI == "" || c.MandateID == "" || c.IntentID == "" || c.Op == "" || c.PayeeRef == "" ||
		c.Ceiling.Minor <= 0 || !c.Ceiling.Currency.Valid() || c.PolicyHash == "" {
		return "", Capability{}, fmt.Errorf("%w: missing required field", ErrInvalidCapability)
	}
	now := m.now().UTC().Truncate(time.Second)
	c.IssuedAt, c.Expires, c.KeyID = now, now.Add(ttl), m.keyID

	t := paseto.NewToken()
	t.SetIssuer(Issuer)
	t.SetAudience(Audience)
	t.SetSubject(c.Subject)
	t.SetJti(c.JTI)
	t.SetIssuedAt(c.IssuedAt)
	t.SetNotBefore(c.IssuedAt)
	t.SetExpiration(c.Expires)
	t.SetString("mid", c.MandateID)
	t.SetString("iid", c.IntentID)
	t.SetString("op", string(c.Op))
	t.SetString("payee", c.PayeeRef)
	t.SetString("ph", c.PolicyHash)
	t.SetString("qh", c.QuoteHash)
	if err := t.Set("ceil", c.Ceiling); err != nil {
		return "", Capability{}, err
	}
	footer, _ := json.Marshal(map[string]string{"kid": m.keyID})
	t.SetFooter(footer)
	return t.V4Sign(m.key, implicit), c, nil
}

// Verifier checks tokens against published public keys by key id.
type Verifier struct {
	keys map[string]paseto.V4AsymmetricPublicKey
	now  func() time.Time
}

func NewVerifier(keys map[string]ed25519.PublicKey) (*Verifier, error) {
	v := &Verifier{keys: map[string]paseto.V4AsymmetricPublicKey{}, now: time.Now}
	for kid, pk := range keys {
		k, err := paseto.NewV4AsymmetricPublicKeyFromEd25519(pk)
		if err != nil {
			return nil, err
		}
		v.keys[kid] = k
	}
	return v, nil
}

func (v *Verifier) Verify(raw string) (Capability, error) {
	p := paseto.NewParserWithoutExpiryCheck()
	footer, err := p.UnsafeParseFooter(paseto.V4Public, raw)
	if err != nil {
		return Capability{}, fmt.Errorf("%w: %v", ErrInvalidCapability, err)
	}
	var f struct {
		KID string `json:"kid"`
	}
	if err := json.Unmarshal(footer, &f); err != nil {
		return Capability{}, fmt.Errorf("%w: bad footer", ErrInvalidCapability)
	}
	key, ok := v.keys[f.KID]
	if !ok {
		return Capability{}, fmt.Errorf("%w: unknown key id %q", ErrInvalidCapability, f.KID)
	}
	p.AddRule(paseto.IssuedBy(Issuer), paseto.ForAudience(Audience), paseto.ValidAt(v.now()))
	t, err := p.ParseV4Public(key, raw, implicit)
	if err != nil {
		return Capability{}, fmt.Errorf("%w: %v", ErrInvalidCapability, err)
	}
	var c Capability
	c.KeyID = f.KID
	c.JTI, _ = t.GetJti()
	c.Subject, _ = t.GetSubject()
	c.IssuedAt, _ = t.GetIssuedAt()
	c.Expires, _ = t.GetExpiration()
	c.MandateID, _ = t.GetString("mid")
	c.IntentID, _ = t.GetString("iid")
	op, _ := t.GetString("op")
	c.Op = Op(op)
	c.PayeeRef, _ = t.GetString("payee")
	c.PolicyHash, _ = t.GetString("ph")
	c.QuoteHash, _ = t.GetString("qh")
	if err := t.Get("ceil", &c.Ceiling); err != nil {
		return Capability{}, fmt.Errorf("%w: ceiling", ErrInvalidCapability)
	}
	return c, nil
}

// Narrow derives a sub-capability for delegation (Mesh). The kernel re-mints
// it; a child can never exceed its parent's ceiling or lifetime.
func Narrow(parent Capability, child Capability) error {
	if child.MandateID != parent.MandateID || child.Ceiling.Currency != parent.Ceiling.Currency {
		return fmt.Errorf("%w: child must share mandate and currency", ErrInvalidCapability)
	}
	if child.Ceiling.Minor > parent.Ceiling.Minor {
		return fmt.Errorf("%w: child ceiling exceeds parent", ErrInvalidCapability)
	}
	if !child.Expires.IsZero() && child.Expires.After(parent.Expires) {
		return fmt.Errorf("%w: child outlives parent", ErrInvalidCapability)
	}
	return nil
}
