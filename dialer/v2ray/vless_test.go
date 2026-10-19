package v2ray

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mzz2017/gg/dialer"
	"gopkg.in/yaml.v3"
)

const testUUID = "9cfe9a32-4515-4b6f-805f-1de788c28d95"

func realityLink() string {
	key := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	return "vless://" + testUUID + "@[::1]:443?type=tcp&security=reality&sni=example.com&pbk=" + key + "&sid=0123456789abcdef&fp=chrome&flow=xtls-rprx-vision#test"
}

func TestVLESSDefaults(t *testing.T) {
	s, err := ParseVlessURL("vless://" + testUUID + "@127.0.0.1:443#plain")
	if err != nil {
		t.Fatal(err)
	}
	if s.Net != "tcp" || s.TLS != "none" || s.Flow != "" || s.Encryption != "none" {
		t.Fatalf("unexpected defaults: %+v", s)
	}
	d, err := NewV2Ray(s.ExportToURL(), nil)
	if err != nil || !d.SupportUDP() || d.Protocol() != "vless" {
		t.Fatalf("dialer = %v, error = %v", d, err)
	}
}

func TestVLESSRoundTrip(t *testing.T) {
	for _, link := range []string{
		realityLink(),
		"vless://" + testUUID + "@127.0.0.1:443?type=raw&security=tls&sni=cert.example&alpn=h2%2Chttp%2F1.1&flow=xtls-rprx-vision-udp443&allowInsecure=true&udp=false#TLS",
		"vless://" + testUUID + "@127.0.0.1:443?type=ws&security=tls&sni=cert.example&host=cdn.example&path=%2Fws%3Fed%3D2048#WS",
		"vless://" + testUUID + "@127.0.0.1:443?type=grpc&security=tls&sni=cert.example&serviceName=service#GRPC",
	} {
		t.Run(link, func(t *testing.T) {
			s, err := ParseVlessURL(link)
			if err != nil {
				t.Fatal(err)
			}
			if s.TLS == "reality" {
				s.SpiderX = "/path?q=a%2Bb+value"
			}
			restored, err := ParseVlessURL(s.ExportToURL())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(s, restored) {
				t.Fatalf("cached link changed settings:\nwant %+v\ngot  %+v", s, restored)
			}
		})
	}
}

func TestVLESSInvalidSettings(t *testing.T) {
	tests := map[string]func(*url.URL){
		"missing user":     func(u *url.URL) { u.User = nil },
		"invalid UUID":     func(u *url.URL) { u.User = url.User("invalid") },
		"password":         func(u *url.URL) { u.User = url.UserPassword(testUUID, "password") },
		"missing server":   func(u *url.URL) { u.Host = ":443" },
		"missing port":     func(u *url.URL) { u.Host = "localhost" },
		"invalid port":     func(u *url.URL) { u.Host = "localhost:65536" },
		"zero port":        func(u *url.URL) { u.Host = "localhost:0" },
		"missing key":      func(u *url.URL) { setQuery(u, "pbk", "") },
		"short key":        func(u *url.URL) { setQuery(u, "pbk", "AQ") },
		"long key":         func(u *url.URL) { setQuery(u, "pbk", strings.Repeat("A", 64)) },
		"invalid key":      func(u *url.URL) { setQuery(u, "pbk", "!!!") },
		"odd short ID":     func(u *url.URL) { setQuery(u, "sid", "123") },
		"long short ID":    func(u *url.URL) { setQuery(u, "sid", strings.Repeat("ab", 9)) },
		"invalid short ID": func(u *url.URL) { setQuery(u, "sid", "zz") },
		"unknown security": func(u *url.URL) { setQuery(u, "security", "unknown") },
		"old XTLS flow":    func(u *url.URL) { setQuery(u, "flow", "xtls-rprx-direct") },
		"plain Vision":     func(u *url.URL) { setQuery(u, "security", "none") },
		"Reality WS":       func(u *url.URL) { setQuery(u, "type", "ws") },
		"HTTP header":      func(u *url.URL) { setQuery(u, "headerType", "http") },
		"encryption":       func(u *url.URL) { setQuery(u, "encryption", "unsupported") },
		"invalid UDP":      func(u *url.URL) { setQuery(u, "udp", "invalid") },
		"fingerprint":      func(u *url.URL) { setQuery(u, "fp", "unknown") },
		"spider path":      func(u *url.URL) { setQuery(u, "spx", "https://example.com") },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			u, _ := url.Parse(realityLink())
			change(u)
			if _, err := NewV2Ray(u.String(), nil); err == nil {
				t.Fatal("expected configuration error")
			}
		})
	}
}

func setQuery(u *url.URL, key, value string) {
	q := u.Query()
	q.Set(key, value)
	u.RawQuery = q.Encode()
}

