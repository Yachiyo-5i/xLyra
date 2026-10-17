package httpclient

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
)

// TLSFingerprintClaudeCode makes outgoing TLS handshakes look like the ones
// produced by the Node.js runtime that the real Claude Code CLI runs on.
const TLSFingerprintClaudeCode = "claude_code"

type dialContextFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// nodeClientHelloCipherSuites mirrors the cipher suite order of Node.js 24 (JA3
// 44f88fca027f27bab4bb08d4af15f23e). The order is part of the fingerprint.
var nodeClientHelloCipherSuites = []uint16{
	0x1301, 0x1302, 0x1303, // TLS 1.3
	0xc02b, 0xc02f, 0xc02c, 0xc030, // ECDHE + AES-GCM
	0xcca9, 0xcca8, // ECDHE + ChaCha20-Poly1305
	0xc009, 0xc013, 0xc00a, 0xc014, // ECDHE + AES-CBC-SHA
	0x009c, 0x009d, // RSA + AES-GCM
	0x002f, 0x0035, // RSA + AES-CBC-SHA
}

var nodeClientHelloCurves = []utls.CurveID{utls.X25519, utls.CurveP256, utls.CurveP384}

var nodeClientHelloSignatureSchemes = []utls.SignatureScheme{
	0x0403, 0x0804, 0x0401, 0x0503, 0x0805, 0x0501, 0x0806, 0x0601, 0x0201,
}

// nodeClientHelloSpec builds the ClientHello that Node.js 24 sends. Extension
// order matters for JA3/JA4, so it is spelled out explicitly.
func nodeClientHelloSpec() *utls.ClientHelloSpec {
	return &utls.ClientHelloSpec{
		CipherSuites:       nodeClientHelloCipherSuites,
		CompressionMethods: []uint8{0},
		Extensions: []utls.TLSExtension{
			&utls.SNIExtension{},
			&utls.GREASEEncryptedClientHelloExtension{},
			&utls.ExtendedMasterSecretExtension{},
			&utls.RenegotiationInfoExtension{},
			&utls.SupportedCurvesExtension{Curves: nodeClientHelloCurves},
			&utls.SupportedPointsExtension{SupportedPoints: []uint8{0}},
			&utls.SessionTicketExtension{},
			&utls.ALPNExtension{AlpnProtocols: []string{"http/1.1"}},
			&utls.StatusRequestExtension{},
			&utls.SignatureAlgorithmsExtension{SupportedSignatureAlgorithms: nodeClientHelloSignatureSchemes},
			&utls.SCTExtension{},
			&utls.KeyShareExtension{KeyShares: []utls.KeyShare{{Group: utls.X25519}}},
			&utls.PSKKeyExchangeModesExtension{Modes: []uint8{utls.PskModeDHE}},
			&utls.SupportedVersionsExtension{Versions: []uint16{utls.VersionTLS13, utls.VersionTLS12}},
		},
		TLSVersMax: utls.VersionTLS13,
		TLSVersMin: utls.VersionTLS10,
	}
}

// fingerprintedTLSDialer dials the raw connection with base (which already
// handles proxies) and then performs the handshake with the Node.js hello.
func fingerprintedTLSDialer(base dialContextFunc) dialContextFunc {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := base(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		host, _, splitErr := net.SplitHostPort(addr)
		if splitErr != nil {
			host = addr
		}
		client := utls.UClient(conn, &utls.Config{ServerName: host}, utls.HelloCustom)
		if err := client.ApplyPreset(nodeClientHelloSpec()); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("apply tls fingerprint: %w", err)
		}
		if err := client.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("tls handshake: %w", err)
		}
		return client, nil
	}
}

// httpConnectDialer returns a dialer that tunnels through an HTTP(S) proxy using
// CONNECT. It is used instead of http.Transport.Proxy because the transport only
// honours DialTLSContext for direct connections.
func httpConnectDialer(proxyURL *url.URL, direct *net.Dialer) dialContextFunc {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		proxyAddr := proxyURL.Host
		if proxyURL.Port() == "" {
			port := "80"
			if strings.EqualFold(proxyURL.Scheme, "https") {
				port = "443"
			}
			proxyAddr = net.JoinHostPort(proxyURL.Hostname(), port)
		}
		conn, err := direct.DialContext(ctx, "tcp", proxyAddr)
		if err != nil {
			return nil, fmt.Errorf("connect to proxy: %w", err)
		}
		if strings.EqualFold(proxyURL.Scheme, "https") {
			tlsConn := utls.UClient(conn, &utls.Config{ServerName: proxyURL.Hostname()}, utls.HelloGolang)
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				_ = conn.Close()
				return nil, fmt.Errorf("tls handshake with proxy: %w", err)
			}
			conn = tlsConn
		}

		request := &http.Request{
			Method: http.MethodConnect,
			URL:    &url.URL{Opaque: addr},
			Host:   addr,
			Header: http.Header{},
		}
		if proxyURL.User != nil {
			password, _ := proxyURL.User.Password()
			credentials := base64.StdEncoding.EncodeToString([]byte(proxyURL.User.Username() + ":" + password))
			request.Header.Set("Proxy-Authorization", "Basic "+credentials)
		}
		if deadline, ok := ctx.Deadline(); ok {
			_ = conn.SetDeadline(deadline)
		} else {
			_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
		}
		if err := request.Write(conn); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("write proxy CONNECT: %w", err)
		}
		reader := bufio.NewReader(conn)
		response, err := http.ReadResponse(reader, request)
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("read proxy CONNECT response: %w", err)
		}
		if response.StatusCode != http.StatusOK {
			_ = conn.Close()
			return nil, fmt.Errorf("proxy CONNECT failed: %s", response.Status)
		}
		_ = conn.SetDeadline(time.Time{})
		if reader.Buffered() > 0 {
			return &bufferedConn{Conn: conn, reader: reader}, nil
		}
		return conn, nil
	}
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.reader.Read(p) }
