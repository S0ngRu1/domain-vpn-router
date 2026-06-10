package proxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"domain-vpn-router/internal/rules"
)

type Router interface {
	Route(ctx context.Context, target string) (rules.Match, error)
}

type Server struct {
	server       *http.Server
	router       Router
	directBindIP net.IP
	foreignProxy string
	mu           sync.RWMutex
}

func New(listen, directBindIP, foreignProxy string, router Router) *Server {
	s := &Server{
		router:       router,
		directBindIP: parseDirectBindIP(directBindIP),
		foreignProxy: strings.TrimSpace(foreignProxy),
	}
	s.server = &http.Server{
		Addr:              listen,
		Handler:           s,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

func (s *Server) SetDirectBindIP(directBindIP string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.directBindIP = parseDirectBindIP(directBindIP)
}

func (s *Server) ListenAndServe() error {
	err := s.server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	target := r.Host
	if r.Method == http.MethodConnect {
		target = r.URL.Host
	}
	match, err := s.router.Route(r.Context(), target)
	if err != nil {
		http.Error(w, "切换网络失败: "+err.Error(), http.StatusBadGateway)
		return
	}
	log.Printf("访问目标=%s 动作=%s 规则=%s", target, match.Action, match.Rule)

	if r.Method == http.MethodConnect {
		s.handleConnect(w, r, match)
		return
	}
	s.handleHTTP(w, r, match)
}

func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request, match rules.Match) {
	address := r.URL.Host
	if !strings.Contains(address, ":") {
		address += ":443"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	dst, err := s.dial(ctx, match.Action, address)
	if err != nil {
		http.Error(w, "连接目标失败: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer dst.Close()

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "当前响应不支持连接接管", http.StatusInternalServerError)
		return
	}
	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, "接管连接失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()

	if _, err := clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		return
	}
	errc := make(chan error, 2)
	go copyAndReport(errc, dst, clientConn)
	go copyAndReport(errc, clientConn, dst)
	<-errc
}

func (s *Server) handleHTTP(w http.ResponseWriter, r *http.Request, match rules.Match) {
	outReq := r.Clone(r.Context())
	outReq.RequestURI = ""
	outReq.Header.Del("Proxy-Connection")
	outReq.Header.Del("Connection")
	if outReq.URL.Scheme == "" {
		outReq.URL.Scheme = "http"
	}
	if outReq.URL.Host == "" {
		outReq.URL.Host = r.Host
	}

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				return s.dial(ctx, match.Action, address)
			},
		},
		Timeout: 0,
	}
	resp, err := client.Do(outReq)
	if err != nil {
		http.Error(w, "转发请求失败: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (s *Server) dial(ctx context.Context, action rules.Action, address string) (net.Conn, error) {
	bindIP := s.currentDirectBindIP()
	dialer := &net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	if action == rules.ActionDirect && bindIP != nil {
		dialer.LocalAddr = &net.TCPAddr{IP: bindIP}
	}
	if action == rules.ActionDirect {
		if resolved, err := s.resolveAddress(ctx, address, bindIP); err == nil {
			address = resolved
		} else {
			log.Printf("直连 DNS 解析失败，回退系统解析: address=%s err=%v", address, err)
		}
	}
	if action == rules.ActionForeign {
		if s.foreignProxy != "" {
			return s.dialHTTPProxy(ctx, s.foreignProxy, address)
		}
		if resolved, err := s.resolveAddress(ctx, address, bindIP); err == nil {
			address = resolved
		} else {
			log.Printf("外网 DNS 解析失败，回退系统解析: address=%s err=%v", address, err)
		}
	}
	return dialer.DialContext(ctx, "tcp", address)
}

func (s *Server) dialHTTPProxy(ctx context.Context, proxyAddr, target string) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	conn, err := dialer.DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	}
	req := "CONNECT " + target + " HTTP/1.1\r\nHost: " + target + "\r\nProxy-Connection: Keep-Alive\r\n\r\n"
	if _, err := io.WriteString(conn, req); err != nil {
		_ = conn.Close()
		return nil, err
	}
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_ = conn.Close()
		return nil, fmt.Errorf("上游代理 CONNECT 失败: %s", resp.Status)
	}
	_ = conn.SetDeadline(time.Time{})
	return &bufferedConn{Conn: conn, reader: reader}, nil
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	if c.reader != nil && c.reader.Buffered() > 0 {
		return c.reader.Read(p)
	}
	return c.Conn.Read(p)
}

func (s *Server) currentDirectBindIP() net.IP {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.directBindIP == nil {
		return nil
	}
	return append(net.IP(nil), s.directBindIP...)
}

func copyAndReport(errc chan<- error, dst io.Writer, src io.Reader) {
	_, err := io.Copy(dst, src)
	errc <- err
}

func localIPAvailable(ip net.IP) bool {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, addr := range addrs {
		var localIP net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			localIP = v.IP
		case *net.IPAddr:
			localIP = v.IP
		}
		if localIP != nil && localIP.Equal(ip) {
			return true
		}
	}
	return false
}

func (s *Server) resolveAddress(ctx context.Context, address string, bindIP net.IP) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", err
	}
	if net.ParseIP(host) != nil {
		return address, nil
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			dialer := &net.Dialer{Timeout: 3 * time.Second}
			if bindIP != nil {
				dialer.LocalAddr = &net.UDPAddr{IP: bindIP}
			}
			var lastErr error
			for _, dns := range []string{"223.5.5.5:53", "119.29.29.29:53", "114.114.114.114:53"} {
				conn, err := dialer.DialContext(ctx, "udp", dns)
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			return nil, lastErr
		},
	}
	ips, err := resolver.LookupIPAddr(lookupCtx, host)
	if err != nil {
		return "", err
	}
	for _, ip := range ips {
		if ip.IP.To4() != nil {
			return net.JoinHostPort(ip.IP.String(), port), nil
		}
	}
	if len(ips) > 0 {
		return net.JoinHostPort(ips[0].IP.String(), port), nil
	}
	return "", fmt.Errorf("没有可用 IP: %s", host)
}

func parseDirectBindIP(directBindIP string) net.IP {
	ip := net.ParseIP(strings.TrimSpace(directBindIP))
	if ip != nil && !localIPAvailable(ip) {
		log.Printf("直连绑定 IP %s 当前不可用，已回退到系统默认路由", ip)
		return nil
	}
	return ip
}
