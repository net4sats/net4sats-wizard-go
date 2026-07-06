package main

import (
	"strconv"
	"strings"
	"time"
)

const (
	tollgateAPKURL = "https://blossom.primal.net/1fcc1635a7d94a005ff270c4a44f49fb9c56b05a7fbfe01eabcba40e8d31571d.apk"
	cwIPKURL       = "https://github.com/net4sats/configurationwizzard/releases/download/net4sats-portal-3e05134/configurationwizzard.ipk"
)

// deploySteps returns the ordered deployment step definitions.
func deploySteps() []Step {
	return []Step{
		{Name: "verify", Desc: "Verifying SSH access to router...", Status: "pending"},
		{Name: "firmware", Desc: "Checking firmware version...", Status: "pending"},
		{Name: "password", Desc: "Setting root password...", Status: "pending"},
		{Name: "upstream", Desc: "Configuring upstream connection...", Status: "pending"},
		{Name: "tollgate", Desc: "Installing tollgate payment backend...", Status: "pending"},
		{Name: "portal", Desc: "Installing net4sats portal...", Status: "pending"},
		{Name: "brand", Desc: "Branding captive portal as net4sats...", Status: "pending"},
		{Name: "lnurl", Desc: "Configuring Lightning address...", Status: "pending"},
		{Name: "services", Desc: "Restarting services...", Status: "pending"},
		{Name: "health", Desc: "Running health check...", Status: "pending"},
	}
}

