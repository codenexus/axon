package updater

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// signFixture writes data to a temp file and returns its path plus a hex
// signature over its SHA-256 digest, signed with priv.
func signFixture(t *testing.T, dir string, data []byte, priv ed25519.PrivateKey) (path, sigHex string) {
	t.Helper()
	path = filepath.Join(dir, "fixture-binary")
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	digest := sha256.Sum256(data)
	sig := ed25519.Sign(priv, digest[:])
	return path, hex.EncodeToString(sig)
}

func TestVerifyBinaryWithKeyAcceptsValidSignature(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate test keypair: %v", err)
	}
	path, sigHex := signFixture(t, t.TempDir(), []byte("a throwaway pulse binary"), priv)

	if err := verifyBinaryWithKey(path, sigHex, hex.EncodeToString(pub)); err != nil {
		t.Fatalf("expected valid signature to verify, got: %v", err)
	}
}

func TestVerifyBinaryWithKeyRejectsTamperedFile(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate test keypair: %v", err)
	}
	path, sigHex := signFixture(t, t.TempDir(), []byte("original content"), priv)

	if err := os.WriteFile(path, []byte("tampered content"), 0o755); err != nil {
		t.Fatalf("tamper fixture: %v", err)
	}

	if err := verifyBinaryWithKey(path, sigHex, hex.EncodeToString(pub)); err == nil {
		t.Fatal("expected tampered file to fail verification")
	}
}

func TestVerifyBinaryWithKeyRejectsWrongSignature(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate test keypair: %v", err)
	}
	path, _ := signFixture(t, t.TempDir(), []byte("real content"), priv)
	_, wrongSigHex := signFixture(t, t.TempDir(), []byte("different content"), priv)

	if err := verifyBinaryWithKey(path, wrongSigHex, hex.EncodeToString(pub)); err == nil {
		t.Fatal("expected a signature over different content to fail verification")
	}
}

func TestVerifyBinaryWithKeyRejectsSignatureFromDifferentKeypair(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate test keypair: %v", err)
	}
	_, otherPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate other test keypair: %v", err)
	}
	path, sigHex := signFixture(t, t.TempDir(), []byte("content"), otherPriv)

	if err := verifyBinaryWithKey(path, sigHex, hex.EncodeToString(pub)); err == nil {
		t.Fatal("expected signature from a different keypair to fail verification")
	}
}

func TestVerifyBinaryWithKeyRejectsMalformedInputs(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate test keypair: %v", err)
	}
	path, sigHex := signFixture(t, t.TempDir(), []byte("content"), priv)
	pubHex := hex.EncodeToString(pub)

	if err := verifyBinaryWithKey(path, "not-hex", pubHex); err == nil {
		t.Fatal("expected non-hex signature to be rejected")
	}
	if err := verifyBinaryWithKey(path, sigHex, "not-hex"); err == nil {
		t.Fatal("expected non-hex public key to be rejected")
	}
	if err := verifyBinaryWithKey(filepath.Join(t.TempDir(), "missing"), sigHex, pubHex); err == nil {
		t.Fatal("expected a missing file to be rejected")
	}
}

// TestVerifyBinaryUsesPinnedKey confirms the exported entry point is wired
// to pinnedPublicKey (not some other key) without touching the real
// production private key — a signature from a throwaway keypair must be
// rejected by the real, pinned-key code path.
func TestVerifyBinaryUsesPinnedKey(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate test keypair: %v", err)
	}
	path, sigHex := signFixture(t, t.TempDir(), []byte("content"), priv)

	if err := VerifyBinary(path, sigHex); err == nil {
		t.Fatal("expected VerifyBinary to reject a signature not from the pinned production key")
	}
}
