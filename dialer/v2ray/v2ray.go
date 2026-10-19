package v2ray

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/daeuniverse/outbound/protocol"
	jsoniter "github.com/json-iterator/go"
	"github.com/mzz2017/gg/common"
	"github.com/mzz2017/gg/dialer"
	"github.com/mzz2017/gg/dialer/transport/grpc"
	"github.com/mzz2017/gg/dialer/transport/tls"
	"github.com/mzz2017/gg/dialer/transport/ws"
	"gopkg.in/yaml.v3"
)

func init() {
	dialer.FromLinkRegister("vmess", NewV2Ray)
	dialer.FromLinkRegister("vless", NewV2Ray)
	dialer.FromClashRegister("vmess", NewVMessFromClashObj)
	dialer.FromClashRegister("vless", NewVLESSFromClashObj)
}

type V2Ray struct {
	Ps            string `json:"ps"`
	Add           string `json:"add"`
	Port          string `json:"port"`
	ID            string `json:"id"`
	Aid           string `json:"aid"`
	Net           string `json:"net"`
	Type          string `json:"type"`
	Host          string `json:"host"`
	SNI           string `json:"sni"`
	Path          string `json:"path"`
	TLS           string `json:"tls"`
	Flow          string `json:"flow,omitempty"`
	Encryption    string `json:"encryption,omitempty"`
	Fingerprint   string `json:"fp,omitempty"`
	PublicKey     string `json:"pbk,omitempty"`
	ShortID       string `json:"sid,omitempty"`
	SpiderX       string `json:"spx,omitempty"`
	UDP           *bool  `json:"udp,omitempty"`
	Alpn          string `json:"alpn,omitempty"`
	AllowInsecure bool   `json:"allowInsecure"`
	V             string `json:"v"`
	Protocol      string `json:"protocol"`
}

func NewV2Ray(link string, opt *dialer.GlobalOption) (*dialer.Dialer, error) {
	var (
		s   *V2Ray
		err error
	)
	switch {
	case strings.HasPrefix(link, "vmess://"):
		s, err = ParseVmessURL(link)
		if err != nil {
			return nil, err
		}
		if s.Aid != "0" && s.Aid != "" {
			return nil, fmt.Errorf("%w: aid: %v, we only support AEAD encryption", dialer.UnexpectedFieldErr, s.Aid)
		}
	case strings.HasPrefix(link, "vless://"):
		s, err = ParseVlessURL(link)
		if err != nil {
			return nil, err
		}
	default:
		return nil, dialer.InvalidParameterErr
	}
	if opt != nil && opt.AllowInsecure {
		s.AllowInsecure = true
	}
	return s.Dialer()
}

func NewVMessFromClashObj(o *yaml.Node, opt *dialer.GlobalOption) (*dialer.Dialer, error) {
	s, err := ParseClashVMess(o)
	if err != nil {
		return nil, err
	}
	if opt.AllowInsecure {
		s.AllowInsecure = true
	}
	return s.Dialer()
}

