package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/fulmenhq/limensafe/pkg/output"
)

const (
	defaultAttestationPath = ".limensafe/scan-attestation.json"
	defaultKnownHashesPath = ".limensafe/known-catalog-hashes.txt"
	attestationSchemaV1    = "1.0.0"
)

var (
	attestCatalogs   []string
	attestConfigFile string
	attestVisibility string
	attestMode       string
	attestDiffBase   string
	attestOutput     string

	verifyAttestationFile string
	verifyAttestationMode string
)

type scanAttestation struct {
	SchemaVersion string                  `json:"schema_version"`
	CommitSHA     string                  `json:"commit_sha"`
	ScanTimestamp time.Time               `json:"scan_timestamp"`
	ToolVersion   string                  `json:"tool_version"`
	CatalogIDHash string                  `json:"catalog_id_hash"`
	Visibility    string                  `json:"visibility"`
	ExitCode      int                     `json:"exit_code"`
	Operator      string                  `json:"operator"`
	ScanMetadata  attestationScanMetadata `json:"scan_metadata"`
}

type attestationScanMetadata struct {
	FilesScanned  int   `json:"files_scanned"`
	FindingsTotal int   `json:"findings_total"`
	DurationMS    int64 `json:"duration_ms"`
}

var attestCmd = &cobra.Command{
	Use:   "attest [path]",
	Short: "Run a local scan and write a scan attestation file",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runAttest,
}

var verifyAttestationCmd = &cobra.Command{
	Use:   "verify-attestation",
	Short: "Verify the committed scan attestation",
	Args:  cobra.NoArgs,
	RunE:  runVerifyAttestation,
}

func init() {
	attestCmd.Flags().StringSliceVar(&attestCatalogs, "catalog", nil,
		"Path to a catalog YAML file used for the attested scan")
	attestCmd.Flags().StringVar(&attestConfigFile, "config-file", "",
		"Reserved; attest currently requires explicit --catalog paths")
	attestCmd.Flags().StringVar(&attestVisibility, "visibility", "public_oss",
		"Repo visibility scope for the attested scan")
	attestCmd.Flags().StringVar(&attestMode, "mode", "local",
		"Scan posture macro for the attested scan (local|ci|release)")
	attestCmd.Flags().StringVar(&attestDiffBase, "diff-base", "origin/main",
		"Base ref for the attested introduced-lines scan (ignored for bare repositories, which attest HEAD's tracked tree)")
	attestCmd.Flags().StringVar(&attestOutput, "output", defaultAttestationPath,
		"Path to write the attestation JSON")

	verifyAttestationCmd.Flags().StringVar(&verifyAttestationFile, "file", defaultAttestationPath,
		"Path to the scan attestation JSON")
	verifyAttestationCmd.Flags().StringVar(&verifyAttestationMode, "mode", "push",
		"Verification mode (push|tag)")

	rootCmd.AddCommand(attestCmd)
	rootCmd.AddCommand(verifyAttestationCmd)
}

