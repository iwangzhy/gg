package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mzz2017/gg/common"
	"github.com/mzz2017/gg/dialer"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

type ClashConfig struct {
	Proxy []yaml.Node `yaml:"proxies"`
}

type SIP008 struct {
	Version        int            `json:"version"`
	Servers        []SIP008Server `json:"servers"`
	BytesUsed      int64          `json:"bytes_used"`
	BytesRemaining int64          `json:"bytes_remaining"`
}

type SIP008Server struct {
	Id         string `json:"id"`
	Remarks    string `json:"remarks"`
	Server     string `json:"server"`
	ServerPort int    `json:"server_port"`
	Password   string `json:"password"`
	Method     string `json:"method"`
	Plugin     string `json:"plugin"`
	PluginOpts string `json:"plugin_opts"`
}

func resolveSubscriptionAsClash(log *logrus.Logger, opt *dialer.GlobalOption, b []byte) (dialers []*dialer.Dialer, err error) {
	log.Traceln("try to resolve as Clash")

	var conf ClashConfig
	if err = yaml.Unmarshal([]byte(decodeSubscription(b)), &conf); err != nil {
		return nil, err
	}
	if conf.Proxy == nil {
		return nil, fmt.Errorf("not a Clash subscription: missing proxies list")
	}
	for i, node := range conf.Proxy {
		d, e := dialer.NewFromClash(&node, opt)
		if e != nil {
			log.Warnf("Skipping Clash proxies[%v]: %v", i, e)
			continue
		}
		dialers = append(dialers, d)
	}
	if len(dialers) == 0 {
		return nil, fmt.Errorf("Clash subscription contains no supported proxy nodes")
	}
	return dialers, nil
}

func decodeSubscription(b []byte) string {
	raw := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(b)), "\ufeff"))
	if decoded, err := common.Base64StdDecode(raw); err == nil {
		return decoded
	}
	if decoded, err := common.Base64URLDecode(raw); err == nil {
		return decoded
	}
	// Clash 配置通常直接返回 YAML，不能把 Base64 解码失败当作空订阅。
	return raw
}

func resolveSubscriptionAsBase64(log *logrus.Logger, opt *dialer.GlobalOption, b []byte) (dialers []*dialer.Dialer) {
	log.Traceln("try to resolve as base64")

	raw := decodeSubscription(b)
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		d, e := GetDialerFromLink(line, opt, false, "")
		if e != nil {
			log.Tracef("%v: %v\n", e, line)
			continue
		}
		dialers = append(dialers, d)
	}
	return dialers
}

func resolveSubscriptionAsSIP008(log *logrus.Logger, opt *dialer.GlobalOption, b []byte) (dialers []*dialer.Dialer, err error) {
	log.Traceln("try to resolve as SIP008")

	var sip SIP008
	err = json.Unmarshal(b, &sip)
	if err != nil {
		return
	}
	if sip.Version != 1 || sip.Servers == nil {
		return nil, fmt.Errorf("does not seems like a SIP008 subscription")
	}
	for i, server := range sip.Servers {
		u := url.URL{
			Scheme:   "ss",
			User:     url.UserPassword(server.Method, server.Password),
			Host:     net.JoinHostPort(server.Server, strconv.Itoa(server.ServerPort)),
			RawQuery: url.Values{"plugin": []string{server.PluginOpts}}.Encode(),
			Fragment: server.Remarks,
		}
		d, e := dialer.NewFromLink("shadowsocks", u.String(), opt)
		if e != nil {
			log.Tracef("servers[%v]: %v\n", i, e)
			continue
		}
		dialers = append(dialers, d)
	}
	if len(dialers) == 0 {
		return nil, fmt.Errorf("SIP008 subscription contains no supported proxy nodes")
	}
	return
}

func pullDialersFromSubscription(log *logrus.Logger, opt *dialer.GlobalOption, subscription string) (dialers []*dialer.Dialer, err error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(subscription)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("subscription request failed: %s", resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if dialers, err = resolveSubscriptionAsSIP008(log, opt, b); err == nil {
		return dialers, nil
	} else {
		log.Traceln(err)
	}
	if dialers, err = resolveSubscriptionAsClash(log, opt, b); err == nil {
		return dialers, nil
	} else {
		log.Traceln(err)
	}
	if dialers = resolveSubscriptionAsBase64(log, opt, b); len(dialers) > 0 {
		return dialers, nil
	}
	return nil, fmt.Errorf("subscription contains no supported proxy nodes (expected Clash proxies, SIP008, or share links)")
}
