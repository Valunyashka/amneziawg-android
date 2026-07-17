package main

import (
	"net"
	"net/netip"
	"sync"
	"syscall"

	"github.com/amnezia-vpn/amneziawg-go/conn"
	"github.com/amnezia-vpn/amneziawg-go/tun/netstack"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
)

type ProxyBind struct {
	net *netstack.Net
	mu  sync.Mutex
	udp *gonet.UDPConn
}

var _ conn.Bind = (*ProxyBind)(nil)

func NewProxyBind(net *netstack.Net) *ProxyBind {
	return &ProxyBind{net: net}
}

func (b *ProxyBind) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.udp != nil {
		return nil, 0, conn.ErrBindAlreadyOpen
	}

	udpConn, err := b.net.DialUDPAddrPort(netip.AddrPortFrom(netip.AddrFrom4([4]byte{0, 0, 0, 0}), port), netip.AddrPort{})
	if err != nil {
		return nil, 0, err
	}
	b.udp = udpConn

	fn := func(packets [][]byte, sizes []int, eps []conn.Endpoint) (n int, err error) {
		for i := range packets {
			nBytes, addr, err := udpConn.ReadFrom(packets[i])
			if err != nil {
				return i, err
			}
			sizes[i] = nBytes

			udpAddr := addr.(*net.UDPAddr)
			ep, err := b.ParseEndpoint(udpAddr.String())
			if err != nil {
				return i, err
			}
			eps[i] = ep
			n++
		}
		return n, nil
	}

	return []conn.ReceiveFunc{fn}, port, nil
}

func (b *ProxyBind) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.udp != nil {
		err := b.udp.Close()
		b.udp = nil
		return err
	}
	return nil
}

func (b *ProxyBind) SetMark(mark uint32) error {
	return nil
}

func (b *ProxyBind) Send(bufs [][]byte, ep conn.Endpoint) error {
	b.mu.Lock()
	c := b.udp
	b.mu.Unlock()

	if c == nil {
		return net.ErrClosed
	}

	stdEp, ok := ep.(*ProxyEndpoint)
	if !ok {
		return conn.ErrWrongEndpointType
	}

	for _, buf := range bufs {
		_, err := c.WriteTo(buf, &net.UDPAddr{
			IP:   stdEp.DstIP().AsSlice(),
			Port: int(stdEp.AddrPort.Port()),
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (b *ProxyBind) ParseEndpoint(s string) (conn.Endpoint, error) {
	e, err := netip.ParseAddrPort(s)
	if err != nil {
		return nil, err
	}
	return &ProxyEndpoint{
		AddrPort: e,
	}, nil
}

func (b *ProxyBind) BatchSize() int {
	return 1
}

type ProxyEndpoint struct {
	netip.AddrPort
}

func (e *ProxyEndpoint) ClearSrc() {}
func (e *ProxyEndpoint) SrcToString() string { return "" }
func (e *ProxyEndpoint) DstToString() string { return e.AddrPort.String() }
func (e *ProxyEndpoint) DstToBytes() []byte {
	b, _ := e.AddrPort.MarshalBinary()
	return b
}
func (e *ProxyEndpoint) DstIP() netip.Addr { return e.AddrPort.Addr() }
func (e *ProxyEndpoint) SrcIP() netip.Addr { return netip.Addr{} }

func (b *ProxyBind) PeekLookAtSocketFd4() (int, error) {
    return -1, syscall.ENOTSUP
}

func (b *ProxyBind) PeekLookAtSocketFd6() (int, error) {
    return -1, syscall.ENOTSUP
}
