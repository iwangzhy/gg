package dialer

import (
	"context"
	"fmt"
	"net"

	"github.com/daeuniverse/outbound/netproxy"
	"golang.org/x/net/proxy"
)

// ToOutbound 保留现有传输层，适配后继协议库的连接接口。
func ToOutbound(d proxy.Dialer) netproxy.Dialer { return &outboundTransport{d} }

type outboundTransport struct{ proxy.Dialer }

func (d *outboundTransport) DialContext(ctx context.Context, network, addr string) (netproxy.Conn, error) {
	parsed, err := netproxy.ParseMagicNetwork(network)
	if err != nil {
		return nil, err
	}
	cd := ContextDialer{Dialer: d.Dialer}
	return cd.DialContext(ctx, parsed.Network, addr)
}

func FromOutbound(d netproxy.Dialer) proxy.Dialer { return &outboundProxy{d} }

type outboundProxy struct{ netproxy.Dialer }

func (d *outboundProxy) Dial(network, addr string) (net.Conn, error) {
	return d.DialContext(context.Background(), network, addr)
}

func (d *outboundProxy) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	conn, err := d.Dialer.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	if network == "udp" {
		packet, ok := conn.(netproxy.PacketConn)
		if !ok {
			_ = conn.Close()
			return nil, fmt.Errorf("proxy connection does not support UDP")
		}
		return netproxy.NewFakeNetPacketConn(packet, &net.UDPAddr{}, &net.UDPAddr{}), nil
	}
	return &netproxy.FakeNetConn{Conn: conn, LAddr: &net.TCPAddr{}, RAddr: &net.TCPAddr{}}, nil
}
