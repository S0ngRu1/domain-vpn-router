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

// companyIPer 是可选接口：若 Router 实现了它，公司流量会显式绑定到 GP 网卡 IP，
// 避免 Mihomo TUN 路由优先级更高时把公司流量错误地带进 Tyty 隧道。
type companyIPer interface {
	CompanyAdapterIP() net.IP
}

type Server struct {
	server          *http.Server
	router          Router
	directBindIPStr string // 配置的静态 IP（空 = 自动检测）
	foreignProxy    string
	excludeAdapters []string // 动态检测时排除的网卡名称关键词
	physicalIPCache net.IP
	mu              sync.RWMutex
}

func New(listen, directBindIP, foreignProxy string, router Router, excludeAdapters []string) *Server {
	s := &Server{
		router:          router,
		directBindIPStr: strings.TrimSpace(directBindIP),
		foreignProxy:    strings.TrimSpace(foreignProxy),
		excludeAdapters: excludeAdapters,
	}
	s.refreshPhysicalIPLocked()
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
	s.directBindIPStr = strings.TrimSpace(directBindIP)
	s.refreshPhysicalIPLocked()
}

func (s *Server) RefreshPhysicalIP() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshPhysicalIPLocked()
	if s.physicalIPCache == nil {
		return ""
	}
	return s.physicalIPCache.String()
}

func (s *Server) CurrentPhysicalIPStr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.physicalIPCache == nil {
		return ""
	}
	return s.physicalIPCache.String()
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
	bindIP := s.currentPhysicalIP()
	companyIP := net.IP(nil)
	if action == rules.ActionCompany {
		if r, ok := s.router.(companyIPer); ok {
			companyIP = r.CompanyAdapterIP()
		}
	}
	action = effectiveDialAction(action, companyIP)
	dialer := &net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	if action == rules.ActionDirect && bindIP != nil {
		dialer.LocalAddr = &net.TCPAddr{IP: bindIP}
	}
	if action == rules.ActionCompany {
		if companyIP != nil {
			dialer.LocalAddr = &net.TCPAddr{IP: companyIP}
		}
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

func effectiveDialAction(action rules.Action, companyIP net.IP) rules.Action {
	if action == rules.ActionCompany && companyIP == nil {
		return rules.ActionDirect
	}
	return action
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

// currentPhysicalIP 返回启动或配置变更时缓存的物理网卡 IP，避免请求路径反复枚举网卡。
func (s *Server) currentPhysicalIP() net.IP {
	s.mu.RLock()
	cached := cloneIP(s.physicalIPCache)
	s.mu.RUnlock()
	return cached
}

func (s *Server) refreshPhysicalIPLocked() {
	if ip := parseDirectBindIP(s.directBindIPStr); ip != nil {
		s.physicalIPCache = cloneIP(ip)
		return
	}
	s.physicalIPCache = cloneIP(dynamicPhysicalIP(s.excludeAdapters))
}

func cloneIP(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}
	out := make(net.IP, len(ip))
	copy(out, ip)
	return out
}

// DynamicPhysicalIPStr 供外部查询当前动态检测到的物理网卡 IP（用于 GUI 显示）。
func DynamicPhysicalIPStr(excludeAdapters []string) string {
	if ip := dynamicPhysicalIP(excludeAdapters); ip != nil {
		return ip.String()
	}
	return ""
}

// dynamicPhysicalIP 枚举网卡，排除回环、VPN/虚拟适配器，返回第一个可用的物理 IPv4。
func dynamicPhysicalIP(excludeKeywords []string) net.IP {
	exclude := make([]string, 0, len(excludeKeywords)+4)
	for _, kw := range excludeKeywords {
		if kw = strings.ToLower(strings.TrimSpace(kw)); kw != "" {
			exclude = append(exclude, kw)
		}
	}
	for _, kw := range []string{"wsl", "vethernet", "bluetooth", "loopback"} {
		exclude = append(exclude, kw)
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := strings.ToLower(iface.Name)
		skip := false
		for _, kw := range exclude {
			if strings.Contains(name, kw) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			ip, _, err := net.ParseCIDR(addr.String())
			if err == nil && ip.To4() != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
				return ip
			}
		}
	}
	return nil
}

func copyAndReport(errc chan<- error, dst io.Writer, src io.Reader) {
	_, err := io.Copy(dst, src)
	errc <- err
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
	return net.ParseIP(strings.TrimSpace(directBindIP))
}
