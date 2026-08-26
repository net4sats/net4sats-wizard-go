package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestScanFailedHeuristic covers the error-string detection used by the
// WiFi scan fallback chain to distinguish real scan results from error
// text emitted by iwinfo/iw on OpenWrt.
func TestScanFailedHeuristic(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"empty output", "", true},
		{"whitespace only", "   \n	  ", true},
		{"command not found", "iwinfo: command not found", true},
		{"No such device", "No such device: wlan0", true},
		{"No such wireless device", "No such wireless device: phy0-ap0", true},
		{"Operation not supported", "Operation not supported", true},
		{"Operation not permitted", "Operation not permitted", true},
		{"Device or resource busy", "Device or resource busy", true},
		{"real scan output", "Cell 01 - Address: AA:BB:CC:DD:EE:FF\n  ESSID: \"MyWiFi\"\n  Signal: -45 dBm", false},
		{"iwinfo scan with SSIDs", "phy0-ap0   ESSID: \"TollGate-F794\"\n          Cell 01 - Address: ...\n          ESSID: \"Net4Sats\"", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := scanFailedHeuristic(tc.in); got != tc.want {
				t.Errorf("scanFailedHeuristic(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
// TestAllRadiosUp covers the `ubus call network.wireless status` parser used
// by enableWifiAndWait (WiFi scan pre-flight): every radio must report up.
func TestAllRadiosUp(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{
			"all radios up",
			`{"radio0":{"up":true,"pending":false,"disabled":false},"radio1":{"up":true,"pending":false,"disabled":false}}`,
			true,
		},
		{
			"single radio up",
			`{"radio0":{"up":true,"pending":false,"disabled":false,"interfaces":[{}]}}`,
			true,
		},
		{
			"one radio down — keep polling",
			`{"radio0":{"up":true},"radio1":{"up":false,"pending":true}}`,
			false,
		},
		{"radio still pending", `{"radio0":{"up":false,"pending":true}}`, false},
		{"empty object — keep polling", `{}`, false},
		{"empty output", "", false},
		{"ubus error text", "Failed to parse message", false},
		{"array not object", `["radio0"]`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := allRadiosUp(tc.in); got != tc.want {
				t.Errorf("allRadiosUp(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestIfaceUp covers the `ubus call network.interface.wwan status` parser
// used by configureSTA — the only reliable STA verification (grep-based
// checks against iwinfo / network.wireless status are false positives).
func TestIfaceUp(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{
			"wwan up with dhcp address",
			`{"up":true,"pending":false,"available":true,"autostart":true,"uptime":42,"l3_device":"phy1-sta0","proto":"dhcp","updated":["addresses"],"route":[],"dns-server":[],"data":{}}`,
			true,
		},
		{
			"wwan up minimal",
			`{"up":true,"pending":false}`,
			true,
		},
		{
			"wwan down",
			`{"up":false,"pending":true,"available":true}`,
			false,
		},
		{"missing up field", `{"pending":false}`, false},
		{"empty output", "", false},
		{"ubus not found", "Object not found", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ifaceUp(tc.in); got != tc.want {
				t.Errorf("ifaceUp(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestHTTPGetFile exercises the laptop-side download helper used as the
// PRIMARY package install path: it must follow redirects (GitHub release
// URLs redirect to a CDN) and surface non-200s as errors.
func TestHTTPGetFile(t *testing.T) {
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("FAKE_IPK_BYTES"))
	}))
	defer final.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL, http.StatusFound)
	}))
	defer redirector.Close()

	data, err := httpGetFile(redirector.URL)
	if err != nil {
		t.Fatalf("httpGetFile(redirecting URL): unexpected error: %v", err)
	}
	if string(data) != "FAKE_IPK_BYTES" {
		t.Errorf("httpGetFile data = %q, want %q", data, "FAKE_IPK_BYTES")
	}

	gone := httptest.NewServer(http.NotFoundHandler())
	defer gone.Close()
	if _, err := httpGetFile(gone.URL); err == nil {
		t.Error("httpGetFile(404) should return an error")
	}
}

// TestStaSetupScript pins the safety properties of the STA setup script:
// rollback snapshot taken, existing STAs disabled (not deleted), single
// commit, and idempotent re-run support.
func TestStaSetupScript(t *testing.T) {
	s := staSetupScript("Net4Sats-Field", "correct horse")
	for _, want := range []string{
		// Snapshot for rollback BEFORE any change.
		"cp /etc/config/wireless /tmp/wireless.pre-net4sats",
		// Dual-STA guard: existing STAs on the target radio are disabled…
		"uci -q set wireless.$s.disabled='1'",
		// …not deleted…
		"rm /etc/config/wireless",
		// …and the new uplink is explicitly enabled (idempotent re-run).
		"wireless.net4sats_uplink.disabled='0'",
		// Single commit pair before the marker.
		"uci commit wireless",
		"uci commit network",
		// Success marker parsed by configureSTA.
		"STA_CFG_OK target=$target",
		// Operator-supplied credentials land in the right sections —
		// base64-carried and decoded in-shell (injection-safe; payload
		// proofs in security_hardening_test.go).
		`uci set wireless.net4sats_uplink.ssid="$sta_ssid"`,
		`uci set wireless.net4sats_uplink.key="$sta_key"`,
	} {
		if want == "rm /etc/config/wireless" {
			if strings.Contains(s, want) {
				t.Errorf("staSetupScript must NOT contain %q (config is snapshotted+disabled, never deleted)", want)
			}
			continue
		}
		if !strings.Contains(s, want) {
			t.Errorf("staSetupScript missing %q", want)
		}
	}
	// Exactly ONE commit per config file (no partial applies).
	if got := strings.Count(s, "uci commit"); got != 2 {
		t.Errorf("staSetupScript has %d `uci commit` calls, want exactly 2 (wireless+network)", got)
	}
	// Credentials round-trip exactly through the base64 carriers.
	carriers := decodedCarriers(t, s)
	wantCreds := []string{"Net4Sats-Field", "correct horse"}
	if len(carriers) != len(wantCreds) || carriers[0] != wantCreds[0] || carriers[1] != wantCreds[1] {
		t.Errorf("staSetupScript carriers decode to %q, want %q", carriers, wantCreds)
	}
}