func runAttest(cmdObj *cobra.Command, args []string) error {
	scanRoot := "."
	if len(args) == 1 {
		scanRoot = args[0]
	}
	if len(attestCatalogs) == 0 {
		return fmt.Errorf("attest: at least one explicit --catalog path is required")
	}
	if attestConfigFile != "" {
		return fmt.Errorf("attest: --config-file is not supported until catalog source content hashing is implemented; pass explicit --catalog paths")
	}
	if attestMode != "" && !validScanMode(attestMode) {
		return fmt.Errorf("attest: --mode must be one of local, ci, release")
	}

	commitSHA, err := gitOutput(scanRoot, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	isBare, err := gitIsBareRepository(scanRoot)
	if err != nil {
		return err
	}
	catalogHash, err := hashAttestationInputs(attestCatalogs, attestConfigFile)
	if err != nil {
		return err
	}

	scanArgs := []string{"scan", scanRoot, "--diff", "--diff-base", attestDiffBase, "--visibility", attestVisibility}
	if isBare {
		scanArgs = []string{"scan", scanRoot, "--git-archive=HEAD", "--visibility", attestVisibility}
	}
	if attestMode != "" {
		scanArgs = append(scanArgs, "--mode", attestMode)
	}
	if attestConfigFile != "" {
		scanArgs = append(scanArgs, "--config-file", attestConfigFile)
	}
	for _, path := range attestCatalogs {
		scanArgs = append(scanArgs, "--catalog", path)
	}

	var stdout, stderr bytes.Buffer
	scan := exec.Command(os.Args[0], scanArgs...)
	scan.Stdout = &stdout
	scan.Stderr = &stderr
	if err := scan.Run(); err != nil {
		if stderr.Len() > 0 {
			_, _ = fmt.Fprint(os.Stderr, stderr.String())
		}
		return fmt.Errorf("attest: scan failed; refusing to write attestation: %w", err)
	}

	var scanOut output.Output
	dec := json.NewDecoder(&stdout)
	if err := dec.Decode(&scanOut); err != nil {
		return fmt.Errorf("attest: parse scan output: %w", err)
	}

	att := scanAttestation{
		SchemaVersion: attestationSchemaV1,
		CommitSHA:     strings.TrimSpace(commitSHA),
		ScanTimestamp: time.Now().UTC(),
		ToolVersion:   versionInfo.Version,
		CatalogIDHash: catalogHash,
		Visibility:    attestVisibility,
		ExitCode:      0,
		Operator:      attestationOperator(),
		ScanMetadata: attestationScanMetadata{
			FilesScanned:  scanOut.ScanMetadata.FilesScanned,
			FindingsTotal: scanOut.Summary.FindingsTotal,
			DurationMS:    scanOut.ScanMetadata.DurationMS,
		},
	}
	if err := writeAttestation(attestOutput, att); err != nil {
		return err
	}
	if isBare {
		fmt.Fprintf(os.Stderr, "attestation written: %s\n", attestOutput)
		return nil
	}
	if err := gitAdd(scanRoot, attestOutput); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "attestation written and staged: %s\n", attestOutput)
	return nil
}

func runVerifyAttestation(cmdObj *cobra.Command, args []string) error {
	opts, err := verifierOptionsFromEnv(".", verifyAttestationMode)
	if err != nil {
		return err
	}
	if err := verifyAttestation(".", verifyAttestationFile, opts); err != nil {
		return fmt.Errorf("verify-attestation: %w", err)
	}
	fmt.Fprintf(os.Stderr, "scan attestation verified: %s mode=%s\n", verifyAttestationFile, opts.Mode)
	return nil
}

type attestationVerifyOptions struct {
	Mode            string
	MaxAge          time.Duration
	KnownHashes     map[string]bool
	AllowHashBypass bool
}

func verifierOptionsFromEnv(repoRoot, mode string) (attestationVerifyOptions, error) {
	switch mode {
	case "push":
		return attestationVerifyOptions{
			Mode:   mode,
			MaxAge: durationFromEnv("LIMENSAFE_ATTESTATION_MAX_AGE_PUSH", 24*time.Hour),
		}, nil
	case "tag":
		hashes, err := knownCatalogHashes(repoRoot, defaultKnownHashesPath)
		if err != nil {
			return attestationVerifyOptions{}, err
		}
		return attestationVerifyOptions{
			Mode:            mode,
			MaxAge:          durationFromEnv("LIMENSAFE_ATTESTATION_MAX_AGE_TAG", time.Hour),
			KnownHashes:     hashes,
			AllowHashBypass: os.Getenv("LIMENSAFE_RELEASE_CATALOG_OK") == "1",
		}, nil
	default:
		return attestationVerifyOptions{}, fmt.Errorf("--mode must be one of push, tag")
	}
}

