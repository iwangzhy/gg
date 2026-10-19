package grpc

import (
	"context"
	"net"

	outgrpc "github.com/daeuniverse/outbound/transport/grpc"
	"github.com/mzz2017/gg/dialer"
	"golang.org/x/net/proxy"
)

type Dialer struct {
	NextDialer    proxy.Dialer
	ServiceName   string
	ServerName    string
	AllowInsecure bool
}

func (d *Dialer) Dial(network, addr string) (net.Conn, error) {
	return d.DialContext(context.Background(), network, addr)
}

func (d *Dialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	// 新旧协议库的 gRPC 消息同名，进程内只能注册一份；这里统一使用后继库。
	g := outgrpc.Dialer{
		NextDialer:    dialer.ToOutbound(d.NextDialer),
		ServiceName:   d.ServiceName,
		ServerName:    d.ServerName,
		AllowInsecure: d.AllowInsecure,
	}
	cd := dialer.ContextDialer{Dialer: dialer.FromOutbound(&g)}
	return cd.DialContext(ctx, network, addr)
}