func (s *V2Ray) Dialer() (data *dialer.Dialer, err error) {
	if s.Protocol == "vless" {
		if err := s.validateVLESS(); err != nil {
			return nil, err
		}
		if s.Net == "tcp" {
			return s.tcpVLESSDialer()
		}
	}
	// 未实现的安全层必须明确报错，否则会意外按明文 TCP 建连。
	if s.TLS != "" && s.TLS != "none" && s.TLS != "tls" {
		return nil, fmt.Errorf("%w: security: %v", dialer.UnexpectedFieldErr, s.TLS)
	}
	var (
		d = dialer.SymmetricDirect
	)

	switch strings.ToLower(s.Net) {
	case "ws":
		scheme := "ws"
		if s.TLS == "tls" {
			scheme = "wss"
		}
		sni := s.SNI
		if sni == "" {
			sni = s.Host
		}
		u := url.URL{
			Scheme: scheme,
			Host:   net.JoinHostPort(s.Add, s.Port),
			Path:   s.Path,
			RawQuery: url.Values{
				"host":          []string{s.Host},
				"sni":           []string{sni},
				"allowInsecure": []string{common.BoolToString(s.AllowInsecure)},
			}.Encode(),
		}
		d, err = ws.NewWs(u.String(), d)
		if err != nil {
			return nil, err
		}
	case "tcp":
		if s.TLS == "tls" {
			sni := s.SNI
			if sni == "" {
				sni = s.Host
			}
			u := url.URL{
				Scheme: "tls",
				Host:   net.JoinHostPort(s.Add, s.Port),
				RawQuery: url.Values{
					"sni":           []string{sni},
					"allowInsecure": []string{common.BoolToString(s.AllowInsecure)},
				}.Encode(),
			}
			d, err = tls.NewTls(u.String(), d)
			if err != nil {
				return nil, err
			}
		}
		if s.Type != "none" && s.Type != "" {
			return nil, fmt.Errorf("%w: type: %v", dialer.UnexpectedFieldErr, s.Type)
		}
	case "grpc":
		sni := s.SNI
		if sni == "" {
			sni = s.Host
		}
		serviceName := s.Path
		if serviceName == "" {
			serviceName = "GunService"
		}
		d = &grpc.Dialer{
			NextDialer:    d,
			ServiceName:   serviceName,
			ServerName:    sni,
			AllowInsecure: s.AllowInsecure,
		}
	default:
		return nil, fmt.Errorf("%w: network: %v", dialer.UnexpectedFieldErr, s.Net)
	}

	next, err := protocol.NewDialer(s.Protocol, dialer.ToOutbound(d), protocol.Header{
		ProxyAddress: net.JoinHostPort(s.Add, s.Port),
		Cipher:       "aes-128-gcm",
		Password:     s.ID,
		IsClient:     true,
		Feature1:     s.Flow,
	})
	if err != nil {
		return nil, err
	}
	d = dialer.FromOutbound(next)
	supportUDP := s.UDP == nil || *s.UDP
	return dialer.NewDialer(d, supportUDP, s.Ps, s.Protocol, s.ExportToURL()), nil
}

