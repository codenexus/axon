// keygen generates an Ed25519 key pair for signing Pulse release binaries.
// Run once, store the private key somewhere secure (password manager / CI
// secret) — never in this repo — and paste the public key into
// pulse/internal/updater/verify.go as pinnedPublicKey.
//
// Usage: go run ./tools/keygen
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
)

func main() {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatalf("keygen: %v", err)
	}

	fmt.Printf("PUBLIC KEY  (embed in pulse/internal/updater/verify.go):\n%s\n\n", hex.EncodeToString(pub))
	fmt.Printf("PRIVATE KEY (store in a password manager / CI secret, e.g. AXON_SIGNING_KEY):\n%s\n\n", hex.EncodeToString(priv))
	fmt.Println("The private key bytes in Go's ed25519 format are: seed (32 bytes) || public key (32 bytes).")
}
