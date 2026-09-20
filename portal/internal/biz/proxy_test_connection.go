package biz

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/sixath/framework/netx"
)

const (
	proxyTestTimeout        = 5 * time.Second
	proxyTestCONNECTTarget  = "example.com:443"
	proxyTestCONNECTRequest = "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n"
)

func testProxyConnection(ctx context.Context, meta *ProxyMeta) error {
	if meta == nil {
		return errors.New("proxy is required")
	}
	spec := netx.Spec{ID: meta.ID, Host: meta.Host, Type: meta.Type, Port: meta.Port}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, proxyTestTimeout)
	defer cancel()

	addr := net.JoinHostPort(meta.Host, strconv.Itoa(meta.Port))
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return redactProxyTestErr(meta, netx.AnnotateError(spec, err))
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	switch strings.ToLower(strings.TrimSpace(meta.Type)) {
	case netx.TypeHTTP:
		err = httpCONNECTHandshake(conn, meta)
	case netx.TypeSOCKS5:
		err = socks5Handshake(conn, meta)
	default:
		err = fmt.Errorf("unsupported proxy type %q", meta.Type)
	}
	if err != nil {
		return redactProxyTestErr(meta, netx.AnnotateError(spec, err))
	}
	return nil
}

func httpCONNECTHandshake(conn net.Conn, meta *ProxyMeta) error {
	var b strings.Builder
	b.WriteString(proxyTestCONNECTRequest)
	if meta.User != "" || meta.Password != "" {
		token := base64.StdEncoding.EncodeToString([]byte(meta.User + ":" + meta.Password))
		b.WriteString("Proxy-Authorization: Basic ")
		b.WriteString(token)
		b.WriteString("\r\n")
	}
	b.WriteString("\r\n")
	if _, err := io.WriteString(conn, b.String()); err != nil {
		return err
	}
	status, err := readHTTPStatus(conn)
	if err != nil {
		return err
	}
	if status != 200 {
		return fmt.Errorf("CONNECT %s returned %d", proxyTestCONNECTTarget, status)
	}
	return nil
}

func readHTTPStatus(r io.Reader) (int, error) {
	br := bufio.NewReader(r)
	line, err := br.ReadString('\n')
	if err != nil {
		return 0, err
	}
	line = strings.TrimRight(line, "\r\n")
	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 2 {
		return 0, fmt.Errorf("invalid status line")
	}
	code, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, fmt.Errorf("invalid status line")
	}
	return code, nil
}

func socks5Handshake(conn net.Conn, meta *ProxyMeta) error {
	if meta.User != "" || meta.Password != "" {
		return socks5UserPass(conn, meta.User, meta.Password)
	}
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return err
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil {
		return err
	}
	if reply[0] != 0x05 || reply[1] != 0x00 {
		return fmt.Errorf("socks5 handshake rejected")
	}
	return nil
}

func socks5UserPass(conn net.Conn, user, pass string) error {
	if len(user) > 255 || len(pass) > 255 {
		return errors.New("socks5 credentials too long")
	}
	if _, err := conn.Write([]byte{0x05, 0x01, 0x02}); err != nil {
		return err
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil {
		return err
	}
	if reply[0] != 0x05 || reply[1] != 0x02 {
		return fmt.Errorf("socks5 method rejected")
	}
	req := make([]byte, 0, 3+len(user)+len(pass))
	req = append(req, 0x01, byte(len(user)))
	req = append(req, user...)
	req = append(req, byte(len(pass)))
	req = append(req, pass...)
	if _, err := conn.Write(req); err != nil {
		return err
	}
	authReply := make([]byte, 2)
	if _, err := io.ReadFull(conn, authReply); err != nil {
		return err
	}
	if authReply[0] != 0x01 || authReply[1] != 0x00 {
		return errors.New("socks5 authentication failed")
	}
	return nil
}

func redactProxyTestErr(meta *ProxyMeta, err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if meta != nil && meta.Password != "" && strings.Contains(msg, meta.Password) {
		msg = strings.ReplaceAll(msg, meta.Password, "***")
	}
	return errors.New(msg)
}
