package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVerifyAttestationReadsCommittedBlob(t *testing.T) {
	repo := initAttestationRepo(t)
	parent := gitOut(t, repo, "rev-parse", "HEAD")
	writeTestAttestation(t, repo, scanAttestation{
		SchemaVersion: attestationSchemaV1,
		CommitSHA:     parent,
		ScanTimestamp: time.Now().UTC(),
		ToolVersion:   "test",
		CatalogIDHash: "sha256:" + strings.Repeat("a", 64),
		Visibility:    "public_oss",
		ExitCode:      0,
		Operator:      "test",
		ScanMetadata:  attestationScanMetadata{FilesScanned: 1},
	})
	git(t, repo, "add", defaultAttestationPath)
	git(t, repo, "commit", "-q", "-m", "attest")

	// Mutate the working-tree file after commit. The verifier must ignore
	// this uncommitted disk state and read HEAD:path instead.
	writeTestAttestation(t, repo, scanAttestation{
		SchemaVersion: attestationSchemaV1,
		CommitSHA:     strings.Repeat("f", 40),
		ScanTimestamp: time.Now().UTC(),
		ToolVersion:   "test",
		CatalogIDHash: "sha256:" + strings.Repeat("b", 64),
		Visibility:    "public_oss",
		ExitCode:      1,
		Operator:      "test",
		ScanMetadata:  attestationScanMetadata{FilesScanned: 1},
	})

	err := verifyAttestation(repo, defaultAttestationPath, attestationVerifyOptions{
		Mode:   "push",
		MaxAge: 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("verify committed attestation: %v", err)
	}
}

func TestVerifyAttestationRejectsUncommittedOnlyFile(t *testing.T) {
	repo := initAttestationRepo(t)
	writeTestAttestation(t, repo, scanAttestation{
		SchemaVersion: attestationSchemaV1,
		CommitSHA:     gitOut(t, repo, "rev-parse", "HEAD"),
		ScanTimestamp: time.Now().UTC(),
		ToolVersion:   "test",
		CatalogIDHash: "sha256:" + strings.Repeat("a", 64),
		Visibility:    "public_oss",
		ExitCode:      0,
		Operator:      "test",
		ScanMetadata:  attestationScanMetadata{FilesScanned: 1},
	})

	err := verifyAttestation(repo, defaultAttestationPath, attestationVerifyOptions{
		Mode:   "push",
		MaxAge: 24 * time.Hour,
	})
	if err == nil {
		t.Fatal("expected uncommitted attestation to be rejected")
	}
}

func TestVerifyAttestationRejectsParentBindingWithCodeChanges(t *testing.T) {
	repo := initAttestationRepo(t)
	parent := gitOut(t, repo, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTestAttestation(t, repo, scanAttestation{
		SchemaVersion: attestationSchemaV1,
		CommitSHA:     parent,
		ScanTimestamp: time.Now().UTC(),
		ToolVersion:   "test",
		CatalogIDHash: "sha256:" + strings.Repeat("a", 64),
		Visibility:    "public_oss",
		ExitCode:      0,
		Operator:      "test",
		ScanMetadata:  attestationScanMetadata{FilesScanned: 1},
	})
	git(t, repo, "add", "README.md", defaultAttestationPath)
	git(t, repo, "commit", "-q", "-m", "code and stale attest")

	err := verifyAttestation(repo, defaultAttestationPath, attestationVerifyOptions{
		Mode:   "push",
		MaxAge: 24 * time.Hour,
	})
	if err == nil {
		t.Fatal("expected parent-bound attestation with code changes to be rejected")
	}
}

func TestVerifyAttestationRejectsFutureTimestamp(t *testing.T) {
	repo := initAttestationRepo(t)
	parent := gitOut(t, repo, "rev-parse", "HEAD")
	hash := "sha256:" + strings.Repeat("a", 64)
	writeTestAttestation(t, repo, scanAttestation{
		SchemaVersion: attestationSchemaV1,
		CommitSHA:     parent,
		ScanTimestamp: time.Now().UTC().Add(10 * time.Minute),
		ToolVersion:   "test",
		CatalogIDHash: hash,
		Visibility:    "public_oss",
		ExitCode:      0,
		Operator:      "test",
		ScanMetadata:  attestationScanMetadata{FilesScanned: 1},
	})
	git(t, repo, "add", defaultAttestationPath)
	git(t, repo, "commit", "-q", "-m", "attest")

	err := verifyAttestation(repo, defaultAttestationPath, attestationVerifyOptions{
		Mode:        "tag",
		MaxAge:      time.Hour,
		KnownHashes: map[string]bool{hash: true},
	})
	if err == nil || !strings.Contains(err.Error(), "future") {
		t.Fatalf("expected future timestamp rejection, got %v", err)
	}
}

func TestVerifyAttestationTagIgnoresUncommittedKnownHashes(t *testing.T) {
	t.Setenv("LIMENSAFE_KNOWN_CATALOG_HASHES", "")
	t.Setenv("LIMENSAFE_RELEASE_CATALOG_OK", "")

	repo := initAttestationRepo(t)
	attestedHash := "sha256:" + strings.Repeat("a", 64)
	committedHash := "sha256:" + strings.Repeat("b", 64)
	writeKnownCatalogHashes(t, repo, committedHash+"\n")
	git(t, repo, "add", defaultKnownHashesPath)
	git(t, repo, "commit", "-q", "-m", "known hashes")

	parent := gitOut(t, repo, "rev-parse", "HEAD")
	writeTestAttestation(t, repo, scanAttestation{
		SchemaVersion: attestationSchemaV1,
		CommitSHA:     parent,
		ScanTimestamp: time.Now().UTC(),
		ToolVersion:   "test",
		CatalogIDHash: attestedHash,
		Visibility:    "public_oss",
		ExitCode:      0,
		Operator:      "test",
		ScanMetadata:  attestationScanMetadata{FilesScanned: 1},
	})
	git(t, repo, "add", defaultAttestationPath)
	git(t, repo, "commit", "-q", "-m", "attest")

	// An uncommitted working-tree allowlist edit must not satisfy tag mode;
	// the release approval list must travel in the committed audit trail.
	writeKnownCatalogHashes(t, repo, attestedHash+"\n")
	opts, err := verifierOptionsFromEnv(repo, "tag")
	if err != nil {
		t.Fatalf("verifier options: %v", err)
	}
	err = verifyAttestation(repo, defaultAttestationPath, opts)
	if err == nil || !strings.Contains(err.Error(), "known-catalog-hashes") {
		t.Fatalf("expected committed known-hash rejection, got %v", err)
	}
}

func TestVerifyAttestationTagIgnoresEnvKnownHashes(t *testing.T) {
	t.Setenv("LIMENSAFE_RELEASE_CATALOG_OK", "")

	repo := initAttestationRepo(t)
	attestedHash := "sha256:" + strings.Repeat("a", 64)
	committedHash := "sha256:" + strings.Repeat("b", 64)
	writeKnownCatalogHashes(t, repo, committedHash+"\n")
	git(t, repo, "add", defaultKnownHashesPath)
	git(t, repo, "commit", "-q", "-m", "known hashes")

	parent := gitOut(t, repo, "rev-parse", "HEAD")
	writeTestAttestation(t, repo, scanAttestation{
		SchemaVersion: attestationSchemaV1,
		CommitSHA:     parent,
		ScanTimestamp: time.Now().UTC(),
		ToolVersion:   "test",
		CatalogIDHash: attestedHash,
		Visibility:    "public_oss",
		ExitCode:      0,
		Operator:      "test",
		ScanMetadata:  attestationScanMetadata{FilesScanned: 1},
	})
	git(t, repo, "add", defaultAttestationPath)
	git(t, repo, "commit", "-q", "-m", "attest")

	t.Setenv("LIMENSAFE_KNOWN_CATALOG_HASHES", attestedHash)
	opts, err := verifierOptionsFromEnv(repo, "tag")
	if err != nil {
		t.Fatalf("verifier options: %v", err)
	}
	err = verifyAttestation(repo, defaultAttestationPath, opts)
	if err == nil || !strings.Contains(err.Error(), "known-catalog-hashes") {
		t.Fatalf("expected env known-hash rejection, got %v", err)
	}
}

func TestHashAttestationInputsRejectsConfigFile(t *testing.T) {
	if _, err := hashAttestationInputs(nil, "config.yaml"); err == nil {
		t.Fatal("expected config-file hashing to be rejected")
	}
}

func initAttestationRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	git(t, repo, "init", "-q", "-b", "main")
	git(t, repo, "config", "user.email", "test@example.com")
	git(t, repo, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("clean\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "README.md")
	git(t, repo, "commit", "-q", "-m", "initial")
	return repo
}

func writeTestAttestation(t *testing.T, repo string, att scanAttestation) {
	t.Helper()
	if err := writeAttestation(filepath.Join(repo, defaultAttestationPath), att); err != nil {
		t.Fatal(err)
	}
}

func writeKnownCatalogHashes(t *testing.T, repo, content string) {
	t.Helper()
	path := filepath.Join(repo, defaultKnownHashesPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
