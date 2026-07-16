package main

import (
	"bytes"
	"embed"
	"fmt"
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

// sshUploadPipe writes binary data to the router via SSH stdin.
func sshUploadPipe(client *ssh.Client, data []byte, extractCmd string) string {
	session, err := client.NewSession()
	if err != nil {
		return ""
	}
	defer session.Close()
	session.Stdin = bytes.NewReader(data)
	output, err := session.CombinedOutput(extractCmd)
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
func sshDeployPortal(client *ssh.Client, fs embed.FS, rootDir string) error {
	sshRun(client, "mkdir -p "+rootDir+"/assets "+rootDir+"/locales")

	entries, err := fs.ReadDir("portal")
	if err != nil {
		return fmt.Errorf("read portal embed: %w", err)
	}

	for _, entry := range entries {
		fullPath := "portal/" + entry.Name()
		if entry.IsDir() {
			subEntries, err := fs.ReadDir(fullPath)
			if err != nil {
				continue
			}
			sshRun(client, "mkdir -p "+path.Join(rootDir, entry.Name()))
			for _, sub := range subEntries {
				if sub.IsDir() {
					continue
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
