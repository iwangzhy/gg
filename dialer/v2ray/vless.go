package v2ray

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	outdialer "github.com/daeuniverse/outbound/dialer"
	"github.com/daeuniverse/outbound/netproxy"
	outprotocol "github.com/daeuniverse/outbound/protocol"
	"github.com/daeuniverse/outbound/protocol/vless"
	outtls "github.com/daeuniverse/outbound/transport/tls"
	"github.com/google/uuid"
	"github.com/mzz2017/gg/dialer"
	"gopkg.in/yaml.v3"
)

func NewVLESSFromClashObj(node *yaml.Node, opt *dialer.GlobalOption) (*dialer.Dialer, error) {
	s, err := ParseClashVLESS(node)
	if err != nil {
		return nil, err
	}
	if opt != nil && opt.AllowInsecure {
		s.AllowInsecure = true
	}
	return s.Dialer()
}

func ParseClashVLESS(node *yaml.Node) (*V2Ray, error) {
	var option struct {
		Name              string   `yaml:"name"`
		Server            string   `yaml:"server"`
		Port              int      `yaml:"port"`
		UUID              string   `yaml:"uuid"`
		Network           string   `yaml:"network"`
		TLS               bool     `yaml:"tls"`
		ServerName        string   `yaml:"servername"`
		SNI               string   `yaml:"sni"`
		Flow              string   `yaml:"flow"`
		Encryption        string   `yaml:"encryption"`
		ClientFingerprint string   `yaml:"client-fingerprint"`
		ALPN              []string `yaml:"alpn"`
		SkipCertVerify    bool     `yaml:"skip-cert-verify"`
		UDP               bool     `yaml:"udp"`
		PacketEncoding    string   `yaml:"packet-encoding"`
		Reality           *struct {
			PublicKey string `yaml:"public-key"`
			ShortID   string `yaml:"short-id"`
		} `yaml:"reality-opts"`
		WS struct {
			Path    string            `yaml:"path"`
			Headers map[string]string `yaml:"headers"`
		} `yaml:"ws-opts"`
		GRPC struct {
			ServiceName string `yaml:"grpc-service-name"`
		} `yaml:"grpc-opts"`
	}
	if err := node.Decode(&option); err != nil {
		return nil, err
	}
	s := &V2Ray{
		Ps: option.Name, Add: option.Server, Port: strconv.Itoa(option.Port),
		ID: option.UUID, Net: option.Network, Protocol: "vless", Type: "none",
		SNI: option.ServerName, Flow: option.Flow, Encryption: option.Encryption,
		Fingerprint: option.ClientFingerprint, Alpn: strings.Join(option.ALPN, ","),
		AllowInsecure: option.SkipCertVerify, UDP: &option.UDP,
	}
	if s.SNI == "" {
		s.SNI = option.SNI
	}
	if option.TLS {
		s.TLS = "tls"
	}
	if option.Reality != nil {
		if !option.TLS {
			return nil, fmt.Errorf("%w: reality-opts requires tls: true", dialer.InvalidParameterErr)
		}
		s.TLS, s.PublicKey, s.ShortID = "reality", option.Reality.PublicKey, option.Reality.ShortID
	}
	if s.Flow == vless.XRV {
		// Clash 的 Vision 允许 UDP/443；缓存为分享链接时也要保留这个行为。
		s.Flow += "-udp443"
	}
	if option.PacketEncoding != "" && (option.PacketEncoding != "xudp" || s.Flow == "") {
		return nil, fmt.Errorf("%w: packet-encoding: %s (xudp requires Vision)", dialer.UnexpectedFieldErr, option.PacketEncoding)
	}
	switch strings.ToLower(s.Net) {
	case "ws":
		s.Path, s.Host = option.WS.Path, option.WS.Headers["Host"]
	case "grpc":
		s.Path = option.GRPC.ServiceName
	}
	return s, s.validateVLESS()
}

