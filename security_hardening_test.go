package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// attackPayloads is the canonical audit payload set for the wizard's
// root shell-injection sinks (deploy.go). Each one splices a command out
// of a single-quoted shell string (or out of an unquoted jq --arg).
var attackPayloads = []string{
	`x' ; touch /tmp/PWN ; #`,
	"x';touch${IFS}/tmp/PWN;#@y.z",
	"https://m;touch${IFS}/tmp/PWN;#",
}

// carrierRe matches the base64 carriers embedded in remote commands:
//
//	echo <b64> | base64 -d
//
// Every operator-supplied value that must reach the router as a shell
// value travels through such a carrier (decoded on the router into a shell
// variable), so the command string itself never contains raw user input.
var carrierRe = regexp.MustCompile(`echo ([A-Za-z0-9+/]+={0,2}) \| base64 -d`)

// stripCarriers removes the base64 carrier spans from s, leaving the
// command scaffold. Injection probes must survive this stripping: if any
// payload fragment appears in the scaffold, it would execute on the router.
func stripCarriers(s string) string {
	return carrierRe.ReplaceAllString(s, "<carrier>")
}

// decodedCarriers returns the decoded values of every base64 carrier in s.
// The decoded values are what the router shell receives — they must equal
// the operator's input exactly (round-trip).
func decodedCarriers(t *testing.T, s string) []string {
	t.Helper()
	matches := carrierRe.FindAllStringSubmatch(s, -1)
	values := make([]string, 0, len(matches))
	for i, m := range matches {
		v, err := base64.StdEncoding.DecodeString(m[1])
		if err != nil {
			t.Fatalf("carrier %d does not decode (invalid base64 %q)", i, m[1])
		}
		values = append(values, string(v))
	}
	return values
}

// TestRemoteCommandsNeverInterpolatePayloads is the malformed-input probe
// for all five sinks: whatever the operator submits as password, SSID,
// WiFi key, Lightning address, or mint URL, the constructed remote
// command/script must (a) not contain the raw payload anywhere, (b) not
// contain any command fragments once the base64 carriers are stripped,
// and (c) for the stdin-carried sinks (jq --rawfile), be completely
// independent of the payload.
func TestRemoteCommandsNeverInterpolatePayloads(t *testing.T) {
	type builder struct {
		name  string
		build func(payload string) string
		// static means the command must be byte-identical no matter the
		// payload (the value travels over SSH stdin, not in the string).
		static bool
	}
	builders := []builder{
		{"passwd-sink", func(p string) string { return passwdCommand(p) }, false},
		{"sta-ssid-sink", func(p string) string { return staSetupScript(p, "benign-key") }, false},
		{"sta-key-sink", func(p string) string { return staSetupScript("benign-ssid", p) }, false},
		{"lnurl-sink", func(string) string { return lnIdentCommand() }, true},
		{"mint-sink", func(string) string { return cfgConfigCommand(0, "0.9000", "0.1000") }, true},
	}

	for _, b := range builders {
		for i, payload := range attackPayloads {
			t.Run(fmt.Sprintf("%s/payload-%d", b.name, i), func(t *testing.T) {
				cmd := b.build(payload)

				// (a) raw payload must not appear anywhere in the command.
				if strings.Contains(cmd, payload) {
					t.Errorf("%s: command contains raw payload %q:\n%s", b.name, payload, cmd)
				}

				// (b) once the base64 carriers are stripped, no payload
				// fragment may remain — anything left would execute.
				scaffold := stripCarriers(cmd)
				for _, forbidden := range []string{"touch", "${IFS}", "PWN"} {
					if strings.Contains(scaffold, forbidden) {
						t.Errorf("%s: scaffold contains %q after stripping carriers:\n%s", b.name, forbidden, scaffold)
					}
				}

				// (c) static commands must not depend on the payload at all.
				if b.static && cmd != b.build("other-value") {
					t.Errorf("%s: command varies with payload (must be static, value goes over stdin):\n%s", b.name, cmd)
				}
			})
		}
	}
}