func ParseClashVMess(o *yaml.Node) (data *V2Ray, err error) {
	type WSOptions struct {
		Path                string            `yaml:"path,omitempty"`
		Headers             map[string]string `yaml:"headers,omitempty"`
		MaxEarlyData        int               `yaml:"max-early-data,omitempty"`
		EarlyDataHeaderName string            `yaml:"early-data-header-name,omitempty"`
	}
	type GrpcOptions struct {
		GrpcServiceName string `yaml:"grpc-service-name,omitempty"`
	}
	type HTTP2Options struct {
		Host []string `proxy:"host,omitempty"`
		Path string   `proxy:"path,omitempty"`
	}
	type VmessOption struct {
		Name           string       `yaml:"name"`
		Server         string       `yaml:"server"`
		Port           int          `yaml:"port"`
		UUID           string       `yaml:"uuid"`
		AlterID        int          `yaml:"alterId"`
		Cipher         string       `yaml:"cipher"`
		UDP            bool         `yaml:"udp,omitempty"`
		Network        string       `yaml:"network,omitempty"`
		TLS            bool         `yaml:"tls,omitempty"`
		SkipCertVerify bool         `yaml:"skip-cert-verify,omitempty"`
		ServerName     string       `yaml:"servername,omitempty"`
		HTTPOpts       interface{}  `yaml:"http-opts,omitempty"`
		HTTP2Opts      HTTP2Options `yaml:"h2-opts,omitempty"`
		GrpcOpts       GrpcOptions  `yaml:"grpc-opts,omitempty"`
		WSOpts         WSOptions    `yaml:"ws-opts,omitempty"`
	}
	var option VmessOption
	if err = o.Decode(&option); err != nil {
		return nil, err
	}

	if option.Network == "" {
		option.Network = "tcp"
	}
	var (
		path string
		host string
		alpn string
	)
	switch option.Network {
	case "ws":
		path = option.WSOpts.Path
		host = option.WSOpts.Headers["Host"]
		alpn = "http/1.1"
	case "grpc":
		path = option.GrpcOpts.GrpcServiceName
	case "h2":
		host = strings.Join(option.HTTP2Opts.Host, ",")
		path = option.HTTP2Opts.Path
		alpn = "h2"
	case "http":
		// TODO
	}
	s := &V2Ray{
		Ps:            option.Name,
		Add:           option.Server,
		Port:          strconv.Itoa(option.Port),
		ID:            option.UUID,
		Aid:           strconv.Itoa(option.AlterID),
		Net:           option.Network,
		Type:          "none", // FIXME
		Host:          host,
		SNI:           option.ServerName,
		Path:          path,
		AllowInsecure: option.SkipCertVerify,
		Alpn:          alpn,
		V:             "2",
		Protocol:      "vmess",
	}
	if option.TLS {
		s.TLS = "tls"
	}
	return s, nil
}
func ParseVlessURL(vless string) (data *V2Ray, err error) {
	u, err := url.Parse(vless)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "vless" || u.User == nil {
		return nil, fmt.Errorf("%w: expected vless://UUID@server:port", dialer.InvalidParameterErr)
	}
	if _, hasPassword := u.User.Password(); hasPassword {
		return nil, fmt.Errorf("%w: VLESS user info must contain only a UUID", dialer.InvalidParameterErr)
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid VLESS query: %v", dialer.InvalidParameterErr, err)
	}
	data = &V2Ray{
		Ps:            u.Fragment,
		Add:           u.Hostname(),
		Port:          u.Port(),
		ID:            u.User.Username(),
		Net:           q.Get("type"),
		Type:          q.Get("headerType"),
		SNI:           q.Get("sni"),
		Host:          q.Get("host"),
		Path:          q.Get("path"),
		TLS:           q.Get("security"),
		Flow:          q.Get("flow"),
		Encryption:    q.Get("encryption"),
		Fingerprint:   q.Get("fp"),
		PublicKey:     q.Get("pbk"),
		ShortID:       q.Get("sid"),
		SpiderX:       q.Get("spx"),
		Alpn:          q.Get("alpn"),
		AllowInsecure: common.StringToBool(q.Get("allowInsecure")),
		Protocol:      "vless",
	}
	if data.Net == "" {
		data.Net = "tcp"
	}
	if data.Net == "grpc" {
		data.Path = q.Get("serviceName")
	}
	if data.Type == "" {
		data.Type = "none"
	}
	if data.TLS == "" {
		data.TLS = "none"
	}
	if value := q.Get("udp"); value != "" {
		udp, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("%w: udp: %v", dialer.InvalidParameterErr, value)
		}
		data.UDP = &udp
	}
	return data, data.validateVLESS()
}

