package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"filippo.io/age"
	"filippo.io/age/armor"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/reporterupdate"
)

const signingPassphraseEnv = "REPORTER_RELEASE_KEY_PASSPHRASE"

type signingKeyOptions struct {
	PlainPath       string
	EncryptedPath   string
	PublicKeysPath  string
	KeyID           string
	KeychainService string
}

func (o signingKeyOptions) passphrase(create bool) (string, error) {
	if value, exists := os.LookupEnv(signingPassphraseEnv); exists {
		if len(value) < 16 {
			return "", errors.New("REPORTER_RELEASE_KEY_PASSPHRASE must contain at least 16 bytes")
		}
		return value, nil
	}
	if o.KeychainService == "" {
		return "", errors.New("set REPORTER_RELEASE_KEY_PASSPHRASE or use --keychain-service on macOS")
	}
	if runtime.GOOS != "darwin" || !reporterupdate.ValidID(o.KeychainService) {
		return "", errors.New("keychain-service requires macOS and a valid service identifier")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-s", o.KeychainService, "-a", o.KeyID, "-w").Output()
	if err == nil {
		value := strings.TrimSuffix(string(output), "\n")
		if len(value) < 16 {
			return "", errors.New("Keychain signing passphrase is too short")
		}
		return value, nil
	}
	var exit *exec.ExitError
	if !create || !errors.As(err, &exit) || exit.ExitCode() != 44 {
		return "", errors.New("could not read signing passphrase from Keychain")
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	value := base64.RawURLEncoding.EncodeToString(random[:])
	// security's interactive mode receives the secret through stdin, rather
	// than exposing it in process arguments. All fields use restricted alphabets.
	command := exec.CommandContext(ctx, "/usr/bin/security", "-i")
	command.Stdin = strings.NewReader("add-generic-password -s " + o.KeychainService + " -a " + o.KeyID + " -w " + value + "\n")
	if err := command.Run(); err != nil {
		return "", errors.New("could not store signing passphrase in Keychain")
	}
	// The interactive shell can exit successfully after a subcommand fails.
	stored, err := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-s", o.KeychainService, "-a", o.KeyID, "-w").Output()
	if err != nil || strings.TrimSuffix(string(stored), "\n") != value {
		return "", errors.New("could not verify the new Keychain signing passphrase")
	}
	return value, nil
}

// Plain signing keys remain supported only outside the actual Git checkout,
// including when the command is invoked from a subdirectory or via a symlink.
func outsideSourceTree(path string) error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	if output, err := exec.Command("git", "rev-parse", "--show-toplevel").Output(); err == nil {
		root = strings.TrimSpace(string(output))
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if os.IsNotExist(err) {
		parent, parentErr := filepath.EvalSymlinks(filepath.Dir(abs))
		if parentErr != nil {
			return parentErr
		}
		resolved, err = filepath.Join(parent, filepath.Base(abs)), nil
	}
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return errors.New("store plaintext signing keys outside the source tree; use --encrypted-key-file for tracked keys")
	}
	return nil
}

func (o signingKeyOptions) generate() error {
	path := o.PlainPath
	if o.EncryptedPath != "" {
		path = o.EncryptedPath
		if o.PublicKeysPath == "" {
			return errors.New("encrypted key generation requires --public-keys-file")
		}
	} else if err := outsideSourceTree(path); err != nil {
		return err
	}
	for _, candidate := range []string{path, o.PublicKeysPath} {
		if candidate == "" {
			continue
		}
		if _, err := os.Lstat(candidate); !os.IsNotExist(err) {
			return errors.New("signing key output already exists or is inaccessible")
		}
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	defer clear(priv)
	var content []byte
	if o.EncryptedPath != "" {
		passphrase, err := o.passphrase(true)
		if err != nil {
			return err
		}
		recipient, err := age.NewScryptRecipient(passphrase)
		if err != nil {
			return err
		}
		var encrypted bytes.Buffer
		armored := armor.NewWriter(&encrypted)
		writer, err := age.Encrypt(armored, recipient)
		if err != nil {
			return err
		}
		if _, err := writer.Write(priv); err != nil {
			return err
		}
		if err := writer.Close(); err != nil {
			return err
		}
		if err := armored.Close(); err != nil {
			return err
		}
		content = encrypted.Bytes()
	} else {
		content = []byte(base64.StdEncoding.EncodeToString(priv))
	}
	defer clear(content)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(content)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		_ = os.Remove(path)
		return err
	}
	if o.PublicKeysPath != "" {
		data, err := common.Marshal(reporterupdate.TrustKeys{o.KeyID: base64.StdEncoding.EncodeToString(pub)})
		if err == nil {
			var public *os.File
			public, err = os.OpenFile(o.PublicKeysPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
			if err == nil {
				_, writeErr := public.Write(append(data, '\n'))
				err = errors.Join(writeErr, public.Close())
				if err != nil {
					_ = os.Remove(o.PublicKeysPath)
				}
			}
		}
		if err != nil {
			_ = os.Remove(path)
			return err
		}
	}
	fmt.Printf("public key %s: %s\n", o.KeyID, base64.StdEncoding.EncodeToString(pub))
	return nil
}

func (o signingKeyOptions) load() (ed25519.PrivateKey, error) {
	path := o.EncryptedPath
	if path == "" {
		path = o.PlainPath
		if err := outsideSourceTree(path); err != nil {
			return nil, err
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if info.Mode().Perm()&0o077 != 0 {
			return nil, errors.New("private key permissions must be 0600")
		}
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, 64<<10+1))
	if err != nil || len(content) > 64<<10 {
		return nil, errors.New("could not read signing key within size limit")
	}
	defer clear(content)
	var raw []byte
	if o.EncryptedPath == "" {
		raw, err = base64.StdEncoding.DecodeString(strings.TrimSpace(string(content)))
	} else {
		passphrase, passErr := o.passphrase(false)
		if passErr != nil {
			return nil, passErr
		}
		identity, identityErr := age.NewScryptIdentity(passphrase)
		if identityErr != nil {
			return nil, identityErr
		}
		identity.SetMaxWorkFactor(18)
		reader, decryptErr := age.Decrypt(armor.NewReader(bytes.NewReader(content)), identity)
		if decryptErr != nil {
			return nil, errors.New("could not decrypt signing key; check passphrase and encrypted file")
		}
		raw, err = io.ReadAll(io.LimitReader(reader, ed25519.PrivateKeySize+1))
	}
	if err != nil || len(raw) != ed25519.PrivateKeySize {
		clear(raw)
		return nil, errors.New("invalid or unauthenticated signing key")
	}
	key := ed25519.PrivateKey(raw)
	canonical := ed25519.NewKeyFromSeed(key.Seed())
	defer clear(canonical)
	if !bytes.Equal(key, canonical) {
		clear(raw)
		return nil, errors.New("invalid signing key")
	}
	if o.PublicKeysPath != "" {
		var keys reporterupdate.TrustKeys
		if reporterupdate.Load(o.PublicKeysPath, &keys) != nil || keys[o.KeyID] != base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey)) {
			clear(raw)
			return nil, errors.New("signing key does not match the configured public key")
		}
	}
	return key, nil
}
