package main

import "testing"

func TestValidLightningAddress(t *testing.T) {
	cases := map[string]bool{
		// valid — email-shaped Lightning addresses
		"you@wallet.app":               true,
		"alice@walletofsatoshi.com":    true,
		"bob@sub.domain.co":            true,
		"node1@42.42.42.42.xdd":        true,
		"  padded@wallet.app  ":        true, // surrounding whitespace trimmed
		// invalid — required field, must be rejected
		"":                             false, // empty (the MVP-required gate)
		"   ":                          false, // whitespace only
		"noatsign":                     false,
		"@nodomain.com":                false, // empty localpart
		"nolocalpart@":                 false, // empty domain
		"user@domain":                  false, // no TLD / dot
		"has space@domain.com":         false, // space in localpart
		"user@domain.c om":             false, // space in domain
		"user@@domain.com":             false, // double @
	}
	for addr, want := range cases {
		got := validLightningAddress(addr)
		if got != want {
			t.Errorf("validLightningAddress(%q) = %v, want %v", addr, got, want)
		}
	}
}
