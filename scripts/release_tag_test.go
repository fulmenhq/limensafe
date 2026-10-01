//go:build releasecrypto

package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// All signing and pushes use throwaway keys and disposable local repositories.
// Missing crypto tools fail these tests; CI must not silently skip this gate.
func tagEnv(extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		key := strings.SplitN(kv, "=", 2)[0]
		if strings.HasPrefix(key, "LIMENSAFE_") || strings.HasPrefix(key, "GIT_") || key == "GNUPGHOME" || key == "CI" {
			continue
		}
		env = append(env, kv)
	}
	return append(append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0"), extra...)
}

func tagRun(dir string, env []string, program string, args ...string) (string, error) {
	cmd := exec.Command(program, args...)
	cmd.Dir, cmd.Env = dir, env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func tagMust(t *testing.T, dir string, env []string, program string, args ...string) string {
	t.Helper()
	out, err := tagRun(dir, env, program, args...)
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", program, args, err, out)
	}
	return strings.TrimSpace(out)
}

func tagWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseTag(t *testing.T) {
	for _, tool := range []string{"bash", "git", "gpg", "gpgconf"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("required release crypto tool %s: %v", tool, err)
		}
	}
	script, err := filepath.Abs("release-tag.sh")
	if err != nil {
		t.Fatal(err)
	}
	// GPG socket names need a short path on macOS; only throwaway key material
	// lives here, outside the repository and the operator's real keyring.
	home, err := os.MkdirTemp("/tmp", "lim-tag-")
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
	fprs := func(selector string) []string {
		listing := gpg("--with-colons", "--list-keys", selector)
		var result []string
		for _, line := range strings.Split(listing, "\n") {
			fields := strings.Split(line, ":")
			if len(fields) > 9 && fields[0] == "fpr" {
				result = append(result, fields[9])
			}
		}
		return result
	}
	gpg("--quick-gen-key", "Release Test <tagger@example.test>", "ed25519", "cert", "never")
	primary := fprs("tagger@example.test")[0]
	gpg("--quick-add-key", primary, "ed25519", "sign", "never")
	gpg("--quick-add-key", primary, "ed25519", "sign", "never")
	keys := fprs(primary)
	gpg("--quick-gen-key", "Other Test <other@example.test>", "ed25519", "sign", "never")
	foreign := fprs("other@example.test")[0]

	type fixture struct {
		dir, origin string
		vars        map[string]string
	}
	newRepo := func(t *testing.T) fixture {
		t.Helper()
		root := t.TempDir()
		r := fixture{dir: filepath.Join(root, "clone"), origin: filepath.Join(root, "origin.git"), vars: map[string]string{
			"LIMENSAFE_RELEASE_TAG": "v1.2.3", "LIMENSAFE_PGP_KEY_ID": keys[1] + "!",
			"LIMENSAFE_GPG_HOMEDIR": home, "LIMENSAFE_TAGGER_NAME": "Release Test", "LIMENSAFE_TAGGER_EMAIL": "tagger@example.test",
		}}
		tagMust(t, root, tagEnv(), "git", "init", "--quiet", "--bare", "--initial-branch=main", r.origin)
		tagMust(t, root, tagEnv(), "git", "clone", "--quiet", r.origin, r.dir)
		tagMust(t, r.dir, tagEnv(), "git", "config", "user.name", "Ambient Test")
		tagMust(t, r.dir, tagEnv(), "git", "config", "user.email", "ambient@example.test")
		tagWrite(t, filepath.Join(r.dir, "VERSION"), "1.2.3\n")
		tagMust(t, r.dir, tagEnv(), "git", "add", "VERSION")
		tagMust(t, r.dir, tagEnv(), "git", "commit", "--quiet", "-m", "initial")
		tagMust(t, r.dir, tagEnv(), "git", "push", "--quiet", "origin", "main")
		return r
	}
	env := func(r fixture) []string {
		var extra []string
		for k, v := range r.vars {
			extra = append(extra, k+"="+v)
		}
		return tagEnv(extra...)
	}
	call := func(r fixture, mode string) (string, error) { return tagRun(r.dir, env(r), "bash", script, mode) }
	git := func(t *testing.T, r fixture, args ...string) string {
		return tagMust(t, r.dir, tagEnv(), "git", args...)
	}
	fail := func(t *testing.T, r fixture, mode, want string) {
		t.Helper()
		out, err := call(r, mode)
		if err == nil || !strings.Contains(out, want) {
			t.Fatalf("wanted failure %q, err=%v output=%s", want, err, out)
		}
	}
	create := func(t *testing.T, r fixture) {
		t.Helper()
		if out, err := call(r, "create"); err != nil {
			t.Fatalf("create: %v\n%s", err, out)
		}
	}
	for _, variable := range []string{"LIMENSAFE_RELEASE_TAG", "LIMENSAFE_PGP_KEY_ID", "LIMENSAFE_GPG_HOMEDIR", "LIMENSAFE_TAGGER_NAME", "LIMENSAFE_TAGGER_EMAIL"} {
		t.Run("missing "+variable, func(t *testing.T) {
			r := newRepo(t)
			delete(r.vars, variable)
			fail(t, r, "create", variable+" is required")
			if git(t, r, "tag", "--list") != "" {
				t.Fatal("created a tag despite missing identity")
			}
		})
	}
	tests := []struct {
		name, mode, want string
		setup            func(t *testing.T, r fixture)
	}{
		{"version mismatch", "create", "does not match VERSION", func(t *testing.T, r fixture) { r.vars["LIMENSAFE_RELEASE_TAG"] = "v2.0.0" }},
		{"dirty", "create", "not clean", func(t *testing.T, r fixture) { tagWrite(t, filepath.Join(r.dir, "extra"), "dirty") }},
		{"wrong branch", "create", "require main", func(t *testing.T, r fixture) { git(t, r, "checkout", "-b", "other") }},
		{"stale main", "create", "does not match origin/main", func(t *testing.T, r fixture) { git(t, r, "commit", "--allow-empty", "-m", "ahead") }},
		{"wrong UID", "create", "not a live UID", func(t *testing.T, r fixture) { r.vars["LIMENSAFE_TAGGER_EMAIL"] = "other@example.test" }},
		{"short selector", "create", "full fingerprint", func(t *testing.T, r fixture) { r.vars["LIMENSAFE_PGP_KEY_ID"] = keys[1][32:] }},
		{"unforced subkey", "create", "requires !", func(t *testing.T, r fixture) { r.vars["LIMENSAFE_PGP_KEY_ID"] = keys[1] }},
		{"local duplicate", "create", "exists locally", func(t *testing.T, r fixture) { git(t, r, "tag", "v1.2.3") }},
		{"remote duplicate", "create", "exists on origin", func(t *testing.T, r fixture) { git(t, r, "push", "origin", "HEAD:refs/tags/v1.2.3") }},
		{"unsigned", "verify", "signature verification failed", func(t *testing.T, r fixture) {
			tagMust(t, r.dir, tagEnv("GIT_COMMITTER_NAME=Release Test", "GIT_COMMITTER_EMAIL=tagger@example.test"), "git", "tag", "-a", "v1.2.3", "-m", "unsigned")
		}},
		{"lightweight", "verify", "not an annotated", func(t *testing.T, r fixture) { git(t, r, "tag", "v1.2.3") }},
		{"changed target", "verify", "target is not HEAD", func(t *testing.T, r fixture) { create(t, r); git(t, r, "commit", "--allow-empty", "-m", "ahead") }},
		{"changed tagger", "verify", "unexpected tagger", func(t *testing.T, r fixture) { create(t, r); r.vars["LIMENSAFE_TAGGER_NAME"] = "Different" }},
		{"CI creation", "create", "disabled in CI", func(t *testing.T, r fixture) { r.vars["CI"] = "true" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { r := newRepo(t); tc.setup(t, r); fail(t, r, tc.mode, tc.want) })
	}
	for name, signer := range map[string]string{"foreign signer": foreign, "sibling signer": keys[2] + "!"} {
		t.Run(name, func(t *testing.T) {
			r := newRepo(t)
			tagMust(t, r.dir, tagEnv("GNUPGHOME="+home, "GIT_COMMITTER_NAME=Release Test", "GIT_COMMITTER_EMAIL=tagger@example.test"), "git", "-c", "gpg.program=gpg", "tag", "-s", "-u", signer, "v1.2.3", "-m", "test")
			fail(t, r, "push", "unexpected signing")
			if git(t, r, "ls-remote", "--tags", "origin") != "" {
				t.Fatal("bad signature published")
			}
		})
	}
	t.Run("create verify push only chosen tag", func(t *testing.T) {
		r := newRepo(t)
		create(t, r)
		if out, err := call(r, "verify"); err != nil {
			t.Fatalf("verify: %v %s", err, out)
		}
		git(t, r, "tag", "-a", "unrelated", "-m", "unrelated")
		git(t, r, "config", "push.followTags", "true")
		before := git(t, r, "ls-remote", "--heads", "origin")
		if out, err := call(r, "push"); err != nil {
			t.Fatalf("push: %v %s", err, out)
		}
		if got := git(t, r, "ls-remote", "--heads", "origin"); got != before {
			t.Fatal("push moved main")
		}
		want := git(t, r, "rev-parse", "refs/tags/v1.2.3") + "\trefs/tags/v1.2.3\n" + git(t, r, "rev-parse", "HEAD") + "\trefs/tags/v1.2.3^{}"
		if got := git(t, r, "ls-remote", "--tags", "origin"); got != want {
			t.Fatalf("unexpected remote tags: %s", got)
		}
		fail(t, r, "push", "exists on origin")
	})
	t.Run("unforced primary", func(t *testing.T) { r := newRepo(t); r.vars["LIMENSAFE_PGP_KEY_ID"] = primary; create(t, r) })
	t.Run("forced long key ID", func(t *testing.T) { r := newRepo(t); r.vars["LIMENSAFE_PGP_KEY_ID"] = keys[1][24:] + "!"; create(t, r) })
	for _, version := range []string{"01.2.3", "1.02.3", "1.2.03", "1.2.3-rc.01", "1.2.3-beta.00", "1.2.3-alpha.01"} {
		t.Run("noncanonical "+version, func(t *testing.T) {
			r := newRepo(t)
			tagWrite(t, filepath.Join(r.dir, "VERSION"), version+"\n")
			git(t, r, "add", "VERSION")
			git(t, r, "commit", "-m", "version fixture")
			git(t, r, "push", "origin", "main")
			r.vars["LIMENSAFE_RELEASE_TAG"] = "v" + version
			fail(t, r, "create", "not canonical")
			if git(t, r, "tag", "--list") != "" {
				t.Fatal("noncanonical tag created")
			}
		})
	}
	t.Run("primary stub with secret signing subkeys", func(t *testing.T) {
		r := newRepo(t)
		subHome, err := os.MkdirTemp("/tmp", "lim-sub-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, _ = tagRun("", tagEnv(), "gpgconf", "--homedir", subHome, "--kill", "all")
			if err := os.RemoveAll(subHome); err != nil {
				t.Error(err)
			}
		})
		secretSubkeys := gpg("--armor", "--export-secret-subkeys", primary)
		cmd := exec.Command("gpg", "--homedir", subHome, "--batch", "--import")
		cmd.Env, cmd.Stdin = tagEnv(), strings.NewReader(secretSubkeys)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("import: %v %s", err, out)
		}
		listing := tagMust(t, "", tagEnv(), "gpg", "--homedir", subHome, "--batch", "--with-colons", "--list-secret-keys", primary)
		var primaryStub, signingSecret bool
		for _, line := range strings.Split(listing, "\n") {
			fields := strings.Split(line, ":")
			if len(fields) > 14 {
				if fields[0] == "sec" && fields[14] == "#" {
					primaryStub = true
				}
				if fields[0] == "ssb" && fields[14] != "#" && strings.Contains(fields[11], "s") {
					signingSecret = true
				}
			}
		}
		if !primaryStub || !signingSecret {
			t.Fatalf("invalid subkey-only fixture: %s", listing)
		}
		r.vars["LIMENSAFE_GPG_HOMEDIR"], r.vars["LIMENSAFE_PGP_KEY_ID"] = subHome, primary
		create(t, r)
		if out, err := call(r, "verify"); err != nil {
			t.Fatalf("verify stub-primary: %v %s", err, out)
		}
	})
	t.Run("no ambient keyring fallback", func(t *testing.T) {
		r := newRepo(t)
		r.vars["LIMENSAFE_GPG_HOMEDIR"] = t.TempDir()
		r.vars["GNUPGHOME"] = home
		fail(t, r, "create", "missing or ambiguous")
		if git(t, r, "tag", "--list") != "" {
			t.Fatal("ambient keyring created a tag")
		}
	})
	t.Run("stale main at push", func(t *testing.T) {
		r := newRepo(t)
		create(t, r)
		other := filepath.Join(t.TempDir(), "other")
		tagMust(t, "", tagEnv(), "git", "clone", "--quiet", r.origin, other)
		tagMust(t, other, tagEnv(), "git", "-c", "user.name=Other", "-c", "user.email=other@example.test", "commit", "--allow-empty", "-m", "advance")
		tagMust(t, other, tagEnv(), "git", "push", "origin", "main")
		fail(t, r, "push", "does not match origin/main")
		if git(t, r, "ls-remote", "--tags", "origin") != "" {
			t.Fatal("stale tag pushed")
		}
	})
	t.Run("dirty at push", func(t *testing.T) {
		r := newRepo(t)
		create(t, r)
		tagWrite(t, filepath.Join(r.dir, "untracked"), "dirty")
		fail(t, r, "push", "not clean")
	})
	t.Run("alternate push URL refused", func(t *testing.T) {
		r := newRepo(t)
		create(t, r)
		decoy := filepath.Join(t.TempDir(), "decoy.git")
		tagMust(t, "", tagEnv(), "git", "init", "--bare", decoy)
		git(t, r, "config", "remote.origin.pushurl", decoy)
		fail(t, r, "push", "matching fetch/push destination")
		if got := tagMust(t, "", tagEnv(), "git", "--git-dir", decoy, "for-each-ref"); got != "" {
			t.Fatal("push altered decoy")
		}
	})
	t.Run("tampered signature", func(t *testing.T) {
		r := newRepo(t)
		create(t, r)
		object := strings.Replace(git(t, r, "cat-file", "tag", "v1.2.3"), "Release v1.2.3", "Changed message", 1) + "\n"
		cmd := exec.Command("git", "hash-object", "-t", "tag", "-w", "--stdin")
		cmd.Dir, cmd.Env, cmd.Stdin = r.dir, tagEnv(), strings.NewReader(object)
		sha, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		git(t, r, "update-ref", "refs/tags/v1.2.3", strings.TrimSpace(string(sha)))
		fail(t, r, "verify", "signature verification failed")
		fail(t, r, "push", "signature verification failed")
		if git(t, r, "ls-remote", "--tags", "origin") != "" {
			t.Fatal("tampered tag pushed")
		}
	})
	t.Run("wrong tag object name", func(t *testing.T) {
		r := newRepo(t)
		tagMust(t, r.dir, tagEnv("GNUPGHOME="+home, "GIT_COMMITTER_NAME=Release Test", "GIT_COMMITTER_EMAIL=tagger@example.test"), "git", "-c", "gpg.program=gpg", "tag", "-s", "-u", keys[1]+"!", "v1.2.4", "-m", "other version")
		git(t, r, "update-ref", "refs/tags/v1.2.3", git(t, r, "rev-parse", "refs/tags/v1.2.4"))
		fail(t, r, "push", "different name")
	})
	for name, message := range map[string]string{
		"wrong subject":         "Different message",
		"extra paragraph":       "Release v1.2.3\n\nExtra message",
		"extra blank line":      "Release v1.2.3\n",
		"embedded armor marker": "Release v1.2.3\n-----BEGIN PGP SIGNATURE-----\n\nHidden notes",
	} {
		t.Run("correct signature wrong message "+name, func(t *testing.T) {
			r := newRepo(t)
			tagMust(t, r.dir, tagEnv("GNUPGHOME="+home, "GIT_COMMITTER_NAME=Release Test", "GIT_COMMITTER_EMAIL=tagger@example.test"), "git", "-c", "gpg.program=gpg", "tag", "-s", "-u", keys[1]+"!", "--cleanup=verbatim", "v1.2.3", "-m", message+"\n")
			// Prove the fixture is cryptographically valid before expecting
			// rejection of only its message policy.
			tagMust(t, r.dir, tagEnv("GNUPGHOME="+home), "git", "-c", "gpg.program=gpg", "verify-tag", "v1.2.3")
			before := git(t, r, "ls-remote", "origin")
			fail(t, r, "verify", "unexpected tag message")
			fail(t, r, "push", "unexpected tag message")
			if got := git(t, r, "ls-remote", "origin"); got != before {
				t.Fatalf("wrong-message push changed origin: %s", got)
			}
		})
	}
}
