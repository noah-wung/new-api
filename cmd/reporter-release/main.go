// reporter-release runs in the trusted release environment, never on a client.
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/reporterupdate"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	return runRelease(os.Args[1:])
}

func runRelease(args []string) error {
	flags := flag.NewFlagSet("reporter-release", flag.ContinueOnError)
	mode := flags.String("mode", "build", "generate-key, check-key, build or sign (refresh catalog without rebuilding)")
	keyPath := flags.String("key-file", "", "Private key file outside the source tree")
	encryptedKeyPath := flags.String("encrypted-key-file", "", "age-encrypted signing key (may be tracked in Git)")
	publicKeysPath := flags.String("public-keys-file", "", "Public keys JSON to write on generation or match before signing")
	keychainService := flags.String("keychain-service", "", "macOS Keychain service for the signing passphrase; account is key-id")
	keyID := flags.String("key-id", "release-1", "Signing key identifier")
	out := flags.String("out", "", "Release asset directory")
	version := flags.String("version", "", "Reporter SemVer")
	id := flags.String("release", "", "Immutable release identifier")
	sequence := flags.Int64("sequence", 0, "Strictly increasing catalog sequence")
	rollout := flags.Int("rollout", 100, "Rollout percentage")
	seed := flags.String("seed", "stable", "Stable rollout seed")
	days := flags.Int("valid-days", 30, "Catalog lifetime in days")
	nextKeys := flags.String("next-keys", "", "Optional JSON public keys authorized by this signing key")
	if e := flags.Parse(args); e != nil {
		return e
	}
	if (*keyPath == "") == (*encryptedKeyPath == "") || !reporterupdate.ValidID(*keyID) {
		return errors.New("provide exactly one of key-file or encrypted-key-file, and a valid key-id")
	}
	keyOptions := signingKeyOptions{PlainPath: *keyPath, EncryptedPath: *encryptedKeyPath, PublicKeysPath: *publicKeysPath, KeyID: *keyID, KeychainService: *keychainService}
	if *mode == "generate-key" {
		return keyOptions.generate()
	}
	if *mode != "check-key" && *mode != "build" && *mode != "sign" {
		return errors.New("unsupported mode")
	}
	if *mode != "check-key" && (!reporterupdate.ValidVersion(*version) || !reporterupdate.ValidID(*id) || *sequence < 1 || *out == "" || *days < 1 || *days > 90) {
		return errors.New("version, release, sequence, out and a 1..90 day lifetime are required")
	}
	key, e := keyOptions.load()
	if e != nil {
		return e
	}
	defer clear(key)
	if *mode == "check-key" {
		fmt.Println("signing key verified:", *keyID)
		return nil
	}
	commitBytes, e := exec.Command("git", "rev-parse", "HEAD").Output()
	if e != nil {
		return e
	}
	commit := strings.TrimSpace(string(commitBytes))
	if e := exec.Command("git", "diff", "--quiet", "HEAD", "--", "*.go", "go.mod", "go.sum").Run(); e != nil {
		commit += "-dirty"
	}
	root := filepath.Join(*out, *id)
	if *mode == "build" {
		if _, e = os.Stat(root); !os.IsNotExist(e) {
			return errors.New("release directory already exists; use sign to refresh its catalog")
		}
		if e = os.MkdirAll(root, 0o755); e != nil {
			return e
		}
	}
	c := reporterupdate.Catalog{SchemaVersion: 1, Sequence: *sequence, IssuedAt: time.Now().Unix(), ExpiresAt: time.Now().Add(time.Duration(*days) * 24 * time.Hour).Unix(), ReleaseID: *id, Version: *version, Commit: commit, Protocol: reporterupdate.Range{Min: 1, Max: 1}, Config: reporterupdate.Range{Min: 1, Max: 1}, State: reporterupdate.Range{Min: 2, Max: 2}, Queue: reporterupdate.Range{Min: 1, Max: 1}, RolloutPercent: *rollout, RolloutSeed: *seed}
	if *mode == "sign" {
		var old reporterupdate.Envelope
		var previous reporterupdate.Catalog
		if e = reporterupdate.Load(filepath.Join(root, "catalog.json"), &old); e != nil {
			return e
		}
		payload, e := base64.StdEncoding.DecodeString(old.Payload)
		if e != nil {
			return e
		}
		if e = common.Unmarshal(payload, &previous); e != nil {
			return e
		}
		if previous.Version != c.Version || previous.ReleaseID != c.ReleaseID || previous.Sequence >= c.Sequence {
			return errors.New("refresh must preserve the release and increase its sequence")
		}
		c.Commit = previous.Commit
		c.Protocol = previous.Protocol
		c.Config = previous.Config
		c.State = previous.State
		c.Queue = previous.Queue
		c.NextKeys = previous.NextKeys
		oldData, e := os.ReadFile(filepath.Join(root, "catalog.json"))
		if e != nil {
			return e
		}
		oldTrust := reporterupdate.TrustKeys{*keyID: base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey))}
		if _, e = reporterupdate.Verify(oldData, oldTrust, time.Unix(previous.IssuedAt+1, 0)); e != nil {
			return e
		}
		c.Targets = previous.Targets
	} else {
		for _, target := range []string{"darwin-arm64", "darwin-amd64", "windows-amd64"} {
			name, _ := reporterupdate.TargetName(target)
			path := filepath.Join(root, target, name)
			if e = os.MkdirAll(filepath.Dir(path), 0o755); e != nil {
				return e
			}
			ld := "-s -w -X github.com/QuantumNous/new-api/pkg/reporterupdate.Version=" + *version + " -X github.com/QuantumNous/new-api/pkg/reporterupdate.Commit=" + commit + " -X github.com/QuantumNous/new-api/pkg/reporterupdate.BuildReleaseID=" + *id
			parts := strings.Split(target, "-")
			if parts[0] == "windows" {
				ld += " -H=windowsgui"
			}
			cmd := exec.Command("go", "build", "-trimpath", "-ldflags", ld, "-o", path, "./cmd/usage-reporter")
			cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+parts[0], "GOARCH="+parts[1])
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if e = cmd.Run(); e != nil {
				return e
			}
			data, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			c.Targets = append(c.Targets, reporterupdate.Artifact{Target: target, Path: "/api/external-usage/reporter/releases/" + *id + "/" + target + "/" + name, SHA256: reporterupdate.Digest(data), Size: int64(len(data))})
		}
	}
	if *nextKeys != "" {
		if e = reporterupdate.Load(*nextKeys, &c.NextKeys); e != nil {
			return e
		}
	}
	data, e := reporterupdate.Sign(c, *keyID, key)
	if e != nil {
		return e
	}
	keys := reporterupdate.TrustKeys{*keyID: base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey))}
	if _, e = reporterupdate.Verify(data, keys, time.Now()); e != nil {
		return e
	}
	if e = reporterupdate.WritePrivate(filepath.Join(root, "catalog.json"), data); e != nil {
		return e
	}
	fmt.Println("signed", *id, "sequence", strconv.FormatInt(*sequence, 10))
	return nil
}