func (s *V2Ray) validateVLESS() error {
	if s.Add == "" || strings.ContainsAny(s.Add, " /\t\r\n") {
		return fmt.Errorf("%w: VLESS server is missing or invalid", dialer.InvalidParameterErr)
	}
	port, err := strconv.Atoi(s.Port)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("%w: VLESS port must be between 1 and 65535", dialer.InvalidParameterErr)
	}
	id, err := uuid.Parse(s.ID)
	if err != nil {
		return fmt.Errorf("%w: invalid VLESS UUID", dialer.InvalidParameterErr)
	}
	s.ID = id.String()
	s.Net = strings.ToLower(s.Net)
	if s.Net == "" || s.Net == "raw" {
		s.Net = "tcp"
	}
	s.TLS = strings.ToLower(s.TLS)
	if s.TLS == "" {
		s.TLS = "none"
	}
	if s.Encryption == "" {
		s.Encryption = "none"
	}
	if s.Encryption != "none" {
		return fmt.Errorf("%w: VLESS encryption: %s", dialer.UnexpectedFieldErr, s.Encryption)
	}
	if s.TLS != "none" && s.TLS != "tls" && s.TLS != "reality" {
		return fmt.Errorf("%w: VLESS security: %s", dialer.UnexpectedFieldErr, s.TLS)
	}
	if s.Net != "tcp" && s.Net != "ws" && s.Net != "grpc" {
		return fmt.Errorf("%w: VLESS network: %s", dialer.UnexpectedFieldErr, s.Net)
	}
	if s.Net == "tcp" && s.Type != "" && s.Type != "none" {
		return fmt.Errorf("%w: VLESS headerType: %s", dialer.UnexpectedFieldErr, s.Type)
	}
	if s.Flow != "" && s.Flow != vless.XRV && s.Flow != vless.XRV+"-udp443" {
		return fmt.Errorf("%w: VLESS flow: %s", dialer.UnexpectedFieldErr, s.Flow)
	}
	if s.Flow != "" && (s.Net != "tcp" || s.TLS == "none") {
		return fmt.Errorf("%w: Vision requires TCP with TLS or Reality", dialer.InvalidParameterErr)
	}
	if s.TLS == "reality" {
		if s.Net != "tcp" {
			return fmt.Errorf("%w: Reality requires TCP", dialer.UnexpectedFieldErr)
		}
		key, err := base64.RawURLEncoding.DecodeString(s.PublicKey)
		if err != nil || len(key) != 32 {
			return fmt.Errorf("%w: Reality public key must be 32 bytes of URL-safe Base64", dialer.InvalidParameterErr)
		}
		if len(s.ShortID) > 16 || len(s.ShortID)%2 != 0 {
			return fmt.Errorf("%w: Reality short ID must contain 0 to 16 hex digits in pairs", dialer.InvalidParameterErr)
		}
		if _, err := hex.DecodeString(s.ShortID); err != nil {
			return fmt.Errorf("%w: invalid Reality short ID", dialer.InvalidParameterErr)
		}
		if s.Fingerprint == "" {
			s.Fingerprint = "chrome"
		}
		if s.SpiderX != "" && !strings.HasPrefix(s.SpiderX, "/") {
			return fmt.Errorf("%w: Reality spiderX must start with /", dialer.InvalidParameterErr)
		}
	} else if s.PublicKey != "" || s.ShortID != "" || s.SpiderX != "" {
		return fmt.Errorf("%w: Reality parameters require security=reality", dialer.InvalidParameterErr)
	}
	return nil
}

type tcpVLESSDialer struct {
	settings V2Ray
}

func (s *V2Ray) tcpVLESSDialer() (*dialer.Dialer, error) {
	d := &tcpVLESSDialer{settings: *s}
	// 导入时先校验传输参数，不等到用户选中节点后才发现不支持的指纹。
	if _, err := d.build(nil); err != nil {
		return nil, err
	}
	return dialer.NewDialer(d, s.UDP == nil || *s.UDP, s.Ps, s.Protocol, s.ExportToURL()), nil
}

