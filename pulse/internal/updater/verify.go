package updater

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
)

// pinnedPublicKey is the Ed25519 public key that all released Pulse
// binaries must be signed against. Generated once with pulse/tools/keygen
// — never rotate without rebuilding all deployed agents.
const pinnedPublicKey = "47a838bae27103feda6a957c597cc9549dfe9071909d6b638907d6201649bab9"

// VerifyBinary checks that the file at path was signed by the private key
// corresponding to pinnedPublicKey. sigHex is the hex-encoded Ed25519
// signature over the SHA-256 digest of the binary content. This is the
// actual security boundary for self-update — Panel's heartbeat response
// only ever proposes an update, it's never trusted on its own say-so.
func VerifyBinary(path, sigHex string) error {
	return verifyBinaryWithKey(path, sigHex, pinnedPublicKey)
}

// verifyBinaryWithKey is the actual verification logic, parameterized on
// the public key so tests can exercise it with a throwaway keypair rather
// than the real pinned production key. VerifyBinary is the only caller
// outside tests, and always passes pinnedPublicKey — this split changes
// nothing about what a real caller can do, it only makes the crypto itself
// independently testable.
func verifyBinaryWithKey(path, sigHex, pubKeyHex string) error {
	pubBytes, err := hex.DecodeString(pubKeyHex)
	if err != nil || len(pubBytes) != ed25519.PublicKeySize {
		return errors.New("updater: invalid public key")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(data)

	sig, err := hex.DecodeString(sigHex)
	if err != nil {
		return errors.New("updater: invalid signature encoding")
	}

	if !ed25519.Verify(ed25519.PublicKey(pubBytes), digest[:], sig) {
		return errors.New("updater: signature verification failed — binary rejected")
	}
	return nil
}
