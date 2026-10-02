//go:build releasecrypto

package scripts

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestReleaseTagPinned(t *testing.T) {
	script, err := filepath.Abs("release-pin.py")
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.MkdirTemp("/tmp", "lim-pin-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = tagRun("", tagEnv(), "gpgconf", "--homedir", home, "--kill", "all")
		if err := os.RemoveAll(home); err != nil {
			t.Error(err)
		}
	})
	gpg := func(args ...string) string {
		return tagMust(t, "", tagEnv(), "gpg", append([]string{"--homedir", home, "--batch", "--pinentry-mode", "loopback", "--passphrase", ""}, args...)...)
	}
	fprs := func() []string {
		var result []string
		for _, line := range strings.Split(gpg("--with-colons", "--list-keys"), "\n") {
			fields := strings.Split(line, ":")
			if fields[0] == "fpr" {
				result = append(result, fields[9])
			}
		}
		return result
	}
	gpg("--quick-gen-key", "Pin Fixture <pin@example.test>", "ed25519", "cert,sign", "never")
	primary := fprs()[0]
	gpg("--quick-add-key", primary, "ed25519", "sign", "never")
	gpg("--quick-add-key", primary, "cv25519", "encr", "never")
	signer := fprs()[1]
	public := gpg("--armor", "--export", primary) + "\n"
	// Valid disposable minisign public blob; no operator key enters the fixture.
	blob := make([]byte, 42)
	copy(blob, "Ed")
	for i := 2; i < len(blob); i++ {
		blob[i] = byte(i)
	}
	minipub := filepath.Join(t.TempDir(), "fixture.pub")
	tagWrite(t, minipub, "untrusted comment: disposable fixture\n"+base64.StdEncoding.EncodeToString(blob)+"\n")
	minipub, err = filepath.EvalSymlinks(minipub)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"three-record positive", "unauthorized sibling", "primary signature", "bad anchor", "wrong primary", "tampered ndjson", "duplicate ndjson field", "changed minisign id", "missing pin", "extra primary", "unsigned", "annotation", "two-marker annotation", "wrong target", "remote mismatch", "revoked signer", "symlink pin", "private export"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			env := tagEnv("LIMENSAFE_RELEASE_TAG=v1.2.3", "LIMENSAFE_GPG_PRIMARY_FINGERPRINT="+primary,
				"LIMENSAFE_PGP_KEY_ID="+signer+"!", "LIMENSAFE_MINISIGN_PUB="+minipub, "LIMENSAFE_GPG_HOMEDIR="+home)
			git := func(args ...string) string { return tagMust(t, dir, tagEnv(), "git", args...) }
			git("init", "-q", "-b", "main")
			git("config", "user.name", "Pin Fixture")
			git("config", "user.email", "pin@example.test")
			if err := os.MkdirAll(filepath.Join(dir, "docs/security"), 0o700); err != nil {
				t.Fatal(err)
			}
			if scenario == "three-record positive" {
				tagMust(t, dir, env, "python3", script, "export")
				if out, err := tagRun(dir, env, "python3", script, "export"); err == nil {
					t.Fatalf("export overwrote pin: %s", out)
				}
				defaultRoot := t.TempDir()
				defaultHome := filepath.Join(defaultRoot, ".gnupg")
				if err := os.Mkdir(defaultHome, 0o700); err != nil {
					t.Fatal(err)
				}
				defaultEnv := tagEnv("HOME="+defaultRoot, "LIMENSAFE_GPG_HOMEDIR="+defaultHome,
					"LIMENSAFE_GPG_PRIMARY_FINGERPRINT="+primary, "LIMENSAFE_PGP_KEY_ID="+signer+"!", "LIMENSAFE_MINISIGN_PUB="+minipub)
				if out, err := tagRun(dir, defaultEnv, "python3", script, "export"); err == nil || !strings.Contains(out, "default GPG home forbidden") {
					t.Fatalf("default keyring export: %v %s", err, out)
				}
			} else {
				tagWrite(t, filepath.Join(dir, "docs/security/release-signing-keys.asc"), public)
			}
			tagMust(t, dir, env, "python3", script, "validate")
			tagMust(t, dir, env, "python3", script, "insert")
			tagWrite(t, filepath.Join(dir, "VERSION"), "1.2.3\n")
			key := signer + "!"
			message := "Release v1.2.3\n"
			switch scenario {
			case "primary signature":
				key = primary + "!"
			case "bad anchor":
				tagWrite(t, filepath.Join(dir, "keys/expected-fingerprints.txt"), "gpg "+strings.Repeat("0", 40)+"\nminisign "+strings.Repeat("0", 64)+"\n")
			case "wrong primary":
				p := filepath.Join(dir, "keys/expected-fingerprints.txt")
				b, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				tagWrite(t, p, strings.ReplaceAll(string(b), primary, strings.Repeat("0", 40)))
				p = filepath.Join(dir, "keys/expected-fingerprints.ndjson")
				b, err = os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				tagWrite(t, p, strings.ReplaceAll(string(b), primary, strings.Repeat("0", 40)))
			case "tampered ndjson":
				p := filepath.Join(dir, "keys/expected-fingerprints.ndjson")
				b, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				tagWrite(t, p, strings.ReplaceAll(string(b), "openpgp-fingerprint-v1", "wrong-scheme"))
			case "duplicate ndjson field":
				p := filepath.Join(dir, "keys/expected-fingerprints.ndjson")
				b, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				tagWrite(t, p, strings.Replace(string(b), `"algorithm":"openpgp-fingerprint"`, `"algorithm":"wrong","algorithm":"openpgp-fingerprint"`, 1))
			case "changed minisign id":
				p := filepath.Join(dir, "keys/expected-fingerprints.ndjson")
				b, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				tagWrite(t, p, strings.Replace(string(b), `"key_id":"0203040506070809"`, `"key_id":"0000000000000000"`, 1))
			case "symlink pin":
				p := filepath.Join(dir, "docs/security/release-signing-keys.asc")
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(minipub, p); err != nil {
					t.Fatal(err)
				}
			case "private export":
				tagWrite(t, filepath.Join(dir, "docs/security/release-signing-keys.asc"), gpg("--armor", "--export-secret-keys", primary)+"\n")
			case "missing pin":
				if err := os.Remove(filepath.Join(dir, "docs/security/release-signing-keys.asc")); err != nil {
					t.Fatal(err)
				}
			case "annotation":
				message = "Other message\n"
			case "two-marker annotation":
				message += "-----BEGIN PGP SIGNATURE-----\nextra\n"
			case "unauthorized sibling":
				gpg("--quick-add-key", primary, "ed25519", "sign", "never")
				key = fprs()[3] + "!"
				// Include the cryptographically valid sibling, but never authorize it.
				tagWrite(t, filepath.Join(dir, "docs/security/release-signing-keys.asc"), gpg("--armor", "--export", primary)+"\n")
			case "extra primary":
				gpg("--quick-gen-key", "Other Fixture <other@example.test>", "ed25519", "cert", "never")
				tagWrite(t, filepath.Join(dir, "docs/security/release-signing-keys.asc"), gpg("--armor", "--export")+"\n")
			case "revoked signer":
				revHome, err := os.MkdirTemp("/tmp", "lim-revoke-")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					_, _ = tagRun("", tagEnv(), "gpgconf", "--homedir", revHome, "--kill", "all")
					if err := os.RemoveAll(revHome); err != nil {
						t.Error(err)
					}
				})
				input := func(body string, args ...string) {
					cmd := exec.Command("gpg", append([]string{"--homedir", revHome, "--batch", "--yes", "--pinentry-mode", "loopback", "--passphrase", ""}, args...)...)
					cmd.Env, cmd.Stdin = tagEnv(), strings.NewReader(body)
					if out, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("revocation fixture: %v %s", err, out)
					}
				}
				input(gpg("--armor", "--export-secret-keys", primary), "--import")
				input("key 1\nrevkey\ny\n0\n\ny\nsave\n", "--command-fd", "0", "--status-fd", "2", "--edit-key", primary)
				revoked := tagMust(t, "", tagEnv(), "gpg", "--homedir", revHome, "--batch", "--armor", "--export", primary)
				tagWrite(t, filepath.Join(dir, "docs/security/release-signing-keys.asc"), revoked+"\n")
			}
			if scenario == "private export" {
				if out, err := tagRun(dir, env, "python3", script, "validate"); err == nil {
					t.Fatalf("private export accepted: %s", out)
				}
				// Never add even disposable secret exports to a Git object store.
				return
			}
			git("add", ".")
			git("commit", "-q", "-m", "fixture")
			if scenario == "unsigned" {
				git("tag", "-a", "v1.2.3", "-m", "Release v1.2.3")
			} else {
				tagMust(t, dir, tagEnv("GNUPGHOME="+home), "git", "-c", "gpg.program=gpg", "tag", "-s", "-u", key, "--cleanup=verbatim", "v1.2.3", "-m", message)
				// In particular, a sibling signature is cryptographically valid
				// under its original keyring, but unauthorized by the pin.
				tagMust(t, dir, tagEnv("GNUPGHOME="+home), "git", "-c", "gpg.program=gpg", "verify-tag", "v1.2.3")
			}
			if scenario == "wrong target" {
				git("commit", "--allow-empty", "-q", "-m", "ahead")
			}
			action := "verify"
			if scenario == "remote mismatch" {
				remote := filepath.Join(t.TempDir(), "origin.git")
				tagMust(t, dir, tagEnv(), "git", "init", "--bare", "-q", remote)
				git("remote", "add", "origin", remote)
				git("push", "-q", "origin", "HEAD:refs/tags/v1.2.3")
				action = "verify-remote"
			}
			out, err := tagRun(dir, env, "python3", script, action)
			if scenario == "three-record positive" {
				if err != nil {
					t.Fatalf("positive failed: %v %s", err, out)
				}
				if out, err := tagRun(dir, env, "python3", script, "insert"); err == nil {
					t.Fatalf("overwrote anchors: %s", out)
				}
				tagMust(t, dir, env, "python3", script, "verify-minisign")
			} else if err == nil {
				t.Fatalf("negative accepted: %s", out)
			}
			if scenario == "duplicate ndjson field" && !strings.Contains(out, "duplicate machine-anchor field") {
				t.Fatalf("wrong duplicate rejection: %s", out)
			}
			if scenario == "changed minisign id" {
				if !strings.Contains(out, "minisign public key id mismatch") {
					t.Fatalf("wrong ID rejection: %s", out)
				}
				out, err := tagRun(dir, env, "python3", script, "verify-minisign")
				if err == nil || !strings.Contains(out, "minisign public key id mismatch") {
					t.Fatalf("minisign accepted changed ID: %v %s", err, out)
				}
				// No public file: the tag's GPG verification remains independent;
				// the minisign ID is advisory, never claimed as authenticated.
				tagMust(t, dir, tagEnv("LIMENSAFE_RELEASE_TAG=v1.2.3"), "python3", script, "verify")
			}
			if scenario == "bad anchor" {
				// Disposable stand-in for draft creation; verifier failure must
				// short-circuit it, without pushing a tag or calling GitHub.
				marker := filepath.Join(dir, "draft-created")
				_, _ = tagRun(dir, env, "bash", "-c", `python3 "$1" verify && touch "$2"`, "fixture", script, marker)
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Fatal("failed verifier reached draft")
				}
			}
		})
	}
}

