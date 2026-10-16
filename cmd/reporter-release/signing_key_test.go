package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"filippo.io/age/armor"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/reporterupdate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptedSigningKeyLifecycleAndAuthenticatedDecryption(t *testing.T) {
	const passphrase = "fixture-only-passphrase-32-bytes-long"
	t.Setenv(signingPassphraseEnv, passphrase)
	dir := t.TempDir()
	options := signingKeyOptions{EncryptedPath: filepath.Join(dir, "release-1.key.age"), PublicKeysPath: filepath.Join(dir, "public-keys.json"), KeyID: "release-1"}
	require.NoError(t, runRelease([]string{"--mode", "generate-key", "--encrypted-key-file", options.EncryptedPath, "--public-keys-file", options.PublicKeysPath}))
	content, err := os.ReadFile(options.EncryptedPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), armor.Header)
	assert.NotContains(t, string(content), passphrase)
	// Git checkouts normally restore encrypted files with mode 0644.
	require.NoError(t, os.Chmod(options.EncryptedPath, 0o644))
	key, err := options.load()
	require.NoError(t, err)
	defer clear(key)
	assert.NotContains(t, string(content), base64.StdEncoding.EncodeToString(key))
	var keys reporterupdate.TrustKeys
	require.NoError(t, reporterupdate.Load(options.PublicKeysPath, &keys))
	assert.Equal(t, keys["release-1"], base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey)))
	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, files, 2, "generation must leave only ciphertext and public metadata")

	// Verify the real release-signing contract with the recovered private key.
	now := time.Now()
	catalog := reporterupdate.Catalog{SchemaVersion: 1, Sequence: 1, IssuedAt: now.Add(-time.Minute).Unix(), ExpiresAt: now.Add(time.Hour).Unix(), ReleaseID: "fixture-release", Version: "1.0.3", Commit: "fixture", Protocol: reporterupdate.Range{Min: 1, Max: 1}, Config: reporterupdate.Range{Min: 1, Max: 1}, State: reporterupdate.Range{Min: 2, Max: 2}, Queue: reporterupdate.Range{Min: 1, Max: 1}, RolloutPercent: 10, RolloutSeed: "stable"}
	for _, target := range []string{"darwin-arm64", "darwin-amd64", "windows-amd64"} {
		name, err := reporterupdate.TargetName(target)
		require.NoError(t, err)
		catalog.Targets = append(catalog.Targets, reporterupdate.Artifact{Target: target, Path: "/api/external-usage/reporter/releases/fixture-release/" + target + "/" + name, SHA256: reporterupdate.Digest([]byte(target)), Size: int64(len(target))})
	}
	signed, err := reporterupdate.Sign(catalog, options.KeyID, key)
	require.NoError(t, err)
	verified, err := reporterupdate.Verify(signed, keys, now)
	require.NoError(t, err)
	assert.Equal(t, catalog, verified)

	root := filepath.Join(dir, catalog.ReleaseID)
	require.NoError(t, os.Mkdir(root, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "catalog.json"), signed, 0o600))
	require.NoError(t, runRelease([]string{"--mode", "sign", "--encrypted-key-file", options.EncryptedPath, "--public-keys-file", options.PublicKeysPath, "--out", dir, "--version", catalog.Version, "--release", catalog.ReleaseID, "--sequence", "2"}))
	refreshed, err := os.ReadFile(filepath.Join(root, "catalog.json"))
	require.NoError(t, err)
	renewed, err := reporterupdate.Verify(refreshed, keys, time.Now())
	require.NoError(t, err)
	assert.EqualValues(t, 2, renewed.Sequence)
	assert.Equal(t, catalog.Targets, renewed.Targets)

	t.Run("existing outputs are never overwritten", func(t *testing.T) {
		require.ErrorContains(t, options.generate(), "already exists")
		after, err := os.ReadFile(options.EncryptedPath)
		require.NoError(t, err)
		assert.Equal(t, content, after)
	})
	t.Run("wrong passphrase", func(t *testing.T) {
		t.Setenv(signingPassphraseEnv, "wrong-fixture-passphrase")
		_, err := options.load()
		require.Error(t, err)
		assert.NotContains(t, err.Error(), os.Getenv(signingPassphraseEnv))
	})
	t.Run("empty environment never falls back to another secret source", func(t *testing.T) {
		t.Setenv(signingPassphraseEnv, "")
		options.KeychainService = "fixture-keychain-must-not-run"
		_, err := options.load()
		require.ErrorContains(t, err, "at least 16 bytes")
		options.KeychainService = ""
	})
	t.Run("public key must match", func(t *testing.T) {
		other := options
		other.PublicKeysPath = filepath.Join(dir, "wrong-public-keys.json")
		data, err := common.Marshal(reporterupdate.TrustKeys{"release-1": base64.StdEncoding.EncodeToString(make([]byte, ed25519.PublicKeySize))})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(other.PublicKeysPath, data, 0o600))
		_, err = other.load()
		require.ErrorContains(t, err, "does not match")
	})
	t.Run("tampered payload is rejected after header decryption", func(t *testing.T) {
		raw, err := io.ReadAll(armor.NewReader(bytes.NewReader(content)))
		require.NoError(t, err)
		raw[len(raw)-1] ^= 1
		var tampered bytes.Buffer
		writer := armor.NewWriter(&tampered)
		_, err = writer.Write(raw)
		require.NoError(t, err)
		require.NoError(t, writer.Close())
		other := options
		other.EncryptedPath = filepath.Join(dir, "tampered.key.age")
		require.NoError(t, os.WriteFile(other.EncryptedPath, tampered.Bytes(), 0o600))
		_, err = other.load()
		require.ErrorContains(t, err, "unauthenticated")
	})
}

func TestPlainSigningKeyCompatibilityAndRepositoryBoundary(t *testing.T) {
	outside := t.TempDir()
	options := signingKeyOptions{PlainPath: filepath.Join(outside, "fixture.key"), KeyID: "fixture"}
	require.NoError(t, options.generate())
	key, err := options.load()
	require.NoError(t, err)
	assert.Len(t, key, ed25519.PrivateKeySize)
	clear(key)
	require.NoError(t, os.Chmod(options.PlainPath, 0o644))
	_, err = options.load()
	require.ErrorContains(t, err, "permissions")

	// Invoke from a nested directory: the boundary is the checkout root.
	repo := t.TempDir()
	require.NoError(t, exec.Command("git", "init", "-q", repo).Run())
	subdir := filepath.Join(repo, "nested")
	require.NoError(t, os.Mkdir(subdir, 0o700))
	t.Chdir(subdir)
	inside := filepath.Join(repo, "must-not-create.key")
	require.ErrorContains(t, outsideSourceTree(inside), "outside the source tree")
	_, err = os.Stat(inside)
	assert.True(t, os.IsNotExist(err))
	require.NoError(t, os.WriteFile(inside, []byte("fixture-only"), 0o600))
	alias := filepath.Join(outside, "alias.key")
	require.NoError(t, os.Symlink(inside, alias))
	require.ErrorContains(t, outsideSourceTree(alias), "outside the source tree")
}

func TestReleaseCommandRequiresExplicitKeyStorage(t *testing.T) {
	for _, args := range [][]string{
		{"--mode", "check-key"},
		{"--mode", "check-key", "--key-file", "plain.key", "--encrypted-key-file", "encrypted.key.age"},
		{"--mode", "generate-key", "--encrypted-key-file", "encrypted.key.age"},
	} {
		require.Error(t, runRelease(args))
	}
}