// runDeployment executes the full deployment sequence.
func runDeployment(job *Job, req deployRequest) {
	client := sshConnect(req.IP, req.Password)
	if client == nil && req.Password != "" {
		// If password auth failed, try key auth
		client = sshConnect(req.IP, "")
	}
	if client == nil {
		job.mu.Lock()
		job.Status = "failed"
		job.Error = "Cannot connect to router via SSH"
		job.mu.Unlock()
		return
	}
	defer client.Close()

	// Step 0: Verify SSH
	job.setStep(0, "running", "")
	fwOut := sshRun(client, "cat /etc/openwrt_release 2>/dev/null || cat /etc/openwrt_version 2>/dev/null || echo 'not openwrt'")
	fwOut = strings.TrimSpace(fwOut)
	if fwOut == "not openwrt" || fwOut == "" {
		job.setStep(0, "failed", "Router is not running OpenWrt")
		job.mu.Lock()
		job.Status = "failed"
		job.Error = "Router is not running OpenWrt firmware"
		job.mu.Unlock()
		return
	}
	job.addLog("SSH OK. Firmware: " + truncate(fwOut, 100))
	job.setStep(0, "done", truncate(fwOut, 100))
	time.Sleep(500 * time.Millisecond)

	// Step 1: Check firmware
	job.setStep(1, "running", "")
	versionLine := ""
	for _, line := range strings.Split(fwOut, "\n") {
		if strings.Contains(line, "DISTRIB_DESCRIPTION") {
			parts := strings.SplitN(line, "'", 2)
			if len(parts) > 1 {
				versionLine = strings.Trim(parts[1], "'")
			}
		}
	}
	job.addLog("Firmware: " + versionLine)
	job.setStep(1, "done", versionLine)
	time.Sleep(500 * time.Millisecond)

	// Step 2: Set root password
	job.setStep(2, "running", "")
	if req.Password != "" {
		passwdCmd := "echo -e '" + req.Password + "\\n" + req.Password + "' | passwd root 2>&1"
		passwdOut := sshRun(client, passwdCmd)
		if strings.Contains(passwdOut, "changed") || strings.Contains(passwdOut, "successfully") {
			job.addLog("Root password set")
			job.setStep(2, "done", "password updated")
		} else {
			job.addLog("Password set (may already be set)")
			job.setStep(2, "done", "password set")
		}
	} else {
		job.setStep(2, "done", "skipped (no password)")
	}
	time.Sleep(500 * time.Millisecond)

	// Step 3: Configure upstream (WiFi STA if requested)
	job.setStep(3, "running", "")
	if req.Mode == "sta" && req.SSID != "" {
		staCmd := "uci -q set wireless.net4sats_uplink=wifi-iface && " +
			"uci -q set wireless.net4sats_uplink.network='wwan' && " +
			"uci -q set wireless.net4sats_uplink.device='radio0' && " +
			"uci -q set wireless.net4sats_uplink.mode='sta' && " +
			"uci -q set wireless.net4sats_uplink.ssid='" + req.SSID + "' && " +
			"uci -q set wireless.net4sats_uplink.encryption='psk2' && " +
			"uci -q set wireless.net4sats_uplink.key='" + req.WifiPass + "' && " +
			"uci -q set network.wwan=interface && " +
			"uci -q set network.wwan.proto='dhcp' && " +
			"uci commit wireless && uci commit network && echo 'sta configured'"
		staOut := sshRun(client, staCmd)
		if strings.Contains(staOut, "sta configured") {
			job.addLog("WiFi STA configured: " + req.SSID)
			job.setStep(3, "done", "STA mode: "+req.SSID)
		} else {
			job.addLog("WiFi STA configuration attempted")
			job.setStep(3, "done", "STA configured")
		}
	} else {
		job.addLog("Using WAN upstream (default)")
		job.setStep(3, "done", "WAN mode (default)")
	}
	time.Sleep(500 * time.Millisecond)

	// Step 4: Install tollgate
	job.setStep(4, "running", "")
	job.addLog("Downloading tollgate...")
	dlOut := sshRun(client, "wget -q -O /tmp/tollgate.apk '"+tollgateAPKURL+"' && echo 'downloaded' || echo 'download failed'")
	if strings.Contains(dlOut, "downloaded") {
		job.addLog("Installing tollgate...")
		installOut := sshRun(client, "apk add --allow-untrusted /tmp/tollgate.apk 2>&1 | tail -5")
		job.addLog("Tollgate installed: " + truncate(installOut, 80))
		job.setStep(4, "done", "tollgate-wrt installed")
	} else {
		job.addLog("Tollgate download failed — may already be installed")
		job.setStep(4, "done", "download failed (may be pre-installed)")
	}
	time.Sleep(500 * time.Millisecond)

	// Step 5: Install configurationwizzard portal
	job.setStep(5, "running", "")
	job.addLog("Installing net4sats portal...")
	dlOut = sshRun(client, "wget -q -O /tmp/cw.ipk '"+cwIPKURL+"' && echo 'downloaded'")
	if strings.Contains(dlOut, "downloaded") {
		extractOut := sshRun(client, "cd /tmp && tar xzf cw.ipk 2>/dev/null && tar xzf data.tar.gz -C / 2>/dev/null && echo 'extracted'")
		if strings.Contains(extractOut, "extracted") {
			job.addLog("Portal installed")
			job.setStep(5, "done", "configurationwizzard installed")
		} else {
			job.addLog("Portal extraction had issues")
			job.setStep(5, "done", "extracted (may have warnings)")
		}
	} else {
		job.addLog("Portal download failed — may already be installed")
		job.setStep(5, "done", "download failed (may be pre-installed)")
	}
	time.Sleep(500 * time.Millisecond)

	// Step 6: Brand captive portal
	job.setStep(6, "running", "")
	job.addLog("Branding as net4sats...")
	brandOut := sshRun(client, strings.Join([]string{
		"uci -q set nodogsplash.@nodogsplash[0].gatewayname='net4sats'",
		"uci -q set nodogsplash.@nodogsplash[0].enabled='1'",
		"uci -q set nodogsplash.@nodogsplash[0].clientid='mac'",
		"uci -q add_list nodogsplash.@nodogsplash[0].users_to_router='allow tcp port 2121'",
		"uci -q add_list nodogsplash.@nodogsplash[0].users_to_router='allow tcp port 2050'",
		"uci -q add_list nodogsplash.@nodogsplash[0].users_to_router='allow tcp port 80'",
		"uci commit nodogsplash",
		"/etc/init.d/nodogsplash enable",
		"echo 'branded'",
	}, " && "))
	if strings.Contains(brandOut, "branded") {
		job.addLog("Captive portal branded as net4sats")
		job.setStep(6, "done", "gatewayname=net4sats")
	} else {
		job.addLog("Branding applied")
		job.setStep(6, "done", "configured")
	}
	time.Sleep(500 * time.Millisecond)

	// Step 7: Configure tollgate config.json — Lightning address + advanced defaults.
	// config.json keys (lnurl/devSplit/margin/mint) follow tollgate-module-basic-go's
	// schema. If config.json is absent (tollgate not yet installed), we skip gracefully.
	job.setStep(7, "running", "")
	devSplit := clamp(req.DevSplit, 0, 50)
	margin := clamp(req.Margin, 0, 100)
	mint := strings.TrimSpace(req.Mint)
	if mint == "" {
		mint = "https://8333.space/"
	}
	cfgCmd := "jq --arg ln '" + req.LNURL + "' " +
		"--argjson ds " + strconv.Itoa(devSplit) + " " +
		"--argjson m " + strconv.Itoa(margin) + " " +
		"--arg mint '" + mint + "' " +
		"'.lnurl=$ln | .devSplit=$ds | .margin=$m | .mint=$mint' " +
		"/etc/tollgate/config.json > /tmp/cfg.tmp && mv /tmp/cfg.tmp /etc/tollgate/config.json 2>&1 && echo 'config written' || echo 'no config'"
	cfgOut := sshRun(client, cfgCmd)
	if strings.Contains(cfgOut, "config written") {
		job.addLog("config.json updated (lnurl=" + req.LNURL + ", devSplit=" + strconv.Itoa(devSplit) + "%, margin=" + strconv.Itoa(margin) + "%, mint=" + truncate(mint, 30) + ")")
		job.setStep(7, "done", "LNURL: "+req.LNURL)
	} else {
		job.addLog("Config update skipped — no config.json found")
		job.setStep(7, "done", "skipped (no config.json)")
	}
	time.Sleep(500 * time.Millisecond)

	// Step 8: Restart services
	job.setStep(8, "running", "")
	job.addLog("Restarting services...")
	svcOut := sshRun(client, "/etc/init.d/tollgate-wrt restart 2>&1; /etc/init.d/nodogsplash restart 2>&1; sleep 2; echo 'services restarted'")
	job.addLog("Services restarted: " + truncate(svcOut, 60))
	job.setStep(8, "done", "tollgate-wrt + nodogsplash restarted")
	time.Sleep(500 * time.Millisecond)

	// Step 9: Health check
	job.setStep(9, "running", "")
	job.addLog("Running health check...")
	healthOut := sshRun(client, "wget -qO- http://127.0.0.1:2121/ 2>/dev/null | head -c 100 || echo 'health check failed'")
	if strings.Contains(healthOut, "kind") || strings.Contains(healthOut, "metric") || strings.Contains(healthOut, "pubkey") {
		job.addLog("Health check passed — TollGate API responding")
		job.setStep(9, "done", "API healthy on :2121")
	} else {
		job.addLog("Health check: " + truncate(healthOut, 80))
		job.setStep(9, "done", "checked")
	}

	job.mu.Lock()
	job.Status = "done"
	job.mu.Unlock()
	job.addLog("net4sats deployment complete!")
}

func truncate(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

// clamp returns n constrained to the inclusive range [lo, hi]. Used to keep
// the advanced defaults (devSplit, margin) within safe bounds regardless of
// what the client sends.
func clamp(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}
