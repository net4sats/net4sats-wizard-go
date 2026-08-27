package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// pinHexRe matches a lowercase 64-hex-char sha256 digest.
var pinHexRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// TestArtifactPinsCoverDownloadedURLConstants is the static coverage
// assertion for S-4: every URL constant that reaches httpGetFile on the
// install path MUST have a sha256 pin. verifyArtifactPin fails closed on
// unknown URLs, so a missing entry here would break (not weaken) deploys —
// this test catches that before a release is cut.
func TestArtifactPinsCoverDownloadedURLConstants(t *testing.T) {
	// selectedPkgURL in runDeploy is assigned from exactly these two
	// constants (opkg vs apk package managers); both are pinned.
	for _, url := range []string{tollgatePkgURL, tollgatePkgURLApk} {
		pin, ok := artifactPins[url]
		if !ok {
			t.Errorf("artifactPins[%s] = missing — every downloaded install artifact must be pinned", url)
			continue
		}
		if !pinHexRe.MatchString(pin) {
			t.Errorf("artifactPins[%s] = %q — not a 64-hex-char sha256 digest", url, pin)
		}
	}
}

// TestVerifyArtifactPinBenignDownload proves a pristine artifact that went
// through the real httpGetFile download path passes verification.
func TestVerifyArtifactPinBenignDownload(t *testing.T) {
	artifact := []byte("fake but deterministic tollgate-wrt artifact bytes 0123456789")
	sum := sha256.Sum256(artifact)
	pin := hex.EncodeToString(sum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(artifact)
	}))
	defer srv.Close()

	artifactPins[srv.URL+"/pkg.ipk"] = pin
	defer delete(artifactPins, srv.URL+"/pkg.ipk")

	data, err := httpGetFile(srv.URL + "/pkg.ipk")
	if err != nil {
		t.Fatalf("httpGetFile: %v", err)
	}
	if err := verifyArtifactPin(srv.URL+"/pkg.ipk", data); err != nil {
		t.Errorf("benign artifact should pass verification: %v", err)
	}
}

// TestVerifyArtifactPinTamperedDownload is the S-4 tamper test: one byte of
// a downloaded artifact is flipped; verification must return a checksum
// mismatch naming the URL, the expected pin and the actual digest — the
// deploy call path aborts on this error before any SSH push.
func TestVerifyArtifactPinTamperedDownload(t *testing.T) {
	artifact := []byte("fake but deterministic tollgate-wrt artifact bytes 0123456789")
	tampered := append([]byte(nil), artifact...)
	tampered[len(tampered)/2] ^= 0x01 // flip one byte in the middle

	if sha256.Sum256(tampered) == sha256.Sum256(artifact) {
		t.Fatal("byte flip did not change the artifact digest")
	}
	sum := sha256.Sum256(artifact)
	pin := hex.EncodeToString(sum[:])
	gotSum := sha256.Sum256(tampered)
	gotHex := hex.EncodeToString(gotSum[:])

	var serve = artifact
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(serve)
	}))
	defer srv.Close()

	url := srv.URL + "/pkg.ipk"
	artifactPins[url] = pin
	defer delete(artifactPins, url)

	serve = tampered // server now serves the tampered copy
	data, err := httpGetFile(url)
	if err != nil {
		t.Fatalf("httpGetFile: %v", err)
	}
	verr := verifyArtifactPin(url, data)
	if verr == nil {
		t.Fatal("tampered artifact passed verification — deploy would push it to the router")
	}
	for _, want := range []string{"checksum mismatch for ", "aborting deploy", "expected " + pin, "got " + gotHex} {
		if !strings.Contains(verr.Error(), want) {
			t.Errorf("verification error %q missing %q", verr.Error(), want)
		}
	}
}

// TestVerifyArtifactPinUnknownURLFailsClosed proves default-deny: a URL
// without a pin entry is refused even when its bytes would otherwise look
// fine. This is the security posture — an unpinned download must never
// reach the router.
func TestVerifyArtifactPinUnknownURLFailsClosed(t *testing.T) {
	err := verifyArtifactPin("https://evil.example/tollgate-wrt.ipk", []byte("anything"))
	if err == nil {
		t.Fatal("unpinned URL passed verification — default-deny violated")
	}
	if !strings.Contains(err.Error(), "no sha256 pin") {
		t.Errorf("error %q missing 'no sha256 pin'", err.Error())
	}
}

