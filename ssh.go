package main

import (
	"embed"
	"fmt"
	"io"
	"net"
	"os"
	"path"
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

// sshWriteFile writes content to a remote path via SSH (cat > path).
func sshWriteFile(client *ssh.Client, remotePath string, content []byte) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		return err
	}

	if err := session.Start("cat > " + remotePath); err != nil {
		return err
	}

	_, err = stdin.Write(content)
	if err != nil {
		return err
	}
	stdin.Close()

	return session.Wait()
}

// sshDeployPortal writes the embedded portal files to the router.
// Creates the target directory, then writes each file.
func sshDeployPortal(client *ssh.Client, fs embed.FS, rootDir string) error {
	// Create target directory structure
	sshRun(client, "mkdir -p "+rootDir+"/assets "+rootDir+"/locales")

	// Walk the embedded FS and write each file
	entries, err := fs.ReadDir("portal")
	if err != nil {
		return fmt.Errorf("read portal embed: %w", err)
	}

	for _, entry := range entries {
		fullPath := "portal/" + entry.Name()
		if entry.IsDir() {
			// Write directory contents
			subEntries, err := fs.ReadDir(fullPath)
			if err != nil {
				continue
			}
			for _, sub := range subEntries {
				if sub.IsDir() {
					continue // Only 1 level deep
				}
				data, err := fs.ReadFile(fullPath + "/" + sub.Name())
				if err != nil {
					continue
				}
				remotePath := path.Join(rootDir, entry.Name(), sub.Name())
				if err := sshWriteFile(client, remotePath, data); err != nil {
					return fmt.Errorf("write %s: %w", remotePath, err)
				}
			}
		} else {
			data, err := fs.ReadFile(fullPath)
			if err != nil {
				continue
			}
			remotePath := path.Join(rootDir, entry.Name())
			if err := sshWriteFile(client, remotePath, data); err != nil {
				return fmt.Errorf("write %s: %w", remotePath, err)
			}
		}
	}

	return nil
}

// tryDefaultKeys attempts to load the default SSH key.
func tryDefaultKeys() ssh.Signer {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	for _, p := range []string{
		home + "/.ssh/id_ed25519",
		home + "/.ssh/id_rsa",
		home + "/.ssh/id_ecdsa",
	} {
		key, err := os.ReadFile(p)
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

// unused import guard for io (used indirectly through stdin pipe)
var _ io.Writer = (*os.File)(nil)