func TestClashVLESSReality(t *testing.T) {
	key := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	config := `name: Reality 节点
type: vless
server: "::1"
port: 443
uuid: ` + testUUID + `
tls: true
servername: example.com
flow: xtls-rprx-vision
client-fingerprint: chrome
packet-encoding: xudp
reality-opts:
  public-key: ` + key + `
  short-id: "0123456789abcdef"
`
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(config), &node); err != nil {
		t.Fatal(err)
	}
	d, err := dialer.NewFromClash(&node, &dialer.GlobalOption{})
	if err != nil {
		t.Fatal(err)
	}
	if d.Name() != "Reality 节点" || d.SupportUDP() {
		t.Fatalf("unexpected name or UDP default: %s, %v", d.Name(), d.SupportUDP())
	}
	s, err := ParseVlessURL(d.Link())
	if err != nil {
		t.Fatal(err)
	}
	if s.Net != "tcp" || s.TLS != "reality" || s.PublicKey != key || s.SNI != "example.com" || s.Flow != "xtls-rprx-vision-udp443" {
		t.Fatalf("missing Clash settings: %+v", s)
	}
	if _, err := NewV2Ray(d.Link(), nil); err != nil {
		t.Fatalf("cached node cannot be restored: %v", err)
	}
}

func TestVLESSTCPWire(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		header := make([]byte, 22)
		if _, err := io.ReadFull(conn, header); err != nil {
			serverErr <- err
			return
		}
		id := uuid.MustParse(testUUID)
		want := append([]byte{0}, id[:]...)
		want = append(want, 0, 1, 1, 187, 2) // TCP, port 443, domain name
		if !bytes.Equal(header, want) {
			serverErr <- &wireError{header, want}
			return
		}
		var size [1]byte
		if _, err := io.ReadFull(conn, size[:]); err != nil {
			serverErr <- err
			return
		}
		body := make([]byte, int(size[0])+4)
		if _, err := io.ReadFull(conn, body); err != nil {
			serverErr <- err
			return
		}
		if string(body) != "example.comping" {
			serverErr <- &wireError{body, []byte("example.comping")}
			return
		}
		_, err = conn.Write(append([]byte{0, 0}, []byte("pong")...))
		serverErr <- err
	}()
	d, err := NewV2Ray("vless://"+testUUID+"@"+listener.Addr().String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := d.Dial("tcp", "example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, 4)
	if _, err := io.ReadFull(conn, response); err != nil {
		t.Fatal(err)
	}
	if string(response) != "pong" {
		t.Fatalf("unexpected response %q", response)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
}

type wireError struct{ got, want []byte }

func (e *wireError) Error() string {
	return fmt.Sprintf("unexpected VLESS wire data: got %x, want %x", e.got, e.want)
}

func TestRealityHandshakeCancellationClosesConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	closed := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			closed <- err
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, err = io.Copy(io.Discard, conn)
		closed <- err
	}()
	u, _ := url.Parse(realityLink())
	u.Host = listener.Addr().String()
	d, err := NewV2Ray(u.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	cd := dialer.ContextDialer{Dialer: d.Dialer}
	if _, err := cd.DialContext(ctx, "tcp", "example.com:443"); err == nil {
		t.Fatal("expected handshake timeout")
	}
	if err := <-closed; err != nil {
		t.Fatalf("connection was not closed after failed handshake: %v", err)
	}
}

func TestVLESSUDPWire(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		header := make([]byte, 26)
		if _, err := io.ReadFull(conn, header); err != nil {
			serverErr <- err
			return
		}
		if header[18] != 2 || binary.BigEndian.Uint16(header[19:21]) != 53 || header[21] != 1 {
			serverErr <- &wireError{header, nil}
			return
		}
		packet := make([]byte, 6)
		if _, err := io.ReadFull(conn, packet); err != nil {
			serverErr <- err
			return
		}
		if !bytes.Equal(packet, []byte{0, 4, 'p', 'i', 'n', 'g'}) {
			serverErr <- &wireError{packet, nil}
			return
		}
		_, err = conn.Write([]byte{0, 0, 0, 4, 'p', 'o', 'n', 'g'})
		serverErr <- err
	}()
	d, err := NewV2Ray("vless://"+testUUID+"@"+listener.Addr().String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := d.Dial("udp", "1.1.1.1:53")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	packet := conn.(net.PacketConn)
	if _, ok := packet.LocalAddr().(*net.UDPAddr); !ok {
		t.Fatalf("UDP relay needs UDPAddr, got %T", packet.LocalAddr())
	}
	if _, err := packet.WriteTo([]byte("ping"), &net.UDPAddr{IP: net.IPv4(1, 1, 1, 1), Port: 53}); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 64)
	n, addr, err := packet.ReadFrom(buffer)
	if err != nil || string(buffer[:n]) != "pong" || addr.String() != "1.1.1.1:53" {
		t.Fatalf("UDP reply = %q from %v: %v", buffer[:n], addr, err)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
}
