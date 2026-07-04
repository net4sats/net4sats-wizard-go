package main

import (
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

type RouterInfo struct {
	IP       string `json:"ip"`
	MAC      string `json:"mac"`
	Vendor   string `json:"vendor"`
	Model    string `json:"model"`
	Firmware string `json:"firmware"`
	SSH      bool   `json:"ssh_open"`
	HTTPPort int    `json:"http_port,omitempty"`
}

// discoverRouters scans the local subnet for OpenWrt routers.
func discoverRouters() []RouterInfo {
	var found []RouterInfo
	seen := make(map[string]bool)

	// 1. Scan ARP table for known router MACs
	arpEntries := readARPTable()

	// 2. Always check common router gateway IPs
	commonIPs := []string{"192.168.1.1", "192.168.8.1", "192.168.0.1", "192.168.2.1", "10.47.41.1"}

	// Merge: ARP entries first, then common IPs
	var candidates []string
	for _, e := range arpEntries {
		if !seen[e.IP] {
			seen[e.IP] = true
			candidates = append(candidates, e.IP)
		}
	}
	for _, ip := range commonIPs {
		if !seen[ip] {
			seen[ip] = true
			candidates = append(candidates, ip)
		}
	}

	for _, ip := range candidates {
		info := probeRouter(ip)
		if info.SSH || info.HTTPPort > 0 {
			// Enrich with ARP MAC if available
			for _, a := range arpEntries {
				if a.IP == ip && info.MAC == "" {
					info.MAC = a.MAC
				}
			}
			found = append(found, info)
		}
	}
	return found
}

type arpEntry struct {
	IP  string
	MAC string
}

func readARPTable() []arpEntry {
	out, err := exec.Command("arp", "-a").Output()
	if err != nil {
		// Try ip neigh as fallback
		out, err = exec.Command("ip", "neigh").Output()
		if err != nil {
			return nil
		}
	}
	var entries []arpEntry
	lines := strings.Split(string(out), "\n")
	macRe := regexp.MustCompile(`([0-9a-fA-F]{2}[:-]){5}[0-9a-fA-F]{2}`)
	ipRe := regexp.MustCompile(`(\d+\.\d+\.\d+\.\d+)`)
	for _, line := range lines {
		macMatch := macRe.FindString(line)
		ipMatch := ipRe.FindString(line)
		if macMatch != "" && ipMatch != "" {
			entries = append(entries, arpEntry{IP: ipMatch, MAC: macMatch})
		}
	}
	return entries
}

func probeRouter(ip string) RouterInfo {
	info := RouterInfo{IP: ip, Vendor: "unknown", Model: "unknown", Firmware: "unknown"}

	// Check SSH port 22
	info.SSH = tcpProbe(ip, 22, 2*time.Second)

	// Check HTTP ports
	for _, port := range []int{80, 443, 8080} {
		if tcpProbe(ip, port, 1*time.Second) {
			info.HTTPPort = port
			break
		}
	}

	// Try SSH-based identification (passwordless first, common for fresh OpenWrt)
	if info.SSH {
		if fw, vendor, model := sshIdentify(ip, ""); fw != "" {
			info.Firmware = fw
			info.Vendor = vendor
			info.Model = model
		}
	}

	return info
}

func tcpProbe(ip string, port int, timeout time.Duration) bool {
	addr := fmt.Sprintf("%s:%d", ip, port)
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// sshIdentify tries passwordless SSH to read firmware info.
func sshIdentify(ip, password string) (firmware, vendor, model string) {
	client := sshConnect(ip, password)
	if client == nil {
		return
	}
	defer client.Close()

	out := sshRun(client, "cat /etc/openwrt_release 2>/dev/null")
	if strings.Contains(out, "OpenWrt") || strings.Contains(out, "openwrt") {
		vendor = "OpenWrt"
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "DISTRIB_DESCRIPTION") {
				parts := strings.SplitN(line, "'", 2)
				if len(parts) > 1 {
					firmware = strings.Trim(parts[1], "'")
				}
			}
		}
		if firmware == "" {
			firmware = "OpenWrt"
		}
	}

	// Try to get model
	modelOut := sshRun(client, "cat /tmp/sysinfo/board_name 2>/dev/null || cat /tmp/sysinfo/model 2>/dev/null")
	modelOut = strings.TrimSpace(modelOut)
	if modelOut != "" {
		model = modelOut
	}

	return
}