// TestVulnerableConstructionsAreGone pins the exact old interpolation
// patterns as absent, so none of the five sinks can quietly return.
func TestVulnerableConstructionsAreGone(t *testing.T) {
	checks := []struct {
		name string
		cmd  string
		want []string
	}{
		{"passwd", passwdCommand("x"), []string{"base64 -d", "passwd root"}},
		{"sta", staSetupScript("x", "y"), []string{`ssid="$sta_ssid"`, `key="$sta_key"`}},
		{"lnurl", lnIdentCommand(), []string{"cat > /tmp/lnaddr.val", "--rawfile la /tmp/lnaddr.val"}},
		{"mint", cfgConfigCommand(0, "0.9000", "0.1000"), []string{"cat > /tmp/mint.val", "--rawfile mu /tmp/mint.val"}},
	}
	for _, c := range checks {
		for _, want := range c.want {
			if !strings.Contains(c.cmd, want) {
				t.Errorf("%s: command missing safe-passing marker %q:\n%s", c.name, want, c.cmd)
			}
		}
	}

	if strings.Contains(passwdCommand("x"), "--arg") ||
		strings.Contains(lnIdentCommand(), "--arg la") ||
		strings.Contains(cfgConfigCommand(0, "0.9000", "0.1000"), "--arg mu") {
		t.Errorf("old jq --arg / echo -e interpolation present in commands")
	}
}

// TestBenignValuesRoundTripThroughCarriers proves the fix preserves
// semantics: the base64 carriers embedded in the remote commands decode,
// on the router, to exactly the values the operator typed.
func TestBenignValuesRoundTripThroughCarriers(t *testing.T) {
	// Password sink: one carrier, decoded == the password.
	if got := decodedCarriers(t, passwdCommand("hunter2!")); len(got) != 1 || got[0] != "hunter2!" {
		t.Errorf("passwd carriers = %q, want exactly [hunter2!]", got)
	}

	// STA sinks: two carriers, in order: ssid then wifi key.
	got := decodedCarriers(t, staSetupScript("TollGate", "hunter2!"))
	want := []string{"TollGate", "hunter2!"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("sta carriers = %q, want %q", got, want)
	}
}

// TestPasswdCommandInertUnderSh is the executable bite-proof: it runs the
// constructed remote command under a real `sh -c` with a stub `passwd`
// (mirroring the router) and asserts (1) no marker file is created and
// (2) passwd receives exactly the payload twice — the operator's password
// still lands verbatim, it just can no longer escape the quotes.
func TestPasswdCommandInertUnderSh(t *testing.T) {
	shPath, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not in PATH")
	}
	if _, err := exec.LookPath("base64"); err != nil {
		t.Skip("base64 not in PATH")
	}

	dir := t.TempDir()
	marker := filepath.Join(dir, "PWNED")
	capture := filepath.Join(dir, "passwd-stdin.txt")
	stubDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(stubDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stub := "#!/bin/sh\ncat > \"$T7_PWSTDIN\"\necho \"password changed\"\n"
	if err := os.WriteFile(filepath.Join(stubDir, "passwd"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}

	payload := "x' ; touch " + marker + " ; #" // audit payload, marker in the test sandbox
	c := exec.Command(shPath, "-c", passwdCommand(payload))
	c.Env = append(os.Environ(),
		"PATH="+stubDir+":"+os.Getenv("PATH"),
		"T7_PWSTDIN="+capture,
	)
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("sh -c: %v\n%s", err, out)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("marker file was created — payload escaped the construction:\n%s", out)
	}
	stdinGot, _ := os.ReadFile(capture)
	if want := payload + "\n" + payload + "\n"; string(stdinGot) != want {
		t.Errorf("passwd stdin = %q, want %q (password must round-trip verbatim)", stdinGot, want)
	}
}

// TestStaCarriersDecodeUnderSh executes the decode prologue of the STA
// setup script under a real shell and asserts the decoded shell variables
// equal the operator's SSID/key (BusyBox ash supports the same syntax:
// $(…) command substitution and base64 -d).
func TestStaCarriersDecodeUnderSh(t *testing.T) {
	shPath, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not in PATH")
	}
	if _, err := exec.LookPath("base64"); err != nil {
		t.Skip("base64 not in PATH")
	}

	script := staSetupScript("TollGate", "hunter2!")
	// Run only the decode prologue (everything before `target=radio0`).
	prologue, _, ok := strings.Cut(script, "target=radio0")
	if !ok {
		t.Fatal("staSetupScript: cannot locate decode prologue")
	}
	c := exec.Command(shPath, "-c", prologue+`printf '%s\n%s' "$sta_ssid" "$sta_key"`)
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("sh -c decode prologue: %v\n%s", err, out)
	}
	if got, want := strings.TrimSpace(string(out)), "TollGate\nhunter2!"; got != want {
		t.Errorf("decoded ssid/key = %q, want %q", got, want)
	}
}

