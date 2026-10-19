package v2ray

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/mzz2017/gg/dialer"
	"github.com/mzz2017/gg/dialer/trojan"
	_ "github.com/mzz2017/softwind/protocol/trojanc"
)

// XRAY_BIN 指向独立的官方 Xray 可执行文件；所有服务和流量都留在本机。
func TestVLESSXrayInterop(t *testing.T) {
	xray := os.Getenv("XRAY_BIN")
	if xray == "" {
		t.Skip("set XRAY_BIN to run VLESS/TLS/Reality/Vision interoperability tests")
	}
	for _, test := range []struct{ security, flow string }{
		{"none", ""},
		{"tls", ""},
		{"tls", "xtls-rprx-vision"},
		{"reality", ""},
		{"reality", "xtls-rprx-vision"},
	} {
		t.Run(test.security+"/"+test.flow, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				_, _ = w.Write(body)
			})
			target := httptest.NewUnstartedServer(handler)
			target.EnableHTTP2 = true
			// 使用 X25519 便于与旧版 Xray 的 Reality 目标握手互通。
			target.TLS = &tls.Config{CurvePreferences: []tls.CurveID{tls.X25519}}
			target.StartTLS()
			defer target.Close()
			plainTarget := httptest.NewServer(handler)
			defer plainTarget.Close()
			link := startXrayProxy(t, xray, "vless", "tcp", test.security, test.flow, target)
			d, err := NewV2Ray(link, nil)
			if err != nil {
				t.Fatal(err)
			}
			transport := &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return d.Dialer.(*tcpVLESSDialer).DialContext(ctx, network, addr)
				},
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
			// HTTPS 大包会触发 Vision 切换，不能只用几字节的明文验证握手。
			payload := bytes.Repeat([]byte("VLESS interop payload\n"), 4096)
			for _, targetURL := range []string{plainTarget.URL, target.URL} {
				resp, err := client.Post(targetURL, "application/octet-stream", bytes.NewReader(payload))
				if err != nil {
					t.Fatalf("HTTP relay to %s: %v", targetURL, err)
				}
				body, err := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if err != nil || !bytes.Equal(body, payload) {
					t.Fatalf("payload mismatch: got %d bytes, want %d: %v", len(body), len(payload), err)
				}
			}
			testXrayUDP(t, d.Dial)
			if test.security == "reality" {
				for _, parameter := range []string{"pbk", "sid", "sni"} {
					t.Run("reject wrong "+parameter, func(t *testing.T) {
						u, _ := url.Parse(link)
						value := "bad.example"
						if parameter == "pbk" {
							other, _ := ecdh.X25519().GenerateKey(rand.Reader)
							value = base64.RawURLEncoding.EncodeToString(other.PublicKey().Bytes())
						} else if parameter == "sid" {
							value = "abcdef0123456789"
						}
						setQuery(u, parameter, value)
						setQuery(u, "allowInsecure", "true")
						bad, err := NewV2Ray(u.String(), nil)
						if err != nil {
							t.Fatal(err)
						}
						conn, err := bad.Dial("tcp", plainTarget.Listener.Addr().String())
						if err == nil {
							_ = conn.Close()
							t.Fatal("Reality accepted invalid authentication")
						}
					})
				}
			}
		})
	}
}