func ParseVmessURL(vmess string) (data *V2Ray, err error) {
	var info V2Ray
	// perform base64 decoding and unmarshal to VmessInfo
	raw, err := common.Base64StdDecode(vmess[8:])
	if err != nil {
		raw, err = common.Base64URLDecode(vmess[8:])
	}
	if err != nil {
		// not in json format, try to resolve as vmess://BASE64(Security:ID@Add:Port)?remarks=Ps&obfsParam=Host&Path=Path&obfs=Net&tls=TLS
		var u *url.URL
		u, err = url.Parse(vmess)
		if err != nil {
			return
		}
		re := regexp.MustCompile(`.*:(.+)@(.+):(\d+)`)
		s := strings.Split(vmess[8:], "?")[0]
		s, err = common.Base64StdDecode(s)
		if err != nil {
			s, err = common.Base64URLDecode(s)
		}
		subMatch := re.FindStringSubmatch(s)
		if subMatch == nil {
			err = fmt.Errorf("unrecognized vmess address")
			return
		}
		q := u.Query()
		ps := q.Get("remarks")
		if ps == "" {
			ps = q.Get("remark")
		}
		obfs := q.Get("obfs")
		obfsParam := q.Get("obfsParam")
		path := q.Get("path")
		if obfs == "kcp" || obfs == "mkcp" {
			m := make(map[string]string)
			//cater to v2rayN definition
			_ = jsoniter.Unmarshal([]byte(obfsParam), &m)
			path = m["seed"]
			obfsParam = ""
		}
		aid := q.Get("alterId")
		if aid == "" {
			aid = q.Get("aid")
		}
		info = V2Ray{
			ID:            subMatch[1],
			Add:           subMatch[2],
			Port:          subMatch[3],
			Ps:            ps,
			Host:          obfsParam,
			Path:          path,
			Net:           obfs,
			Aid:           aid,
			TLS:           map[string]string{"1": "tls"}[q.Get("tls")],
			AllowInsecure: common.StringToBool(q.Get("allowInsecure")),
		}
		if info.Net == "websocket" {
			info.Net = "ws"
		}
	} else {
		err = jsoniter.Unmarshal([]byte(raw), &info)
		if err != nil {
			return
		}
	}
	// correct the wrong vmess as much as possible
	if strings.HasPrefix(info.Host, "/") && info.Path == "" {
		info.Path = info.Host
		info.Host = ""
	}
	if info.Aid == "" {
		info.Aid = "0"
	}
	info.Protocol = "vmess"
	return &info, nil
}

func (s *V2Ray) ExportToURL() string {
	switch s.Protocol {
	case "vless":
		// https://github.com/XTLS/Xray-core/issues/91
		var query = make(url.Values)
		common.SetValue(&query, "type", s.Net)
		common.SetValue(&query, "security", s.TLS)
		common.SetValue(&query, "encryption", s.Encryption)
		common.SetValue(&query, "flow", s.Flow)
		common.SetValue(&query, "fp", s.Fingerprint)
		common.SetValue(&query, "pbk", s.PublicKey)
		common.SetValue(&query, "sid", s.ShortID)
		common.SetValue(&query, "spx", s.SpiderX)
		if s.UDP != nil {
			query.Set("udp", strconv.FormatBool(*s.UDP))
		}
		switch s.Net {
		case "websocket", "ws", "http", "h2":
			common.SetValue(&query, "path", s.Path)
			common.SetValue(&query, "host", s.Host)
		case "mkcp", "kcp":
			common.SetValue(&query, "headerType", s.Type)
			common.SetValue(&query, "seed", s.Path)
		case "tcp":
			common.SetValue(&query, "headerType", s.Type)
			common.SetValue(&query, "host", s.Host)
			common.SetValue(&query, "path", s.Path)
		case "grpc":
			common.SetValue(&query, "serviceName", s.Path)
		}
		//TODO: QUIC
		if s.TLS != "none" {
			common.SetValue(&query, "sni", s.SNI)
			common.SetValue(&query, "alpn", s.Alpn)
			common.SetValue(&query, "allowInsecure", common.BoolToString(s.AllowInsecure))
		}
		U := url.URL{
			Scheme:   "vless",
			User:     url.User(s.ID),
			Host:     net.JoinHostPort(s.Add, s.Port),
			RawQuery: query.Encode(),
			Fragment: s.Ps,
		}
		return U.String()
	case "vmess":
		s.V = "2"
		b, _ := jsoniter.Marshal(s)
		return "vmess://" + strings.TrimSuffix(base64.StdEncoding.EncodeToString(b), "=")
	}
	//log.Warn("unexpected protocol: %v", v.Protocol)
	return ""
}
