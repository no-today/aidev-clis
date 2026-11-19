package dbconn

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// startEchoTarget is a TCP server that writes "DB-OK" then echoes input. Stands
// in for the database behind the bastion.
func startEchoTarget(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = c.Write([]byte("DB-OK"))
			}(c)
		}
	}()
	return ln.Addr().String()
}

// startTestSSHD runs an in-process sshd accepting the given password and pubkey,
// forwarding direct-tcpip channels to their requested target. Returns host:port.
func startTestSSHD(t *testing.T, password string, authorized ssh.PublicKey) string {
	t.Helper()
	_, hostPriv, _ := ed25519.GenerateKey(rand.Reader)
	hostSigner, err := ssh.NewSignerFromSigner(hostPriv)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(_ ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
			if string(p) == password {
				return nil, nil
			}
			return nil, errBadAuth
		},
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if authorized != nil && string(key.Marshal()) == string(authorized.Marshal()) {
				return nil, nil
			}
			return nil, errBadAuth
		},
	}
	cfg.AddHostKey(hostSigner)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go serveSSHConn(c, cfg)
		}
	}()
	return ln.Addr().String()
}

var errBadAuth = &authErr{}

type authErr struct{}

func (*authErr) Error() string { return "auth failed" }

func serveSSHConn(c net.Conn, cfg *ssh.ServerConfig) {
	sshConn, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		_ = c.Close()
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		if nc.ChannelType() != "direct-tcpip" {
			_ = nc.Reject(ssh.UnknownChannelType, "only direct-tcpip")
			continue
		}
		var p struct {
			DestHost string
			DestPort uint32
			OrigHost string
			OrigPort uint32
		}
		if err := ssh.Unmarshal(nc.ExtraData(), &p); err != nil {
			_ = nc.Reject(ssh.ConnectionFailed, "bad payload")
			continue
		}
		ch, chReqs, err := nc.Accept()
		if err != nil {
			continue
		}
		go ssh.DiscardRequests(chReqs)
		go func() {
			defer ch.Close()
			target, err := net.Dial("tcp", net.JoinHostPort(p.DestHost, itoa(p.DestPort)))
			if err != nil {
				return
			}
			defer target.Close()
			done := make(chan struct{}, 2)
			go func() { _, _ = io.Copy(target, ch); done <- struct{}{} }() // ssh.Channel + net.Conn both io.ReadWriteCloser
			go func() { _, _ = io.Copy(ch, target); done <- struct{}{} }()
			<-done
		}()
	}
}

func TestTunnel_ForwardsBytes_PasswordAuth(t *testing.T) {
	dbAddr := startEchoTarget(t)
	dbHost, dbPort := splitHostPort(t, dbAddr)
	sshAddr := startTestSSHD(t, "pw", nil)
	sshHost, sshPort := splitHostPort(t, sshAddr)

	tun, err := OpenTunnel(context.Background(), SSHConfig{
		Host: sshHost, Port: sshPort, User: "u",
		PasswordCredential: "x", Password: "pw",
	}, dbHost, dbPort)
	if err != nil {
		t.Fatalf("OpenTunnel: %v", err)
	}
	defer tun.Close()
	assertBanner(t, tun.LocalAddr())
}

func TestTunnel_ForwardsBytes_KeyAuth(t *testing.T) {
	dbAddr := startEchoTarget(t)
	dbHost, dbPort := splitHostPort(t, dbAddr)

	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromSigner(priv)
	keyPath := writeSignerKey(t, priv)
	sshAddr := startTestSSHD(t, "", signer.PublicKey())
	sshHost, sshPort := splitHostPort(t, sshAddr)

	tun, err := OpenTunnel(context.Background(), SSHConfig{
		Host: sshHost, Port: sshPort, User: "u", IdentityFile: keyPath,
	}, dbHost, dbPort)
	if err != nil {
		t.Fatalf("OpenTunnel: %v", err)
	}
	defer tun.Close()
	assertBanner(t, tun.LocalAddr())
}

func assertBanner(t *testing.T, localAddr string) {
	t.Helper()
	c, err := net.DialTimeout("tcp", localAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial tunnel: %v", err)
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 5)
	if _, err := c.Read(buf); err != nil {
		t.Fatalf("read through tunnel: %v", err)
	}
	if string(buf) != "DB-OK" {
		t.Fatalf("got %q through the tunnel, want DB-OK", buf)
	}
}

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	return h, atoi(t, p)
}

func atoi(t *testing.T, s string) int {
	t.Helper()
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}

func itoa(n uint32) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// writeSignerKey writes an ed25519 key as an OpenSSH PEM file, returning its path.
func writeSignerKey(t *testing.T, priv ed25519.PrivateKey) string {
	t.Helper()
	block, err := ssh.MarshalPrivateKey(priv, "test")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "id")
	if err := os.WriteFile(p, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}
