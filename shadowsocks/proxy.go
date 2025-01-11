package shadowsocks

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"

	"github.com/shadowsocks/go-shadowsocks2/core"
	"github.com/shadowsocks/go-shadowsocks2/socks"
	"golang.org/x/net/proxy"
)

func init() {
	proxy.RegisterDialerType("ss", New)
}

type ShadowSocks struct {
	URL       *url.URL
	Cipher    core.Cipher
	DialProxy func(context.Context, string, string) (net.Conn, error)
}

func (s *ShadowSocks) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	target := socks.ParseAddr(addr)
	if target == nil {
		return nil, fmt.Errorf("failed to parse address %s", target)
	}

	conn, err := s.DialProxy(ctx, network, s.URL.Host)
	if err != nil {
		return nil, err
	}
	conn = s.Cipher.StreamConn(conn)
	if _, err := conn.Write(target); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func (s *ShadowSocks) Dial(network, addr string) (net.Conn, error) {
	return s.DialContext(context.Background(), network, addr)
}

func New(url *url.URL, forward proxy.Dialer) (proxy.Dialer, error) {
	dialer := new(ShadowSocks)

	if forward != nil {
		if f, ok := forward.(proxy.ContextDialer); ok {
			dialer.DialProxy = func(ctx context.Context, s1, s2 string) (net.Conn, error) {
				return f.DialContext(ctx, s1, s2)
			}
		} else {
			return nil, errors.New("forward dialer does not implement proxy.ContextDialer")
		}
	} else {
		dialer.DialProxy = func(ctx context.Context, s1, s2 string) (net.Conn, error) {
			d := new(net.Dialer)
			return d.DialContext(ctx, s1, s2)
		}
	}

	algo := url.User.Username()
	if len(algo) == 0 {
		algo = "CHACHA20-IETF-POLY1305"
	}

	password, _ := url.User.Password()

	cipher, err := core.PickCipher(algo, nil, password)
	if err != nil {
		return nil, err
	}

	dialer.Cipher = cipher
	dialer.URL = url

	return dialer, nil
}