func verifyAttestation(repoRoot, path string, opts attestationVerifyOptions) error {
	att, err := readCommittedAttestation(repoRoot, path)
	if err != nil {
		return err
	}
	if att.SchemaVersion != attestationSchemaV1 {
		return fmt.Errorf("schema_version %q is not supported", att.SchemaVersion)
	}
	if strings.TrimSpace(att.CommitSHA) == "" {
		return errors.New("commit_sha is required")
	}
	if att.ExitCode != 0 {
		return fmt.Errorf("exit_code is %d, want 0", att.ExitCode)
	}
	now := time.Now()
	if att.ScanTimestamp.After(now.Add(5 * time.Minute)) {
		return fmt.Errorf("scan_timestamp is in the future")
	}
	if now.Sub(att.ScanTimestamp) > opts.MaxAge {
		return fmt.Errorf("scan_timestamp is older than %s", opts.MaxAge)
	}
	if att.CatalogIDHash == "" {
		return errors.New("catalog_id_hash is required")
	}
	if opts.Mode == "tag" && !opts.AllowHashBypass && !opts.KnownHashes[att.CatalogIDHash] {
		return fmt.Errorf("catalog_id_hash is not in .limensafe/known-catalog-hashes.txt; set LIMENSAFE_RELEASE_CATALOG_OK=1 only for an approved alternate catalog")
	}

	head, err := gitOutput(repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	head = strings.TrimSpace(head)
	if att.CommitSHA == head {
		return nil
	}
	parent, _ := gitOutput(repoRoot, "rev-parse", "HEAD~1")
	parent = strings.TrimSpace(parent)
	if (opts.Mode == "push" || opts.Mode == "tag") && parent != "" && att.CommitSHA == parent {
		onlyAttestation, err := headDiffOnlyAttestation(repoRoot, path)
		if err != nil {
			return err
		}
		if onlyAttestation {
			return nil
		}
	}
	return fmt.Errorf("commit_sha %s does not match current HEAD %s", att.CommitSHA, head)
}

func readCommittedAttestation(repoRoot, path string) (scanAttestation, error) {
	blob, err := gitOutput(repoRoot, "show", "HEAD:"+filepath.ToSlash(path))
	if err != nil {
		return scanAttestation{}, fmt.Errorf("read committed attestation %s: %w", path, err)
	}
	return decodeAttestation(strings.NewReader(blob), path)
}

func decodeAttestation(r io.Reader, label string) (scanAttestation, error) {
	var att scanAttestation
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&att); err != nil {
		return scanAttestation{}, fmt.Errorf("parse %s: %w", label, err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return scanAttestation{}, fmt.Errorf("parse %s: trailing JSON content", label)
	}
	return att, nil
}

func writeAttestation(path string, att scanAttestation) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("attest: mkdir %s: %w", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(att, "", "  ")
	if err != nil {
		return fmt.Errorf("attest: encode attestation: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("attest: write %s: %w", path, err)
	}
	return nil
}

func hashAttestationInputs(catalogs []string, configFile string) (string, error) {
	if configFile != "" {
		return "", fmt.Errorf("attest: config-file hashing is not supported")
	}
	paths := append([]string(nil), catalogs...)
	sort.Strings(paths)
	h := sha256.New()
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("attest: hash %s: %w", path, err)
		}
		_, _ = h.Write([]byte(filepath.Base(path)))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(data)
		_, _ = h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func attestationOperator() string {
	for _, key := range []string{"LANYTE_AGENT_ROLE", "USER", "USERNAME"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return "unknown"
}

func durationFromEnv(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return d
}

func knownCatalogHashes(repoRoot, path string) (map[string]bool, error) {
	hashes := map[string]bool{}
	add := func(raw string) {
		for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
			return r == ':' || r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' '
		}) {
			if strings.HasPrefix(part, "sha256") {
				continue
			}
			if len(part) == 64 {
				hashes["sha256:"+part] = true
				continue
			}
			if strings.HasPrefix(part, "sha256:") {
				hashes[part] = true
			}
		}
	}
	blob, err := gitOutput(repoRoot, "show", "HEAD:"+filepath.ToSlash(path))
	if err != nil {
		return nil, fmt.Errorf("read committed known catalog hashes %s: %w", path, err)
	}
	add(blob)
	return hashes, nil
}

func gitOutput(repoRoot string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

func gitIsBareRepository(repoRoot string) (bool, error) {
	out, err := gitOutput(repoRoot, "rev-parse", "--is-bare-repository")
	if err != nil {
		return false, err
	}
	switch strings.TrimSpace(out) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("git rev-parse --is-bare-repository returned %q", strings.TrimSpace(out))
	}
}

func gitAdd(repoRoot, path string) error {
	cmd := exec.Command("git", "add", path)
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add %s: %w (%s)", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func headDiffOnlyAttestation(repoRoot, attestationPath string) (bool, error) {
	out, err := gitOutput(repoRoot, "diff", "--name-only", "HEAD~1..HEAD")
	if err != nil {
		return false, err
	}
	allowed := filepath.ToSlash(attestationPath)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		if filepath.ToSlash(line) != allowed {
			return false, nil
		}
	}
	return true, nil
}
