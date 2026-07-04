package main

import (
	"net"
	"os"
	"time"

	"golang.org/x/crypto/ssh"
)

// sshConnect establishes an SSH session to the router.
// If password is empty, tries key-based auth (default SSH keys).
func sshConnect(ip, password string) *ssh.Client {
	config := &ssh.ClientConfig{
		User:            "root",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	if password != "" {
		config.Auth = []ssh.AuthMethod{ssh.Password(password)}
	} else if signer := tryDefaultKeys(); signer != nil {
		config.Auth = []ssh.AuthMethod{ssh.PublicKeys(signer)}
	} else {
		// No auth method available
		return nil
	}

	client, err := ssh.Dial("tcp", net.JoinHostPort(ip, "22"), config)
	if err != nil {
		return nil
	}
	return client
}

// sshRun executes a command and returns combined output.
func sshRun(client *ssh.Client, cmd string) string {
	session, err := client.NewSession()
	if err != nil {
		return ""
	}
	defer session.Close()
	output, err := session.CombinedOutput(cmd)
	return string(output)
}

// tryDefaultKeys attempts to load the default SSH key.
func tryDefaultKeys() ssh.Signer {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	for _, path := range []string{
		home + "/.ssh/id_ed25519",
		home + "/.ssh/id_rsa",
		home + "/.ssh/id_ecdsa",
	} {
		key, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		signer, err := ssh.ParsePrivateKey(key)
		if err == nil {
			return signer
		}
	}
	return nil
}
