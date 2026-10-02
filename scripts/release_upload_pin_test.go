//go:build releasecrypto

package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseUploadPinnedMinisign(t *testing.T) {
	if _, err := os.Stat("release-pin.py"); err != nil {
		t.Fatal(err)
	}
	// Both real keys stay in disposable storage, never in the Git fixture.
	keys := t.TempDir()
	keys, err := filepath.EvalSymlinks(keys)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pinned", "replacement"} {
		tagMust(t, "", tagEnv(), "minisign", "-G", "-W", "-p", filepath.Join(keys, name+".pub"), "-s", filepath.Join(keys, name+".key"))
	}
	for _, target := range []string{"release-upload", "release-upload-all"} {
		for _, scenario := range []string{"pinned", "replacement matching signatures", "staged replacement"} {
			t.Run(target+"/"+scenario, func(t *testing.T) {
				dir := t.TempDir()
				git := func(args ...string) string { return tagMust(t, dir, tagEnv(), "git", args...) }
				git("init", "-q", "-b", "main")
				git("config", "user.name", "Upload Fixture")
				git("config", "user.email", "fixture@example.test")
				copyFile := func(source, destination string, mode os.FileMode) {
					b, err := os.ReadFile(source)
					if err != nil {
						t.Fatal(err)
					}
					path := filepath.Join(dir, destination)
					if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, b, mode); err != nil {
						t.Fatal(err)
					}
				}
				copyFile("../Makefile", "Makefile", 0o600)
				for _, name := range []string{"release-pin.py", "release-inventory.py", "release-upload.sh", "release-upload-provenance.sh", "verify-checksums.sh", "verify-minisign-public-key.sh", "verify-signatures.sh", "generate-checksums.sh"} {
					copyFile(name, "scripts/"+name, 0o700)
				}
				python := `import importlib.util,sys,pathlib
sys.dont_write_bytecode=True
spec=importlib.util.spec_from_file_location("pin","scripts/release-pin.py");m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
h,i=m.minisign(pathlib.Path(sys.argv[1]).read_bytes());text,rows=m.anchors("A"*40,h,i)
pathlib.Path("keys").mkdir();pathlib.Path(m.TEXT).write_bytes(text);pathlib.Path(m.JSON).write_bytes(rows)
`
				tagMust(t, dir, tagEnv(), "python3", "-c", python, filepath.Join(keys, "pinned.pub"))
				tagWrite(t, filepath.Join(dir, "VERSION"), "1.2.3\n")
				git("add", ".")
				git("-c", "commit.gpgsign=false", "commit", "-qm", "Fixture public anchors")
				stage := filepath.Join(dir, "dist/release")
				if err := os.MkdirAll(stage, 0o700); err != nil {
					t.Fatal(err)
				}
				for _, platform := range []string{"linux", "darwin", "windows"} {
					for _, arch := range []string{"amd64", "arm64"} {
						name := "limensafe-" + platform + "-" + arch
						if platform == "windows" {
							name += ".exe"
						}
						tagWrite(t, filepath.Join(stage, name), "fixture binary\n")
					}
				}
				tagMust(t, dir, tagEnv(), "bash", "scripts/generate-checksums.sh", stage, "limensafe")
				selected := "pinned"
				if scenario == "replacement matching signatures" {
					selected = "replacement"
				}
				for _, manifest := range []string{"SHA256SUMS", "SHA512SUMS"} {
					tagMust(t, dir, tagEnv(), "minisign", "-S", "-s", filepath.Join(keys, selected+".key"), "-m", filepath.Join(stage, manifest))
					// Prove replacement signatures are valid for their replacement
					// public key: refusal must come from committed-pin policy.
					tagMust(t, dir, tagEnv(), "minisign", "-V", "-p", filepath.Join(keys, selected+".pub"), "-m", filepath.Join(stage, manifest))
				}
				staged := selected
				if scenario == "staged replacement" {
					staged = "replacement"
				}
				copyFile(filepath.Join(keys, staged+".pub"), "dist/release/limensafe-minisign.pub", 0o600)
				tagWrite(t, filepath.Join(stage, "release-notes-v1.2.3.md"), "Fixture notes\n")
				tools := t.TempDir()
				marker := filepath.Join(tools, "gh-called")
				fake := filepath.Join(tools, "gh")
				tagWrite(t, fake, "#!/bin/sh\nprintf called > '"+marker+"'\n")
				if err := os.Chmod(fake, 0o700); err != nil {
					t.Fatal(err)
				}
				env := tagEnv("LIMENSAFE_RELEASE_TAG=v1.2.3", "LIMENSAFE_MINISIGN_PUB="+filepath.Join(keys, selected+".pub"), "PATH="+tools+string(os.PathListSeparator)+os.Getenv("PATH"))
				out, err := tagRun(dir, env, "make", target)
				if scenario == "pinned" {
					if err != nil {
						t.Fatalf("pinned upload failed: %v %s", err, out)
					}
					if _, err := os.Stat(marker); err != nil {
						t.Fatal("pinned upload never reached fake gh")
					}
				} else {
					if err == nil || !strings.Contains(out, "minisign public blob anchor mismatch") {
						t.Fatalf("replacement not refused by pin: %v %s", err, out)
					}
					if _, err := os.Stat(marker); !os.IsNotExist(err) {
						t.Fatal("pin failure invoked gh")
					}
				}
			})
		}
	}
}
