package proxy

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
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
}

func New(listen, directBindIP string, router Router) *Server {
	s := &Server{
		router:       router,
		directBindIP: net.ParseIP(strings.TrimSpace(directBindIP)),
	}
	s.server = &http.Server{
		Addr:              listen,
		Handler:           s,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
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
	dialer := &net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	if action == rules.ActionDirect && s.directBindIP != nil {
		dialer.LocalAddr = &net.TCPAddr{IP: s.directBindIP}
	}
	return dialer.DialContext(ctx, "tcp", address)
}

func copyAndReport(errc chan<- error, dst io.Writer, src io.Reader) {
	_, err := io.Copy(dst, src)
	errc <- err
}