func TestReleaseTagPinnedExpiry(t *testing.T) {
	script, err := filepath.Abs("release-pin.py")
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.MkdirTemp("/tmp", "lim-expiry-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = tagRun("", tagEnv(), "gpgconf", "--homedir", home, "--kill", "all")
		if err := os.RemoveAll(home); err != nil {
			t.Error(err)
		}
	})
	gpg := func(args ...string) string {
		return tagMust(t, "", tagEnv(), "gpg", append([]string{"--homedir", home, "--batch", "--pinentry-mode", "loopback", "--passphrase", "", "--faked-system-time", "20200101T000000"}, args...)...)
	}
	gpg("--quick-gen-key", "Expired Fixture <expired@example.test>", "ed25519", "cert", "never")
	var primary, signer string
	for _, line := range strings.Split(gpg("--with-colons", "--list-keys"), "\n") {
		f := strings.Split(line, ":")
		if f[0] == "fpr" {
			primary = f[9]
			break
		}
	}
	gpg("--quick-add-key", primary, "ed25519", "sign", "1d")
	for _, line := range strings.Split(gpg("--with-colons", "--list-keys"), "\n") {
		f := strings.Split(line, ":")
		if f[0] == "fpr" {
			signer = f[9]
		}
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "docs/security"), 0o700); err != nil {
		t.Fatal(err)
	}
	tagWrite(t, filepath.Join(dir, "docs/security/release-signing-keys.asc"), gpg("--armor", "--export", primary)+"\n")
	blob := make([]byte, 42)
	copy(blob, "Ed")
	mini := filepath.Join(dir, "fixture.pub")
	tagWrite(t, mini, "untrusted comment: disposable\n"+base64.StdEncoding.EncodeToString(blob)+"\n")
	mini, err = filepath.EvalSymlinks(mini)
	if err != nil {
		t.Fatal(err)
	}
	out, err := tagRun(dir, tagEnv("LIMENSAFE_GPG_PRIMARY_FINGERPRINT="+primary, "LIMENSAFE_PGP_KEY_ID="+signer+"!", "LIMENSAFE_MINISIGN_PUB="+mini), "python3", script, "validate")
	if err == nil || !strings.Contains(out, "expired") && !strings.Contains(out, "invalid key") {
		t.Fatalf("expiry accepted: %v %s", err, out)
	}
}