func startXrayProxy(t *testing.T, binary, protocol, network, security, flow string, target *httptest.Server) string {
	t.Helper()
	temp := t.TempDir()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	u := url.URL{Scheme: protocol, User: url.User(testUUID), Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(port))}
	q := url.Values{"type": {network}, "security": {security}, "flow": {flow}, "sni": {"example.com"}}
	stream := map[string]interface{}{"network": network, "security": security}
	if network == "grpc" {
		stream["grpcSettings"] = map[string]interface{}{"serviceName": "custom-service"}
		q.Set("serviceName", "custom-service")
	}
	if network == "ws" {
		stream["wsSettings"] = map[string]interface{}{"path": "/websocket"}
		q.Set("path", "/websocket")
	}
	switch security {
	case "tls":
		certificate := target.TLS.Certificates[0]
		certFile := filepath.Join(temp, "server.pem")
		keyFile := filepath.Join(temp, "key.pem")
		key, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600); err != nil {
			t.Fatal(err)
		}
		stream["tlsSettings"] = map[string]interface{}{
			"minVersion":   "1.3",
			"certificates": []interface{}{map[string]interface{}{"certificateFile": certFile, "keyFile": keyFile}},
		}
		q.Set("allowInsecure", "true")
	case "reality":
		key, err := ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		stream["realitySettings"] = map[string]interface{}{
			"dest": target.Listener.Addr().String(), "serverNames": []string{"example.com"},
			"privateKey": base64.RawURLEncoding.EncodeToString(key.Bytes()),
			"shortIds":   []string{"0123456789abcdef"}, "maxTimeDiff": 60000,
		}
		q.Set("pbk", base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()))
		q.Set("sid", "0123456789abcdef")
		fingerprint := os.Getenv("XRAY_FINGERPRINT")
		if fingerprint == "" {
			fingerprint = "chrome"
		}
		q.Set("fp", fingerprint)
	}
	u.RawQuery = q.Encode()
	settings := map[string]interface{}{
		"clients": []interface{}{map[string]interface{}{"id": testUUID, "flow": flow}}, "decryption": "none",
	}
	if protocol == "trojan" {
		settings = map[string]interface{}{"clients": []interface{}{map[string]interface{}{"password": testUUID}}}
	} else if protocol == "vmess" {
		settings = map[string]interface{}{"clients": []interface{}{map[string]interface{}{"id": testUUID, "alterId": 0}}}
	}
	config := map[string]interface{}{
		"log": map[string]interface{}{"loglevel": "warning"},
		"inbounds": []interface{}{map[string]interface{}{
			"listen": "127.0.0.1", "port": port, "protocol": protocol,
			"settings":       settings,
			"streamSettings": stream,
		}},
		"outbounds": []interface{}{map[string]interface{}{"protocol": "freedom"}},
	}
	data, _ := json.Marshal(config)
	configFile := filepath.Join(temp, "config.json")
	if err := os.WriteFile(configFile, data, 0600); err != nil {
		t.Fatal(err)
	}
	logFile, err := os.Create(filepath.Join(temp, "xray.log"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "run", "-config", configFile)
	command.Stdout, command.Stderr = logFile, logFile
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
		_ = logFile.Close()
		if t.Failed() {
			logs, _ := os.ReadFile(logFile.Name())
			t.Logf("Xray log:\n%s", logs)
		}
	})
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		conn, err := net.DialTimeout("tcp", u.Host, 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return u.String()
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("Xray did not start")
	return ""
}

func TestExistingProtocolsXrayInterop(t *testing.T) {
	xray := os.Getenv("XRAY_BIN")
	if xray == "" {
		t.Skip("set XRAY_BIN to run existing protocol regression tests")
	}
	for _, test := range []struct{ protocol, network, security string }{
		{"vmess", "tcp", "none"},
		{"vmess", "ws", "none"},
		{"vmess", "grpc", "tls"},
		{"vless", "ws", "none"},
		{"vless", "grpc", "tls"},
		{"trojan", "grpc", "tls"},
	} {
		t.Run(test.protocol+"/"+test.network, func(t *testing.T) {
			target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, "existing protocol works")
			}))
			defer target.Close()
			link := startXrayProxy(t, xray, test.protocol, test.network, test.security, "", target)
			var d *dialer.Dialer
			var err error
			if test.protocol == "trojan" {
				d, err = trojan.NewTrojan(link, &dialer.GlobalOption{AllowInsecure: true})
			} else {
				u, _ := url.Parse(link)
				s := &V2Ray{
					Add: u.Hostname(), Port: u.Port(), ID: testUUID,
					Protocol: test.protocol, Net: test.network, TLS: test.security,
					SNI: "example.com", AllowInsecure: true, Path: u.Query().Get("path"),
				}
				if test.network == "grpc" {
					s.Path = u.Query().Get("serviceName")
				}
				d, err = s.Dialer()
			}
			if err != nil {
				t.Fatal(err)
			}
			cd := dialer.ContextDialer{Dialer: d.Dialer}
			transport := &http.Transport{DialContext: cd.DialContext, TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
			resp, err := client.Get(target.URL)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err != nil || string(body) != "existing protocol works" {
				t.Fatalf("unexpected response %q: %v", body, err)
			}
		})
	}
}

func testXrayUDP(t *testing.T, dial func(string, string) (net.Conn, error)) {
	t.Helper()
	echo, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		buffer := make([]byte, 65535)
		for {
			n, addr, err := echo.ReadFrom(buffer)
			if err != nil {
				return
			}
			_, _ = echo.WriteTo(buffer[:n], addr)
		}
	}()
	conn, err := dial("udp", echo.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	packet := conn.(net.PacketConn)
	if _, ok := packet.LocalAddr().(*net.UDPAddr); !ok {
		t.Fatalf("UDP relay needs UDPAddr, got %T", packet.LocalAddr())
	}
	for _, size := range []int{4, 512, 2048} {
		payload := bytes.Repeat([]byte{byte(size % 251)}, size)
		if _, err := packet.WriteTo(payload, echo.LocalAddr()); err != nil {
			t.Fatal(err)
		}
		buffer := make([]byte, 4096)
		n, addr, err := packet.ReadFrom(buffer)
		if err != nil || !bytes.Equal(buffer[:n], payload) || addr.String() != echo.LocalAddr().String() {
			t.Fatalf("UDP relay: got %d bytes from %v, want %d from %v: %v", n, addr, size, echo.LocalAddr(), err)
		}
	}
}
