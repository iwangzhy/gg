package cmd

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mzz2017/gg/dialer"
	_ "github.com/mzz2017/gg/dialer/shadowsocks"
	"github.com/mzz2017/gg/dialer/trojan"
	"github.com/mzz2017/gg/dialer/v2ray"
	_ "github.com/mzz2017/softwind/protocol/shadowsocks"
	_ "github.com/mzz2017/softwind/protocol/trojanc"
	"github.com/sirupsen/logrus"
)

const subscriptionUUID = "9cfe9a32-4515-4b6f-805f-1de788c28d95"

const clashSubscription = `mixed-port: 7890
proxies:
  - name: VLESS 节点
    type: vless
    server: proxy.example
    port: 443
    uuid: ` + subscriptionUUID + `
    tls: true
    servername: tls.example
    udp: true
  - {name: SS, type: ss, server: proxy.example, port: 8388, cipher: aes-128-gcm, password: test}
  - {name: VMess, type: vmess, server: proxy.example, port: 443, uuid: ` + subscriptionUUID + `, alterId: 0, cipher: auto}
  - {name: Trojan, type: trojan, server: proxy.example, port: 443, password: test}
proxy-groups:
  - name: select
    type: select
    proxies: [VLESS 节点, SS]
rules:
  - MATCH,select
`

func subscriptionLogger() *logrus.Logger {
	log := logrus.New()
	log.SetOutput(io.Discard)
	return log
}

func TestClashSubscriptionFormats(t *testing.T) {
	formats := map[string]string{
		"YAML":            clashSubscription,
		"BOM":             "\ufeff" + clashSubscription,
		"standard base64": base64.StdEncoding.EncodeToString([]byte(clashSubscription)),
		"raw base64":      base64.RawStdEncoding.EncodeToString([]byte(clashSubscription)),
		"URL base64":      base64.URLEncoding.EncodeToString([]byte(clashSubscription)),
		"raw URL base64":  base64.RawURLEncoding.EncodeToString([]byte(clashSubscription)),
		"JSON":            `{"proxies":[{"name":"VLESS 节点","type":"vless","server":"proxy.example","port":443,"uuid":"` + subscriptionUUID + `","tls":true}]}`,
	}
	for name, body := range formats {
		t.Run(name, func(t *testing.T) {
			dialers, err := resolveSubscriptionAsClash(subscriptionLogger(), &dialer.GlobalOption{}, []byte(body))
			wantCount := 4
			if name == "JSON" {
				wantCount = 1
			}
			if err != nil || len(dialers) != wantCount {
				t.Fatalf("nodes = %d, error = %v", len(dialers), err)
			}
			if dialers[0].Name() != "VLESS 节点" {
				t.Fatalf("node name = %q", dialers[0].Name())
			}
			for _, d := range dialers {
				if _, err := GetDialerFromLink(d.Link(), &dialer.GlobalOption{}, false, ""); err != nil {
					t.Fatalf("cached %s cannot be restored: %v", d.Protocol(), err)
				}
			}
		})
	}
}

func TestClashSubscriptionSkipsInvalidNodes(t *testing.T) {
	data := `proxies:
  - {name: unsupported, type: unsupported}
  - {name: broken, type: vless, server: proxy.example, port: 443, uuid: invalid}
  - {name: valid, type: vless, server: proxy.example, port: 443, uuid: ` + subscriptionUUID + `}
`
	dialers, err := resolveSubscriptionAsClash(subscriptionLogger(), &dialer.GlobalOption{}, []byte(data))
	if err != nil || len(dialers) != 1 || dialers[0].Name() != "valid" {
		t.Fatalf("nodes = %v, error = %v", dialers, err)
	}
}

func TestClashGRPCSubscriptionCache(t *testing.T) {
	data := `proxies:
  - name: VLESS
    type: vless
    server: proxy.example
    port: 443
    uuid: ` + subscriptionUUID + `
    tls: true
    network: grpc
    grpc-opts: {grpc-service-name: custom-service}
  - name: VMess
    type: vmess
    server: proxy.example
    port: 443
    uuid: ` + subscriptionUUID + `
    tls: true
    network: grpc
    grpc-opts: {grpc-service-name: custom-service}
  - name: Trojan
    type: trojan
    server: proxy.example
    port: 443
    password: password
    network: grpc
    grpc-opts: {grpc-service-name: custom-service}
`
	dialers, err := resolveSubscriptionAsClash(subscriptionLogger(), &dialer.GlobalOption{}, []byte(data))
	if err != nil || len(dialers) != 3 {
		t.Fatalf("nodes = %d, error = %v", len(dialers), err)
	}
	vless, err := v2ray.ParseVlessURL(dialers[0].Link())
	if err != nil || vless.Path != "custom-service" {
		t.Fatalf("VLESS cache lost gRPC service: %+v, %v", vless, err)
	}
	vmess, err := v2ray.ParseVmessURL(dialers[1].Link())
	if err != nil || vmess.Path != "custom-service" {
		t.Fatalf("VMess cache lost gRPC service: %+v, %v", vmess, err)
	}
	trojanNode, err := trojan.ParseTrojanURL(dialers[2].Link())
	if err != nil || trojanNode.ServiceName != "custom-service" {
		t.Fatalf("Trojan cache lost gRPC service: %+v, %v", trojanNode, err)
	}
}

func TestInvalidClashSubscriptions(t *testing.T) {
	for _, data := range []string{"", "{}", "rules: []", "proxies: []", "proxies: null", "proxies: invalid", "proxies: [", "proxies: [{type: unsupported}]"} {
		t.Run(data, func(t *testing.T) {
			if _, err := resolveSubscriptionAsClash(subscriptionLogger(), &dialer.GlobalOption{}, []byte(data)); err == nil {
				t.Fatal("expected a useful subscription error")
			}
		})
	}
}

func TestPullSubscription(t *testing.T) {
	link := "vless://" + subscriptionUUID + "@proxy.example:443#link"
	tests := []struct {
		name   string
		body   string
		status int
		count  int
	}{
		{"Clash", clashSubscription, 200, 4},
		{"encoded Clash", base64.StdEncoding.EncodeToString([]byte(clashSubscription)), 200, 4},
		{"links", link + "\r\n", 200, 1},
		{"encoded links", base64.RawStdEncoding.EncodeToString([]byte(link)), 200, 1},
		{"URL encoded links", base64.RawURLEncoding.EncodeToString([]byte(link)), 200, 1},
		{"SIP008", `{"version":1,"servers":[{"server":"proxy.example","server_port":8388,"password":"test","method":"aes-128-gcm","remarks":"SS"}]}`, 200, 1},
		{"HTTP failure", clashSubscription, 403, 0},
		{"HTML response", "<html>please sign in</html>", 200, 0},
		{"empty list", "proxies: []", 200, 0},
		{"missing proxies", "rules: []", 200, 0},
		{"empty SIP008", `{"version":1,"servers":[]}`, 200, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			dialers, err := pullDialersFromSubscription(subscriptionLogger(), &dialer.GlobalOption{}, server.URL)
			if test.count == 0 {
				if err == nil {
					t.Fatal("expected subscription error")
				}
				if test.status != 200 && !strings.Contains(err.Error(), "403") {
					t.Fatalf("missing HTTP status: %v", err)
				}
			} else if err != nil || len(dialers) != test.count {
				t.Fatalf("nodes = %d, error = %v", len(dialers), err)
			}
		})
	}
}