func TestReleaseTagPinnedDraftPredecessor(t *testing.T) {
	data, err := os.ReadFile("../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Needs string `yaml:"needs"`
			If    string `yaml:"if"`
			Steps []struct {
				Run      string                 `yaml:"run"`
				Uses     string                 `yaml:"uses"`
				Continue bool                   `yaml:"continue-on-error"`
				With     map[string]interface{} `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	verify, exists := workflow.Jobs["verify-signature"]
	if !exists {
		t.Fatal("verification job missing")
	}
	verified := false
	for _, step := range verify.Steps {
		if step.Run == "python3 scripts/release-pin.py verify" && !step.Continue {
			verified = true
		}
	}
	if !verified {
		t.Fatal("mandatory verification step missing")
	}
	release := workflow.Jobs["release"]
	if release.Needs != "verify-signature" || strings.Contains(release.If, "always(") || strings.Contains(release.If, "failure(") {
		t.Fatal("draft dependency may bypass failed verifier")
	}
	drafts := 0
	for _, step := range release.Steps {
		if strings.HasPrefix(step.Uses, "softprops/action-gh-release@") {
			if step.With["draft"] != true {
				t.Fatal("release action is not draft-only")
			}
			drafts++
		}
	}
	if drafts != 1 {
		t.Fatal("expected one gated draft step")
	}
}

func TestReleaseTagPinnedSSHAliases(t *testing.T) {
	script, err := filepath.Abs("release-pin.py")
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(t.TempDir(), "ssh-config")
	tagWrite(t, config, "Host fixture-good\n  HostName github.com\nHost fixture-bad\n  HostName example.test\n")
	// Use real ssh -G with a disposable config. The production resolver's
	// subprocess boundary is replaced only to inject that test config.
	code := `import importlib.util,sys
spec=importlib.util.spec_from_file_location("pin",sys.argv[1])
m=importlib.util.module_from_spec(spec); spec.loader.exec_module(m)
original=m.run
def fixture_run(args,*a,**k):
    assert args[:2]==["ssh","-G"]
    return original(["ssh","-F",sys.argv[2],*args[1:]],*a,**k)
m.run=fixture_run
assert m.github_repository("git@fixture-good:acme/example.git")=="acme/example"
assert m.github_repository("https://github.com/acme/example.git")=="acme/example"
for url in ["git@fixture-bad:acme/example.git", "git@-option:acme/example.git", "https://example.test/acme/example", "git@fixture-good:../example.git"]:
    try: m.github_repository(url)
    except ValueError: pass
    else: raise AssertionError("accepted invalid origin")
`
	tagMust(t, "", tagEnv("PYTHONDONTWRITEBYTECODE=1"), "python3", "-c", code, script, config)
}
