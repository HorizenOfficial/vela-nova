package cmd

import (
	"testing"

	"github.com/HorizenOfficial/vela-common-go/subtypes"
	"github.com/HorizenOfficial/vela/pkg/crypto"
	ethCrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func TestGenerateSeed(t *testing.T) {
	key, err := crypto.GeneratePrivateKeySecp256k1()
	require.NoError(t, err)

	seed, err := GenerateSeed(key)
	require.NoError(t, err)
	require.Len(t, seed, SeedSize, "seed must be 65 bytes [R||S||V]")
}

func TestGenerateSeed_NilKey(t *testing.T) {
	_, err := GenerateSeed(nil)
	require.Error(t, err)
}

func TestGenerateSeed_RecoverMatchesSender(t *testing.T) {
	key, err := crypto.GeneratePrivateKeySecp256k1()
	require.NoError(t, err)

	seed, err := GenerateSeed(key)
	require.NoError(t, err)

	// Recover signer from seed and verify it matches the key's address
	msgHash := ethCrypto.Keccak256([]byte(subtypes.SubtypeKeyMessage))
	recoveredPub, err := ethCrypto.SigToPub(msgHash, seed)
	require.NoError(t, err)

	recoveredAddr := ethCrypto.PubkeyToAddress(*recoveredPub)
	expectedAddr := ethCrypto.PubkeyToAddress(key.PrivateKey.PublicKey)
	require.Equal(t, expectedAddr, recoveredAddr, "recovered address must match sender")
}

func TestGenerateSeed_Deterministic(t *testing.T) {
	key, err := crypto.GeneratePrivateKeySecp256k1()
	require.NoError(t, err)

	seed1, err := GenerateSeed(key)
	require.NoError(t, err)
	seed2, err := GenerateSeed(key)
	require.NoError(t, err)

	require.Equal(t, seed1, seed2, "same key must produce same seed")
}

func TestEncryptSeed_Roundtrip(t *testing.T) {
	// Simulate user and enclave key pairs
	userKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)
	enclaveKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)

	secpKey, err := crypto.GeneratePrivateKeySecp256k1()
	require.NoError(t, err)

	seed, err := GenerateSeed(secpKey)
	require.NoError(t, err)

	// Encrypt: user encrypts seed for the enclave
	encrypted, err := EncryptSeed(seed, userKey, enclaveKey.PublicKey())
	require.NoError(t, err)
	require.Len(t, encrypted, EncryptedSeedSize, "encrypted seed must be 93 bytes")

	// Decrypt: enclave decrypts using its private key + user's public key
	decrypted, err := crypto.Decrypt(userKey.PublicKey(), enclaveKey, encrypted)
	require.NoError(t, err)
	require.Equal(t, seed, decrypted, "decrypted seed must match original")
}

func TestEncryptSeed_InvalidSeedSize(t *testing.T) {
	userKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)
	enclaveKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)

	_, err = EncryptSeed([]byte("too-short"), userKey, enclaveKey.PublicKey())
	require.Error(t, err)
	require.Contains(t, err.Error(), "seed must be 65 bytes")
}

func TestEncryptSeed_NilKeys(t *testing.T) {
	userKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)
	enclaveKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)

	seed := make([]byte, SeedSize)

	_, err = EncryptSeed(seed, nil, enclaveKey.PublicKey())
	require.Error(t, err)

	_, err = EncryptSeed(seed, userKey, nil)
	require.Error(t, err)
}

func TestBuildAssociateKeyPayloadWithSeed(t *testing.T) {
	userKeyP521, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)
	secpKey, err := crypto.GeneratePrivateKeySecp256k1()
	require.NoError(t, err)
	enclaveKey, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)

	payload, err := BuildAssociateKeyPayloadWithSeed(userKeyP521, secpKey, enclaveKey.PublicKey())
	require.NoError(t, err)
	require.Len(t, payload, AssociateKeyPayloadWithSeed, "payload must be 226 bytes")

	// First 133 bytes must be the P521 public key
	pubKeyBytes := userKeyP521.PublicKey().Bytes()
	require.Equal(t, pubKeyBytes, payload[:133], "first 133 bytes must be P521 public key")

	// Remaining 93 bytes must be the encrypted seed
	encryptedSeed := payload[133:]
	require.Len(t, encryptedSeed, EncryptedSeedSize)

	// Decrypt and verify the seed
	decryptedSeed, err := crypto.Decrypt(userKeyP521.PublicKey(), enclaveKey, encryptedSeed)
	require.NoError(t, err)
	require.Len(t, decryptedSeed, SeedSize)

	// Verify the decrypted seed is a valid signature from the secp256k1 key
	msgHash := ethCrypto.Keccak256([]byte(subtypes.SubtypeKeyMessage))
	recoveredPub, err := ethCrypto.SigToPub(msgHash, decryptedSeed)
	require.NoError(t, err)
	recoveredAddr := ethCrypto.PubkeyToAddress(*recoveredPub)
	expectedAddr := ethCrypto.PubkeyToAddress(secpKey.PrivateKey.PublicKey)
	require.Equal(t, expectedAddr, recoveredAddr)
}

func TestBuildAssociateKeyPayloadWithSeed_BackwardCompatible(t *testing.T) {
	// Without seed: payload is just the P521 public key (133 bytes)
	userKeyP521, err := crypto.GeneratePrivateKeyP521()
	require.NoError(t, err)

	payload := userKeyP521.PublicKey().Bytes()
	require.Len(t, payload, 133, "legacy payload must be 133 bytes")
}

func TestEventSubTypesFromSeed(t *testing.T) {
	secpKey, err := crypto.GeneratePrivateKeySecp256k1()
	require.NoError(t, err)
	seed, err := GenerateSeed(secpKey)
	require.NoError(t, err)

	subs := EventSubTypesFromSeed(seed, subtypes.DefaultSubtypeN)
	require.Len(t, subs, subtypes.DefaultSubtypeN)

	// Each subtype must be a non-zero [32]byte (SHA-256 output)
	for _, st := range subs {
		require.NotEqual(t, [32]byte{}, st, "subtype must not be all zeros")
	}

	// Deterministic: same seed produces same subtypes
	subs2 := EventSubTypesFromSeed(seed, subtypes.DefaultSubtypeN)
	require.Equal(t, subs, subs2)

	// All subtypes must be unique
	seen := make(map[[32]byte]bool, subtypes.DefaultSubtypeN)
	for _, st := range subs {
		require.False(t, seen[st], "duplicate subtype found")
		seen[st] = true
	}
}

func TestEventSubTypesFromSeed_DifferentSeeds(t *testing.T) {
	key1, err := crypto.GeneratePrivateKeySecp256k1()
	require.NoError(t, err)
	key2, err := crypto.GeneratePrivateKeySecp256k1()
	require.NoError(t, err)

	seed1, err := GenerateSeed(key1)
	require.NoError(t, err)
	seed2, err := GenerateSeed(key2)
	require.NoError(t, err)

	subs1 := EventSubTypesFromSeed(seed1, subtypes.DefaultSubtypeN)
	subs2 := EventSubTypesFromSeed(seed2, subtypes.DefaultSubtypeN)
	require.NotEqual(t, subs1, subs2, "different seeds must produce different subtypes")
}