// TestVerifyArtifactPinHex covers the router-side (fallback wget) arm of
// the gate, which compares a hex digest reported by the router's
// sha256sum instead of locally-hashed bytes.
func TestVerifyArtifactPinHex(t *testing.T) {
	url := "https://example.invalid/tollgate-wrt.ipk"
	pin := strings.Repeat("ab", 32)
	artifactPins[url] = pin
	defer delete(artifactPins, url)

	if err := verifyArtifactPinHex(url, strings.ToUpper(pin)); err != nil {
		t.Errorf("matching hex digest (upper case) should pass: %v", err)
	}
	if err := verifyArtifactPinHex(url, strings.Repeat("cd", 32)); err == nil {
		t.Error("mismatched hex digest passed verification")
	} else if !strings.Contains(err.Error(), "checksum mismatch for ") {
		t.Errorf("error %q missing 'checksum mismatch for '", err.Error())
	}
	// Uppercase hex pin (sha256sum emits lowercase; pins are stored
	// lowercase — a uppercase stored pin would still verify via EqualFold).
	artifactPins[url] = strings.ToUpper(pin)
	if err := verifyArtifactPinHex(url, pin); err != nil {
		t.Errorf("case-insensitive hex comparison should pass: %v", err)
	}
	if err := verifyArtifactPinHex("https://evil.example/unpinned.ipk", pin); err == nil {
		t.Error("unpinned URL passed hex verification — default-deny violated")
	}
}

// TestExtractIPKFilenameStrict proves the nodogsplash/jq scrape hardening:
// a filename extracted from an OpenWrt directory listing is accepted only
// if it matches ^<pkg>_[0-9][A-Za-z0-9._-]*\.ipk$ (a shell-safe subset of
// the mandated ^<pkg>_[0-9][^/]*\.ipk$ — the extracted name is
// interpolated into an SSH command string, so metacharacters must be
// rejected too). Anything else that looked like a candidate errors loudly;
// a listing with no candidates is an empty, non-error result.
func TestExtractIPKFilenameStrict(t *testing.T) {
	tests := []struct {
		name    string
		html    string
		pkg     string
		want    string
		wantErr bool
	}{
		{
			name: "real nodogsplash listing entry",
			html: `<a href="nodogsplash_5.0.2-1_aarch64_cortex-a53.ipk">nodogsplash_5.0.2-1...</a>`,
			pkg:  "nodogsplash",
			want: "nodogsplash_5.0.2-1_aarch64_cortex-a53.ipk",
		},
		{
			name: "real jq listing entry",
			html: "<tr><td><a href=\"jq_1.7-1_aarch64_cortex-a53.ipk\">jq_1.7</a></td></tr>",
			pkg:  "jq",
			want: "jq_1.7-1_aarch64_cortex-a53.ipk",
		},
		{
			name:    "path traversal rejected",
			html:    `<a href="nodogsplash_5.0.2_../../etc/passwd.ipk">x</a>`,
			pkg:     "nodogsplash",
			wantErr: true,
		},
		{
			name:    "shell metacharacters rejected",
			html:    `<a href="nodogsplash_1';touch${IFS}/tmp/PWN;'.ipk">x</a>`,
			pkg:     "nodogsplash",
			wantErr: true,
		},
		{
			name:    "non-numeric version start rejected",
			html:    `<a href="nodogsplash_beta_5.0.2.ipk">x</a>`,
			pkg:     "nodogsplash",
			wantErr: true,
		},
		{
			// a different package whose name merely contains the
			// prefix must not be extracted at all
			name: "prefixed package ignored",
			html: `<a href="libnodogsplash_1.0-1_aarch64_cortex-a53.ipk">x</a>`,
			pkg:  "nodogsplash",
			want: "",
		},
		{
			name: ".ipk not at token end ignored",
			html: `<a href="nodogsplash_5.0.2-1.ipk.rpm">x</a>`,
			pkg:  "nodogsplash",
			want: "",
		},
		{
			name: "empty listing",
			html: "",
			pkg:  "nodogsplash",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractIPKFilename(tt.html, tt.pkg)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("extractIPKFilename(%q, %q) = %q, nil — suspicious entry must error loudly", tt.html, tt.pkg, got)
				}
				if got != "" {
					t.Errorf("rejected entry must not yield a filename, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("extractIPKFilename(%q, %q) unexpected error: %v", tt.html, tt.pkg, err)
			}
			if got != tt.want {
				t.Errorf("extractIPKFilename(%q, %q) = %q, want %q", tt.html, tt.pkg, got, tt.want)
			}
		})
	}
}
