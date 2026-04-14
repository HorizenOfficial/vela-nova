package cmd

import (
	"fmt"

	"github.com/HorizenOfficial/vela-common-go/subtypes"
	cryptotypes "github.com/HorizenOfficial/vela/pkg/common/crypto"
	"github.com/HorizenOfficial/vela/pkg/crypto"
	ethCrypto "github.com/ethereum/go-ethereum/crypto"
)

const (
	// SubtypeKeyMessage is the message signed to produce the seed.
	// Changing this value rotates all user subtype sets.
	SubtypeKeyMessage = "subtype-key-v1"

	// DefaultSubtypeN is the number of privacy-preserving subtypes generated per seed.
	DefaultSubtypeN = 50

	// SeedSize is the expected size of a seed (secp256k1 signature in [R||S||V] format).
	SeedSize = 65

	// EncryptedSeedSize is the expected size of an encrypted seed envelope
	// (12-byte nonce + 65-byte ciphertext + 16-byte GCM tag).
	EncryptedSeedSize = 93

	// AssociateKeyPayloadWithSeed is the full payload size when a seed is included.
	AssociateKeyPayloadWithSeed = 226
)

// GenerateSeed creates a 65-byte secp256k1 signature [R||S||V] by signing
// keccak256(SubtypeKeyMessage) with the user's secp256k1 private key.
func GenerateSeed(key *cryptotypes.PrivateKeySecp256k1) ([]byte, error) {
	if key == nil {
		return nil, fmt.Errorf("secp256k1 private key is required")
	}
	msgHash := ethCrypto.Keccak256([]byte(SubtypeKeyMessage))
	seed, err := ethCrypto.Sign(msgHash, key.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign subtype key message: %w", err)
	}
	return seed, nil
}

// EncryptSeed encrypts the 65-byte seed using ECDH(user_P521_priv, enclave_P521_pub) + AES-256-GCM.
// Returns 93 bytes: 12-byte nonce + 65-byte ciphertext + 16-byte GCM tag.
func EncryptSeed(seed []byte, userPrivKey *cryptotypes.PrivateKeyP521, enclavePubKey *cryptotypes.PublicKeyP521) ([]byte, error) {
	if len(seed) != SeedSize {
		return nil, fmt.Errorf("seed must be %d bytes, got %d", SeedSize, len(seed))
	}
	if userPrivKey == nil {
		return nil, fmt.Errorf("user P521 private key is required")
	}
	if enclavePubKey == nil {
		return nil, fmt.Errorf("enclave P521 public key is required")
	}
	encrypted, err := crypto.Encrypt(userPrivKey, enclavePubKey, seed)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt seed: %w", err)
	}
	if len(encrypted) != EncryptedSeedSize {
		return nil, fmt.Errorf("unexpected encrypted seed size: expected %d, got %d", EncryptedSeedSize, len(encrypted))
	}
	return encrypted, nil
}

// BuildAssociateKeyPayloadWithSeed builds the 226-byte ASSOCIATEKEY payload
// containing the P521 public key (133 bytes) followed by the encrypted seed (93 bytes).
func BuildAssociateKeyPayloadWithSeed(
	keyP521 *cryptotypes.PrivateKeyP521,
	keySecp *cryptotypes.PrivateKeySecp256k1,
	enclavePubKey *cryptotypes.PublicKeyP521,
) ([]byte, error) {
	seed, err := GenerateSeed(keySecp)
	if err != nil {
		return nil, err
	}

	encryptedSeed, err := EncryptSeed(seed, keyP521, enclavePubKey)
	if err != nil {
		return nil, err
	}

	pubKeyBytes := keyP521.PublicKey().Bytes()
	payload := make([]byte, 0, AssociateKeyPayloadWithSeed)
	payload = append(payload, pubKeyBytes...)
	payload = append(payload, encryptedSeed...)
	return payload, nil
}

// EventSubTypesFromSeed generates the deterministic set of N privacy-preserving
// event subtypes from a seed. Each subtype is "0x" + hex(HMAC-SHA256(seed, byte(i)))
// for i in [1, n].
func EventSubTypesFromSeed(seed []byte, n int) []string {
	return subtypes.GenerateSubtypesN(seed, n)
}
