package dbconn

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// SSHConfig describes one SSH bastion to tunnel through. Exactly one of
// IdentityFile / PasswordCredential is set (ParseSSHConfig enforces XOR).
type SSHConfig struct {
	Host                    string
	Port                    int // 0 -> 22
	User                    string
	IdentityFile            string
	KeyPassphraseCredential string // credstore key for an encrypted key's passphrase
	Passphrase              string // loaded from credstore by Resolve
	PasswordCredential      string // credstore key for password auth
	Password                string // loaded from credstore by Resolve
	HandshakeWait           time.Duration
}

// ParseSSHConfig extracts the `ssh:` block. host+user required; identity_file
// XOR password_credential.
func ParseSSHConfig(raw map[string]any) (*SSHConfig, error) {
	cfg := &SSHConfig{Port: 22}
	host, _ := raw["host"].(string)
	if host == "" {
		return nil, errs.Config("SSH_HOST_MISSING", "ssh block missing 'host'")
	}
	cfg.Host = host
	if v, ok := raw["port"]; ok {
		switch p := v.(type) {
		case int:
			cfg.Port = p
		case int64:
			cfg.Port = int(p)
		case float64:
			cfg.Port = int(p)
		}
	}
	user, _ := raw["user"].(string)
	if user == "" {
		return nil, errs.Config("SSH_USER_MISSING", "ssh block missing 'user'")
	}
	cfg.User = user
	idfile, _ := raw["identity_file"].(string)
	passCred, _ := raw["password_credential"].(string)
	switch {
	case idfile != "" && passCred != "":
		return nil, errs.Config("SSH_AUTH_CONFLICT", "ssh block must set exactly one of 'identity_file' or 'password_credential'")
	case idfile == "" && passCred == "":
		return nil, errs.Config("SSH_AUTH_MISSING", "ssh block must set one of 'identity_file' or 'password_credential'")
	case idfile != "":
		cfg.IdentityFile = idfile
		cfg.KeyPassphraseCredential, _ = raw["key_passphrase_credential"].(string)
	default:
		cfg.PasswordCredential = passCred
	}
	return cfg, nil
}

// Tunnel is an active SSH local-forward.
type Tunnel struct {
	Listener net.Listener
	client   *ssh.Client
	target   string
	mu       sync.Mutex
	closed   bool
	wg       sync.WaitGroup
}

func (t *Tunnel) LocalAddr() string { return t.Listener.Addr().String() }

func (t *Tunnel) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.mu.Unlock()
	_ = t.Listener.Close()
	err := t.client.Close()
	t.wg.Wait()
	return err
}

// ExpandHome replaces a leading "~/" with the user's home directory.
func ExpandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// LoadPrivateKey reads an OpenSSH/PEM private key; passphrase required iff encrypted.
func LoadPrivateKey(path, passphrase string) (ssh.Signer, error) {
	data, err := os.ReadFile(ExpandHome(path))
	if err != nil {
		return nil, errs.Auth("SSH_KEY_READ", fmt.Sprintf("read identity_file %s: %v", path, err))
	}
	if passphrase == "" {
		signer, err := ssh.ParsePrivateKey(data)
		if err != nil {
			if _, ok := err.(*ssh.PassphraseMissingError); ok {
				return nil, errs.Auth("SSH_KEY_PASSPHRASE_REQUIRED",
					fmt.Sprintf("identity_file %s is encrypted; set key_passphrase_credential", path))
			}
			return nil, errs.Auth("SSH_KEY_PARSE", fmt.Sprintf("parse %s: %v", path, err))
		}
		return signer, nil
	}
	signer, err := ssh.ParsePrivateKeyWithPassphrase(data, []byte(passphrase))
	if err != nil {
		return nil, errs.Auth("SSH_KEY_DECRYPT", fmt.Sprintf("decrypt %s: %v", path, err))
	}
	return signer, nil
}

// OpenTunnel dials the bastion, then opens a 127.0.0.1:0 listener forwarding each
// accepted conn to remoteHost:remotePort over the SSH session.
func OpenTunnel(ctx context.Context, cfg SSHConfig, remoteHost string, remotePort int) (*Tunnel, error) {
	port := cfg.Port
	if port == 0 {
		port = 22
	}
	var auth []ssh.AuthMethod
	switch {
	case cfg.IdentityFile != "":
		signer, err := LoadPrivateKey(cfg.IdentityFile, cfg.Passphrase)
		if err != nil {
			return nil, err
		}
		auth = []ssh.AuthMethod{ssh.PublicKeys(signer)}
	case cfg.PasswordCredential != "":
		if cfg.Password == "" {
			return nil, errs.Auth("SSH_PASSWORD_MISSING", fmt.Sprintf("ssh password_credential %q resolved to empty value", cfg.PasswordCredential))
		}
		auth = []ssh.AuthMethod{ssh.Password(cfg.Password)}
	default:
		return nil, errs.Auth("SSH_AUTH_UNSET", "ssh block has neither identity_file nor password_credential")
	}

	clientCfg := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // v1: no host-key pinning (see SECURITY-MODEL)
		Timeout:         cfg.HandshakeWait,
	}
	if clientCfg.Timeout == 0 {
		clientCfg.Timeout = 10 * time.Second
	}
	addr := fmt.Sprintf("%s:%d", cfg.Host, port)
	tcpConn, err := (&net.Dialer{Timeout: clientCfg.Timeout}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, errs.Remote("SSH_TCP_DIAL", fmt.Sprintf("dial bastion %s: %v", addr, err))
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(tcpConn, addr, clientCfg)
	if err != nil {
		_ = tcpConn.Close()
		return nil, errs.Auth("SSH_HANDSHAKE", fmt.Sprintf("ssh handshake to %s: %v", addr, err))
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	lst, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = client.Close()
		return nil, errs.Remote("SSH_LISTEN", fmt.Sprintf("local listener: %v", err))
	}
	t := &Tunnel{Listener: lst, client: client, target: fmt.Sprintf("%s:%d", remoteHost, remotePort)}
	t.wg.Add(1)
	go t.acceptLoop()
	return t, nil
}

func (t *Tunnel) acceptLoop() {
	defer t.wg.Done()
	for {
		local, err := t.Listener.Accept()
		if err != nil {
			return
		}
		t.wg.Add(1)
		go t.handleConn(local)
	}
}

func (t *Tunnel) handleConn(local net.Conn) {
	defer t.wg.Done()
	defer local.Close()
	remote, err := t.client.Dial("tcp", t.target)
	if err != nil {
		return
	}
	defer remote.Close()
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(remote, local); done <- struct{}{} }()
	go func() { _, _ = io.Copy(local, remote); done <- struct{}{} }()
	<-done
}