func (d *tcpVLESSDialer) build(next netproxy.Dialer) (netproxy.Dialer, error) {
	s := &d.settings
	serverName := s.SNI
	if serverName == "" {
		serverName = s.Host
	}
	u := url.URL{Host: net.JoinHostPort(s.Add, s.Port)}
	q := url.Values{"sni": {serverName}}
	var err error
	switch s.TLS {
	case "reality":
		u.Scheme = "reality"
		q.Set("pbk", s.PublicKey)
		q.Set("sid", s.ShortID)
		q.Set("fp", s.Fingerprint)
		// 上游还会解码一次 spx，额外编码以保留路径中的百分号和加号。
		q.Set("spx", url.QueryEscape(s.SpiderX))
		u.RawQuery = q.Encode()
		next, err = outtls.NewReality(u.String(), next)
	case "tls":
		u.Scheme = "tls"
		if s.Fingerprint != "" {
			u.Scheme = "utls"
			q.Set("utlsImitate", s.Fingerprint)
		}
		q.Set("allowInsecure", strconv.FormatBool(s.AllowInsecure))
		q.Set("alpn", s.Alpn)
		u.RawQuery = q.Encode()
		next, _, err = outtls.NewTls(&outdialer.ExtraOption{}, next, u.String())
	}
	if err != nil {
		return nil, err
	}
	return vless.NewDialer(next, outprotocol.Header{
		ProxyAddress: u.Host, Password: s.ID, IsClient: true,
		Feature1: strings.TrimSuffix(s.Flow, "-udp443"),
	})
}

func (d *tcpVLESSDialer) Dial(network, addr string) (net.Conn, error) {
	return d.DialContext(context.Background(), network, addr)
}

func (d *tcpVLESSDialer) DialContext(ctx context.Context, network, addr string) (result net.Conn, err error) {
	if network != "tcp" && network != "udp" {
		return nil, net.UnknownNetworkError(network)
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if network == "udp" {
		if d.settings.UDP != nil && !*d.settings.UDP {
			return nil, fmt.Errorf("UDP is disabled for this VLESS node")
		}
		if d.settings.Flow == vless.XRV && port == "443" {
			return nil, fmt.Errorf("xtls-rprx-vision blocks UDP/443; use xtls-rprx-vision-udp443 to allow it")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	raw, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(d.settings.Add, d.settings.Port))
	if err != nil {
		return nil, err
	}
	// Reality 和协议头任一步失败都要关闭底层连接；连接成功后取消拨号超时。
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer func() {
		stop()
		if err != nil {
			_ = raw.Close()
		}
	}()
	deadline, _ := ctx.Deadline()
	if err = raw.SetDeadline(deadline); err != nil {
		return nil, err
	}
	chain, err := d.build(&connectedVLESSDialer{raw})
	if err != nil {
		return nil, err
	}
	conn, err := chain.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	if !stop() {
		return nil, ctx.Err()
	}
	if err = raw.SetDeadline(time.Time{}); err != nil {
		return nil, err
	}
	if network == "udp" {
		packet, ok := conn.(netproxy.PacketConn)
		if !ok {
			return nil, fmt.Errorf("VLESS connection does not support UDP")
		}
		// gg 的 UDP 转发会用 UDPAddr 查询 MTU，不能暴露底层 TCP 隧道的地址类型。
		local := raw.LocalAddr().(*net.TCPAddr)
		remote := raw.RemoteAddr().(*net.TCPAddr)
		return netproxy.NewFakeNetPacketConn(packet,
			&net.UDPAddr{IP: local.IP, Port: local.Port, Zone: local.Zone},
			&net.UDPAddr{IP: remote.IP, Port: remote.Port, Zone: remote.Zone}), nil
	}
	return &netproxy.FakeNetConn{Conn: conn, LAddr: raw.LocalAddr(), RAddr: raw.RemoteAddr()}, nil
}

type connectedVLESSDialer struct{ net.Conn }

func (d *connectedVLESSDialer) DialContext(context.Context, string, string) (netproxy.Conn, error) {
	return d.Conn, nil
}
