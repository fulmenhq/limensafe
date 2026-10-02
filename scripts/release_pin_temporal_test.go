//go:build releasecrypto

package scripts

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseTagPinnedHistoricalValidity(t *testing.T) {
	script, err := filepath.Abs("release-pin.py")
	if err != nil {
		t.Fatal(err)
	}
	newHome := func(t *testing.T) string {
		h, err := os.MkdirTemp("/tmp", "lim-history-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, _ = tagRun("", tagEnv(), "gpgconf", "--homedir", h, "--kill", "all")
			if err := os.RemoveAll(h); err != nil {
				t.Error(err)
			}
		})
		return h
	}
	home := newHome(t)
	gpg := func(h, clock string, args ...string) string {
		return tagMust(t, "", tagEnv(), "gpg", append([]string{"--homedir", h, "--batch", "--yes", "--pinentry-mode", "loopback", "--passphrase", "", "--faked-system-time", clock}, args...)...)
	}
	gpg(home, "20200101T000000", "--quick-gen-key", "History Fixture <history@example.test>", "ed25519", "cert", "never")
	keys := func(h string) []string {
		var keys []string
		for _, line := range strings.Split(gpg(h, "20200101T000000", "--with-colons", "--list-keys"), "\n") {
			f := strings.Split(line, ":")
			if f[0] == "fpr" {
				keys = append(keys, f[9])
			}
		}
		return keys
	}
	primary := keys(home)[0]
	gpg(home, "20200101T000000", "--quick-add-key", primary, "ed25519", "sign", "never")
	gpg(home, "20200101T000000", "--quick-add-key", primary, "cv25519", "encr", "never")
	signer, encryption := keys(home)[1], keys(home)[2]
	secret := gpg(home, "20200101T000000", "--armor", "--export-secret-keys", primary)
	for _, name := range []string{"signer expired after signature", "primary expired after signature", "expired at signature", "expired encryption", "revoked encryption", "retired after signature", "superseded after signature", "compromise after signature", "primary compromise after signature", "primary retired after signature", "revoked before signature", "unknown revocation reason", "revoked UID certification"} {
		t.Run(name, func(t *testing.T) {
			pinHome := newHome(t)
			input := func(clock, body string, args ...string) {
				cmd := exec.Command("gpg", append([]string{"--homedir", pinHome, "--batch", "--yes", "--pinentry-mode", "loopback", "--passphrase", "", "--faked-system-time", clock}, args...)...)
				cmd.Env, cmd.Stdin = tagEnv(), strings.NewReader(body)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("fixture: %v %s", err, out)
				}
			}
			input("20200101T000000", secret, "--import")
			clock := "20200101T010000"
			want := true
			switch name {
			case "signer expired after signature", "expired at signature":
				gpg(pinHome, "20200101T000000", "--quick-set-expire", primary, "2020-01-02", signer)
				if name == "expired at signature" {
					clock, want = "20200103T000000", false
				}
			case "primary expired after signature":
				gpg(pinHome, "20200101T000000", "--quick-set-expire", primary, "2020-01-02")
			case "expired encryption":
				gpg(pinHome, "20200101T000000", "--quick-set-expire", primary, "2020-01-02", encryption)
			case "revoked UID certification":
				uid := "Retired UID <retired@example.test>"
				gpg(pinHome, "20200101T000000", "--quick-add-uid", primary, uid)
				gpg(pinHome, "20200103T000000", "--quick-revoke-uid", primary, uid)
				listing := gpg(pinHome, "20200104T000000", "--with-colons", "--list-options", "show-sig-subpackets=29", "--check-sigs")
				if !strings.Contains(listing, ":30x,20:") ||
					(!strings.Contains(listing, "spk:29:1:1:%20") && !strings.Contains(listing, "spk:29:1:1: \n")) {
					t.Fatalf("UID revocation fixture missing class/reason: %s", listing)
				}
			default:
				reason, revTime := "3", "20200103T000000"
				if name == "superseded after signature" {
					reason = "2"
				}
				if name == "compromise after signature" || name == "primary compromise after signature" {
					reason, want = "1", false
				}
				if name == "unknown revocation reason" {
					reason, want = "0", false
				}
				if name == "revoked before signature" {
					revTime, want = "20200101T003000", false
				}
				selectKey, expectedTarget, expectedClass := "key 1\n", signer, "28"
				if strings.HasPrefix(name, "primary ") {
					selectKey, expectedTarget, expectedClass = "", primary, "20"
				}
				if name == "revoked encryption" {
					selectKey, expectedTarget = "key 2\n", encryption
				}
				input(revTime, selectKey+"revkey\ny\n"+reason+"\n\ny\nsave\n", "--command-fd", "0", "--edit-key", primary)
				// Assert the emitted OpenPGP target/class/reason, not UI menu
				// numbering. A mistaken fixture must fail before policy checks.
				listing := gpg(pinHome, "20200104T000000", "--with-colons", "--list-options", "show-sig-subpackets=29", "--check-sigs")
				current, matched := "", false
				actualReason := map[string]string{"0": "00", "1": "02", "2": "01", "3": "03"}[reason]
				for _, line := range strings.Split(listing, "\n") {
					f := strings.Split(line, ":")
					if f[0] == "fpr" {
						current = f[9]
					}
					if f[0] == "rev" {
						if current != expectedTarget || f[1] != "!" || !strings.HasPrefix(f[10], expectedClass) {
							t.Fatalf("wrong revocation target/class: %s", line)
						}
						matched = true
					}
					if f[0] == "spk" && f[1] == "29" && f[4] != "%"+actualReason {
						t.Fatalf("wrong emitted revocation reason: %s", line)
					}
				}
				if !matched {
					t.Fatal("revocation packet missing")
				}
			}
			dir := t.TempDir()
			git := func(args ...string) string { return tagMust(t, dir, tagEnv(), "git", args...) }
			git("init", "-q", "-b", "main")
			git("config", "user.name", "History Fixture")
			git("config", "user.email", "history@example.test")
			if err := os.MkdirAll(filepath.Join(dir, "docs/security"), 0o700); err != nil {
				t.Fatal(err)
			}
			tagWrite(t, filepath.Join(dir, "docs/security/release-signing-keys.asc"), gpg(pinHome, "20200104T000000", "--armor", "--export", primary)+"\n")
			if name == "revoked encryption" {
				// Export maintenance must reject the revoked encryption packet,
				// although historical tag verification ignores its revocation.
				blob := make([]byte, 42)
				copy(blob, "Ed")
				mini := filepath.Join(dir, "fixture.pub")
				tagWrite(t, mini, "untrusted comment: fixture\n"+base64.StdEncoding.EncodeToString(blob)+"\n")
				mini, err = filepath.EvalSymlinks(mini)
				if err != nil {
					t.Fatal(err)
				}
				env := tagEnv("LIMENSAFE_GPG_HOMEDIR="+pinHome,
					"LIMENSAFE_GPG_PRIMARY_FINGERPRINT="+primary, "LIMENSAFE_PGP_KEY_ID="+signer+"!", "LIMENSAFE_MINISIGN_PUB="+mini)
				out, err := tagRun(dir, env, "python3", script, "export")
				if err == nil || !strings.Contains(out, "revoked key in public export") {
					t.Fatalf("maintenance did not refuse encryption revocation: %v %s", err, out)
				}
			}
			// Derive anchors from actual fixture public bytes. Structural-only
			// inspection is necessary to construct intentionally historical pins;
			// the public maintenance command must reject these expired pins now.
			derive := `import importlib.util,sys,tempfile,pathlib
spec=importlib.util.spec_from_file_location("pin",sys.argv[1]);m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
with tempfile.TemporaryDirectory() as h:
 p,s=m.inspect(h,pathlib.Path(m.PIN).read_bytes(),structural=True)
 text,nd=m.anchors(p,"0"*64,"0"*16)
 m.install(m.TEXT,text);m.install(m.JSON,nd);m.install(m.SIGNER,(s+"\n").encode())
`
			tagMust(t, dir, tagEnv("PYTHONDONTWRITEBYTECODE=1"), "python3", "-c", derive, script)
			tagWrite(t, filepath.Join(dir, "VERSION"), "1.2.3\n")
			git("add", ".")
			git("commit", "-q", "-m", "historical fixture")
			wrapper := filepath.Join(home, "sign-"+strings.ReplaceAll(name, " ", "-"))
			tagWrite(t, wrapper, "#!/bin/sh\nexec gpg --faked-system-time "+clock+" \"$@\"\n")
			if err := os.Chmod(wrapper, 0o700); err != nil {
				t.Fatal(err)
			}
			// The signing keyring retains its original never-expiring binding.
			// The pin contains a verified expiry update; this permits a genuine
			// cryptographic negative signed after the pinned expiry.
			tagMust(t, dir, tagEnv("GNUPGHOME="+home), "git", "-c", "gpg.program="+wrapper, "tag", "-s", "-u", signer+"!", "--cleanup=verbatim", "v1.2.3", "-m", "Release v1.2.3\n")
			out, err := tagRun(dir, tagEnv("LIMENSAFE_RELEASE_TAG=v1.2.3"), "python3", script, "verify")
			if (err == nil) != want {
				t.Fatalf("historical result want=%v err=%v %s", want, err, out)
			}
			if !want {
				reason := "compromise or unknown revocation reason"
				if name == "expired at signature" {
					reason = "key expired at authorization or signature time"
				}
				if name == "revoked before signature" {
					reason = "key revoked at signature time"
				}
				if !strings.Contains(out, reason) {
					t.Fatalf("negative did not reach intended policy %q: %s", reason, out)
				}
			}
			if name == "primary retired after signature" {
				// Fault-inject absent revocation metadata at GPG's machine-output
				// boundary while retaining its revoked primary/subkey statuses.
				missing := `import importlib.util,sys
spec=importlib.util.spec_from_file_location("pin",sys.argv[1]);m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
original=m.gpg
def without_revocation(home,*args,**kwargs):
    data=original(home,*args,**kwargs)
    if "--check-sigs" in args:
        data=b"\n".join(line for line in data.splitlines() if not line.startswith((b"rev:",b"spk:")))+b"\n"
    return data
m.gpg=without_revocation
try: m.verify()
except ValueError as error:
    assert str(error)=="revocation metadata missing",str(error)
else: raise AssertionError("missing revocation metadata accepted")
`
				tagMust(t, dir, tagEnv("LIMENSAFE_RELEASE_TAG=v1.2.3", "PYTHONDONTWRITEBYTECODE=1"), "python3", "-c", missing, script)
				badClass := `import importlib.util,sys
spec=importlib.util.spec_from_file_location("pin",sys.argv[1]);m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
original=m.gpg
def invalid_class(home,*args,**kwargs):
    data=original(home,*args,**kwargs)
    if "--check-sigs" in args:
        rows=[]
        for line in data.decode().splitlines():
            fields=line.split(":")
            if fields[0]=="rev": fields[10]=sys.argv[2]
            rows.append(":".join(fields))
        return ("\n".join(rows)+"\n").encode()
    return data
m.gpg=invalid_class
try: m.verify()
except ValueError as error:
    assert str(error)=="key revocation class/target mismatch",str(error)
else: raise AssertionError("invalid key revocation class accepted")
`
				for _, class := range []string{"", "28x,03", "ffx,03"} {
					tagMust(t, dir, tagEnv("LIMENSAFE_RELEASE_TAG=v1.2.3", "PYTHONDONTWRITEBYTECODE=1"), "python3", "-c", badClass, script, class)
				}
			}
			if name == "primary retired after signature" || name == "primary compromise after signature" {
				// Exercise the alternate GPG machine-output ordering explicitly:
				// pub -> rev -> reason -> fpr, rather than pub -> fpr -> rev.
				ordered := `import importlib.util,sys
spec=importlib.util.spec_from_file_location("pin",sys.argv[1]);m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
original=m.gpg
def before_fpr(home,*args,**kwargs):
    data=original(home,*args,**kwargs)
    if "--check-sigs" not in args: return data
    rows=data.splitlines()
    start=next(i for i,r in enumerate(rows) if r.startswith(b"pub:"))
    stop=next(i for i in range(start+1,len(rows)) if rows[i].startswith((b"uid:",b"sub:")))
    block=rows[start:stop]
    fpr=next(r for r in block if r.startswith(b"fpr:")); block.remove(fpr)
    rev=next(i for i,r in enumerate(block) if r.startswith(b"rev:"))
    position=rev+1
    while position<len(block) and block[position].startswith(b"spk:"): position+=1
    block.insert(position,fpr)
    return b"\n".join(rows[:start]+block+rows[stop:])+b"\n"
m.gpg=before_fpr
try: m.verify()
except ValueError as error:
    assert sys.argv[2]=="compromise" and str(error)=="compromise or unknown revocation reason",str(error)
else: assert sys.argv[2]=="retired","compromise accepted"
`
				policy := "retired"
				if name == "primary compromise after signature" {
					policy = "compromise"
				}
				tagMust(t, dir, tagEnv("LIMENSAFE_RELEASE_TAG=v1.2.3", "PYTHONDONTWRITEBYTECODE=1"), "python3", "-c", ordered, script, policy)
			}
		})
	}
}
