package scripts

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseInventory(t *testing.T) {
	script, err := filepath.Abs("release-inventory.py")
	if err != nil {
		t.Fatal(err)
	}
	checksums, err := filepath.Abs("generate-checksums.sh")
	if err != nil {
		t.Fatal(err)
	}
	write := func(t *testing.T, path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var names []string
	for _, platform := range []string{"linux", "darwin", "windows"} {
		for _, arch := range []string{"amd64", "arm64"} {
			name := "fixture-" + platform + "-" + arch
			if platform == "windows" {
				name += ".exe"
			}
			names = append(names, name)
		}
	}
	provenance := []string{"SHA256SUMS", "SHA512SUMS", "SHA256SUMS.minisig", "SHA512SUMS.minisig", "fixture-minisign.pub", "release-notes-v1.2.3.md"}
	env := []string{}
	for _, variable := range os.Environ() {
		key := strings.SplitN(variable, "=", 2)[0]
		if key != "PGP_KEY_ID" && key != "SIGNING_ENV_PREFIX" && key != "SIGNING_APP_NAME" && !strings.HasPrefix(key, "LIMENSAFE_") {
			env = append(env, variable)
		}
	}
	env = append(env, "SIGNING_APP_NAME=fixture")
	run := func(t *testing.T, env []string, program string, args ...string) (string, error) {
		t.Helper()
		cmd := exec.Command(program, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	fixture := func(t *testing.T) string {
		dir := t.TempDir()
		for _, name := range names {
			write(t, filepath.Join(dir, name), name+"\n")
		}
		if out, err := run(t, env, "bash", checksums, dir, "fixture"); err != nil {
			t.Fatalf("generate: %v %s", err, out)
		}
		return dir
	}
	for _, mode := range []string{"build", "all", "provenance"} {
		t.Run(mode, func(t *testing.T) {
			dir := fixture(t)
			if mode != "build" {
				for _, name := range provenance[2:] {
					write(t, filepath.Join(dir, name), "disposable inventory placeholder\n")
				}
			}
			out, err := run(t, env, "python3", script, dir, "fixture", "v1.2.3", mode)
			if err != nil {
				t.Fatalf("positive: %v %s", err, out)
			}
			count := 8
			if mode == "all" {
				count = 12
			}
			if mode == "provenance" {
				count = 6
			}
			if len(strings.Split(strings.TrimSpace(out), "\n")) != count {
				t.Fatal("incorrect upload count")
			}
			if mode != "build" {
				configured := append(append([]string{}, env...), "SIGNING_ENV_PREFIX=LIMENSAFE", "LIMENSAFE_PGP_KEY_ID=FIXTURE!", "PGP_KEY_ID=FIXTURE!")
				if out, err := run(t, configured, "python3", script, dir, "fixture", "v1.2.3", mode); err != nil {
					t.Fatalf("tag selector forced PGP assets: %v %s", err, out)
				}
			}
			for _, tag := range []string{"v01.2.3", "v1.02.3", "v1.2.03", "v1.2.3-rc.01"} {
				if out, err := run(t, env, "python3", script, dir, "fixture", tag, mode); err == nil {
					t.Fatalf("noncanonical tag accepted: %s", out)
				}
			}
		})
	}
	for _, missing := range append(append([]string{}, names...), provenance...) {
		t.Run("missing-"+missing, func(t *testing.T) {
			dir := fixture(t)
			for _, name := range provenance[2:] {
				write(t, filepath.Join(dir, name), "fixture\n")
			}
			if err := os.Remove(filepath.Join(dir, missing)); err != nil {
				t.Fatal(err)
			}
			if out, err := run(t, env, "python3", script, dir, "fixture", "v1.2.3", "all"); err == nil {
				t.Fatalf("missing accepted: %s", out)
			}
		})
	}
	for _, scenario := range []string{"unexpected", "symlink", "partial-pgp", "complete-pgp", "tampered-binary", "duplicate-manifest"} {
		t.Run(scenario, func(t *testing.T) {
			dir := fixture(t)
			for _, name := range provenance[2:] {
				write(t, filepath.Join(dir, name), "fixture\n")
			}
			switch scenario {
			case "unexpected":
				write(t, filepath.Join(dir, "fixture-extra"), "extra\n")
			case "symlink":
				p := filepath.Join(dir, names[0])
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(dir, names[1]), p); err != nil {
					t.Fatal(err)
				}
			case "partial-pgp":
				write(t, filepath.Join(dir, "SHA256SUMS.asc"), "signature\n")
			case "complete-pgp":
				for _, name := range []string{"SHA256SUMS.asc", "SHA512SUMS.asc", "fulmenhq-release-signing-key.asc"} {
					write(t, filepath.Join(dir, name), "fixture\n")
				}
			case "tampered-binary":
				write(t, filepath.Join(dir, names[0]), "changed\n")
			case "duplicate-manifest":
				p := filepath.Join(dir, "SHA256SUMS")
				b, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				rows := strings.Split(strings.TrimSpace(string(b)), "\n")
				rows[1] = rows[0]
				write(t, p, strings.Join(rows, "\n")+"\n")
			}
			out, err := run(t, env, "python3", script, dir, "fixture", "v1.2.3", "all")
			if (err == nil) != (scenario == "complete-pgp") {
				t.Fatalf("result: %v %s", err, out)
			}
		})
	}
	t.Run("checksums-exclude-public-key", func(t *testing.T) {
		dir := fixture(t)
		write(t, filepath.Join(dir, "fixture-minisign.pub"), "public key\n")
		if out, err := run(t, env, "bash", checksums, dir, "fixture"); err != nil {
			t.Fatalf("regenerate: %v %s", err, out)
		}
		b, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "minisign") || len(strings.Split(strings.TrimSpace(string(b)), "\n")) != 6 {
			t.Fatal("checksum inventory widened")
		}
	})
	t.Run("minisign-only-export-with-tag-selector", func(t *testing.T) {
		dir := fixture(t)
		for _, name := range provenance[2:] {
			write(t, filepath.Join(dir, name), "fixture\n")
		}
		pub := filepath.Join(t.TempDir(), "public.pub")
		write(t, pub, "disposable public inventory fixture\n")
		exporter, err := filepath.Abs("export-release-keys.sh")
		if err != nil {
			t.Fatal(err)
		}
		tools := t.TempDir()
		marker := filepath.Join(tools, "gpg-called")
		fake := filepath.Join(tools, "gpg")
		write(t, fake, fmt.Sprintf("#!/bin/sh\ntouch '%s'\nexit 1\n", marker))
		if err := os.Chmod(fake, 0o700); err != nil {
			t.Fatal(err)
		}
		configured := append(append([]string{}, env...), "SIGNING_ENV_PREFIX=LIMENSAFE", "LIMENSAFE_PGP_KEY_ID=FIXTURE!", "LIMENSAFE_MINISIGN_PUB="+pub,
			"PATH="+tools+string(os.PathListSeparator)+os.Getenv("PATH"))
		// Fake minisign exercises dispatch only; no real private key or
		// signature is used by this non-crypto inventory test.
		fakeMini := filepath.Join(tools, "minisign")
		write(t, fakeMini, "#!/bin/sh\nwhile [ \"$#\" -gt 0 ]; do\n if [ \"$1\" = -m ]; then shift; manifest=\"$1\"; fi\n shift\ndone\nprintf 'fixture signature\\n' > \"$manifest.minisig\"\n")
		if err := os.Chmod(fakeMini, 0o700); err != nil {
			t.Fatal(err)
		}
		fakeKey := filepath.Join(tools, "fake.key")
		write(t, fakeKey, "not a real private key\n")
		signing, err := filepath.Abs("sign-release-manifests.sh")
		if err != nil {
			t.Fatal(err)
		}
		fakeEnv := append(append([]string{}, configured...), "CI=false", "LIMENSAFE_MINISIGN_KEY="+fakeKey)
		if out, err := run(t, fakeEnv, "bash", signing, "v1.2.3", dir); err != nil {
			t.Fatalf("minisign-only signing selected PGP: %v %s", err, out)
		}
		if out, err := run(t, configured, "bash", exporter, dir); err != nil {
			t.Fatalf("minisign-only export failed: %v %s", err, out)
		}
		if out, err := run(t, configured, "python3", script, dir, "fixture", "v1.2.3", "all"); err != nil {
			t.Fatalf("export made partial PGP set: %v %s", err, out)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatal("tag selector caused GPG export")
		}
		write(t, filepath.Join(dir, "SHA256SUMS.asc"), "partial\n")
		if out, err := run(t, configured, "bash", exporter, dir); err != nil {
			t.Fatalf("minisign exporter consulted PGP: %v %s", err, out)
		}
		if out, err := run(t, configured, "python3", script, dir, "fixture", "v1.2.3", "all"); err == nil {
			t.Fatalf("partial PGP inventory accepted: %s", out)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatal("partial PGP set accessed GPG")
		}
	})
	for _, upload := range []string{"release-upload.sh", "release-upload-provenance.sh"} {
		t.Run("upload-"+upload, func(t *testing.T) {
			path, err := filepath.Abs(upload)
			if err != nil {
				t.Fatal(err)
			}
			tools := t.TempDir()
			marker := filepath.Join(tools, "called")
			fake := filepath.Join(tools, "gh")
			write(t, fake, fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" > '%s'\n", marker))
			if err := os.Chmod(fake, 0o700); err != nil {
				t.Fatal(err)
			}
			uploadEnv := append(append([]string{}, env...), "PATH="+tools+string(os.PathListSeparator)+os.Getenv("PATH"))
			dir := fixture(t)
			for _, name := range provenance[2:] {
				write(t, filepath.Join(dir, name), "fixture\n")
			}
			if out, err := run(t, uploadEnv, "bash", path, "v1.2.3", dir); err != nil {
				t.Fatalf("positive upload: %v %s", err, out)
			}
			if err := os.Remove(marker); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(dir, "fixture-windows-arm64.exe")); err != nil {
				t.Fatal(err)
			}
			if out, err := run(t, uploadEnv, "bash", path, "v1.2.3", dir); err == nil {
				t.Fatalf("incomplete upload accepted: %s", out)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("failed gate called gh")
			}
		})
	}
}