// TestLnIdentCommandJQRoundTrip runs the exact jq program with --rawfile
// (the same consumption the router performs) against a sample
// identities.json: the value — including the attack payload — lands as
// inert JSON data, byte-for-byte.
func TestLnIdentCommandJQRoundTrip(t *testing.T) {
	jqPath, err := exec.LookPath("jq")
	if err != nil {
		t.Skip("jq not in PATH — --rawfile round-trip needs a local jq")
	}

	sample := `{"public_identities":[{"name":"owner","lightning_address":"old@example.com"},{"name":"developer","lightning_address":"dev@example.com"}]}`
	for _, value := range append([]string{"a@b.co"}, attackPayloads...) {
		dir := t.TempDir()
		valFile := filepath.Join(dir, "lnaddr.val")
		if err := os.WriteFile(valFile, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
		src := filepath.Join(dir, "identities.json")
		if err := os.WriteFile(src, []byte(sample), 0o600); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(jqPath, "--rawfile", "la", valFile, lnJQProgram, src).CombinedOutput()
		if err != nil {
			t.Fatalf("jq --rawfile: %v\n%s", err, out)
		}
		var doc struct {
			PublicIdentities []struct {
				Name             string `json:"name"`
				LightningAddress string `json:"lightning_address"`
			} `json:"public_identities"`
		}
		if err := json.Unmarshal(out, &doc); err != nil {
			t.Fatalf("jq output not JSON: %v\n%s", err, out)
		}
		var owner string
		for _, id := range doc.PublicIdentities {
			if id.Name == "owner" {
				owner = id.LightningAddress
			}
		}
		if owner != value {
			t.Errorf("owner lightning_address = %q, want exact round-trip %q", owner, value)
		}
	}
}

// mintDoc is the slice of config.json the mint round-trip test inspects.
type mintDoc struct {
	Margin        int `json:"margin"`
	AcceptedMints []struct {
		URL string `json:"url"`
	} `json:"accepted_mints"`
}

// TestCfgConfigCommandJQRoundTrip runs the config jq program with the
// mint value in a --rawfile temp file: the mint (attack payload included)
// is consumed as inert data; margin math and default-mint additions are
// unchanged; an empty mint no longer kills the whole config update (the
// unquoted `--arg mu` used to break that case).
func TestCfgConfigCommandJQRoundTrip(t *testing.T) {
	jqPath, err := exec.LookPath("jq")
	if err != nil {
		t.Skip("jq not in PATH — --rawfile round-trip needs a local jq")
	}

	sample := `{"margin":0,"profit_share":[{"identity":"owner","factor":0.9},{"identity":"developer","factor":0.1}],"accepted_mints":[{"url":"https://existing.example/mint","min_balance":0}]}`

	run := func(t *testing.T, mint string) mintDoc {
		t.Helper()
		dir := t.TempDir()
		valFile := filepath.Join(dir, "mint.val")
		if err := os.WriteFile(valFile, []byte(mint), 0o600); err != nil {
			t.Fatal(err)
		}
		src := filepath.Join(dir, "config.json")
		if err := os.WriteFile(src, []byte(sample), 0o600); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(jqPath,
			"--argjson", "m", "5",
			"--argjson", "of", "0.8500",
			"--argjson", "df", "0.1500",
			"--argjson", "dm", defaultMints,
			"--rawfile", "mu", valFile,
			cfgJQProgram, src).CombinedOutput()
		if err != nil {
			t.Fatalf("jq: %v\n%s", err, out)
		}
		var doc mintDoc
		if err := json.Unmarshal(out, &doc); err != nil {
			t.Fatalf("jq output not JSON: %v\n%s", err, out)
		}
		return doc
	}

	hasMint := func(doc mintDoc, url string) bool {
		for _, m := range doc.AcceptedMints {
			if m.URL == url {
				return true
			}
		}
		return false
	}

	t.Run("benign mint", func(t *testing.T) {
		doc := run(t, "https://mint.example.com")
		if doc.Margin != 5 {
			t.Errorf("margin = %d, want 5 (host-side math unchanged)", doc.Margin)
		}
		if !hasMint(doc, "https://mint.example.com") {
			t.Error("chosen mint not added")
		}
		var chosen int
		for _, m := range doc.AcceptedMints {
			if m.URL == "https://mint.example.com" {
				chosen++
			}
		}
		if chosen != 1 {
			t.Errorf("chosen mint appears %d times, want exactly 1", chosen)
		}
		if !hasMint(doc, "https://mint.coinos.io") || !hasMint(doc, "https://testnut.cashu.space") {
			t.Error("default mints not added")
		}
		if !hasMint(doc, "https://existing.example/mint") {
			t.Error("pre-existing mint dropped")
		}
	})

	t.Run("attack payload mint is inert data", func(t *testing.T) {
		doc := run(t, attackPayloads[1])
		if !hasMint(doc, attackPayloads[1]) {
			t.Error("payload mint not stored verbatim as url data")
		}
	})

	t.Run("empty mint still updates config", func(t *testing.T) {
		doc := run(t, "")
		if doc.Margin != 5 {
			t.Errorf("margin = %d, want 5 — empty mint must not break the update (jq program gates on $mu != \"\")", doc.Margin)
		}
		if len(doc.AcceptedMints) == 0 {
			t.Error("default mints not added on empty mint")
		}
	})
}

// TestListenAddress pins the loopback default and the WIZARD_BIND escape
// hatch for operators who deliberately expose the wizard on the LAN.
func TestListenAddress(t *testing.T) {
	if got := listenAddress(); got != "127.0.0.1:8099" {
		t.Errorf("listenAddress() default = %q, want 127.0.0.1:8099 (loopback-only)", got)
	}
	t.Setenv("WIZARD_BIND", "0.0.0.0:8099")
	if got := listenAddress(); got != "0.0.0.0:8099" {
		t.Errorf("listenAddress() with WIZARD_BIND = %q, want override honored", got)
	}
	t.Setenv("WIZARD_BIND", "   ")
	if got := listenAddress(); got != "127.0.0.1:8099" {
		t.Errorf("listenAddress() with blank WIZARD_BIND = %q, want default", got)
	}
}

// TestCORSMiddleware pins the localhost-only CORS allowlist: the wizard
// drives a root SSH session, so no other origin may read its responses.
func TestCORSMiddleware(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := corsMiddleware(inner)

	cases := []struct {
		name     string
		origin   string
		wantACAO string // "" = header must be absent
	}{
		{"foreign origin", "http://evil.example", ""},
		{"localhost allowed", "http://localhost:8099", "http://localhost:8099"},
		{"loopback-ip allowed", "http://127.0.0.1:8099", "http://127.0.0.1:8099"},
		{"no origin header", "", ""},
		{"scheme must match", "https://localhost:8099", ""},
		{"suffix trick", "http://localhost:8099.evil.example", ""},
		{"port must match", "http://localhost:8098", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/scan", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			resp := w.Result()

			got := resp.Header.Get("Access-Control-Allow-Origin")
			if tc.wantACAO == "" {
				if got != "" {
					t.Errorf("Origin %q: ACAO = %q, want no header", tc.origin, got)
				}
			} else if got != tc.wantACAO {
				t.Errorf("Origin %q: ACAO = %q, want %q", tc.origin, got, tc.wantACAO)
			}

			// Methods/headers behavior is unchanged for every request.
			if m := resp.Header.Get("Access-Control-Allow-Methods"); m != "GET, POST, OPTIONS" {
				t.Errorf("Access-Control-Allow-Methods = %q, want unchanged", m)
			}
			if hd := resp.Header.Get("Access-Control-Allow-Headers"); hd != "Content-Type" {
				t.Errorf("Access-Control-Allow-Headers = %q, want unchanged", hd)
			}
		})
	}

	// OPTIONS preflight from an allowlisted origin still short-circuits 204.
	req := httptest.NewRequest("OPTIONS", "/api/deploy", nil)
	req.Header.Set("Origin", "http://localhost:8099")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusNoContent {
		t.Errorf("OPTIONS status = %d, want 204", w.Code)
	}
}

// TestValidLightningAddressUnchangedOnBenign re-asserts the validation
// gate accepts the benign Lightning targets used across these tests —
// the hardening moved the defense into command construction, not into
// tighter validation.
func TestValidLightningAddressUnchangedOnBenign(t *testing.T) {
	for _, benign := range []string{"a@b.co", "you@wallet.app", "lnurl1dp68gurn8ghj7um5wfnz7rrfh"} {
		if !validLightningAddress(benign) {
			t.Errorf("validLightningAddress(%q) = false, want true (unchanged)", benign)
		}
	}
}
