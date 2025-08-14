// dnstest tries to do a DNS version of [net/http/httptest] package - which is useful inside unit tests.
package dnstest

import (
	"io"
	"log"
	"net"
	"strings"
	"sync"

	"github.com/miekg/dns"
)

type Server struct {
	sync.RWMutex // protects started

	f    dns.HandlerFunc
	addr net.Addr
	pc   net.PacketConn

	records []dns.RR
	started bool
}

// NewServer starts and returns a new [Server].
// The caller should call Close when finished to shut it down.
func NewServer(f dns.HandlerFunc) *Server {
	ts := NewUnstartedServer(f)
	ts.Start()
	return ts
}

// NewServerBind starts and returns a new [Server].
//
// This version takes bind-style definitions and uses a very simple (probably non-RFC-compliant) implementation.
// Which should be good for unit tests that need to test connectivity and simple responses from outside server.
//
// The caller should call Close when finished to shut it down.
func NewServerBind(input string) *Server {
	zp := dns.NewZoneParser(strings.NewReader(input), "", "")
	records := []dns.RR{}
	for rr, ok := zp.Next(); ok; rr, ok = zp.Next() {
		rr.Header().Name = strings.ToLower(rr.Header().Name)
		records = append(records, rr)
	}
	if err := zp.Err(); err != nil {
		panic(err)
	}

	ts := &Server{records: records}
	ts.f = ts.defaultHandler

	ts.Start()

	return ts
}

func (ts *Server) handleUDPPacket(buf *[]byte, size int, addr net.Addr) {
	defer bufPool.Put(buf)

	msg := msgPool.Get().(*dns.Msg)
	resetMsg(msg)
	defer msgPool.Put(msg)

	err := msg.Unpack((*buf)[:size])
	if err != nil {
		log.Printf("error handling UDP packet: %s", err)
		return
	}

	w := responseWriterPool.Get().(*udpResponseWriter)
	w.conn = ts.pc
	w.addr = addr
	w.localAddr = ts.addr
	defer responseWriterPool.Put(w)

	ts.f(w, msg)
}

func (w *udpResponseWriter) WriteMsg(resp *dns.Msg) error {
	addr := w.addr
	respBytes, err := resp.Pack()
	if err != nil {
		log.Printf("Failed to pack UDP response for %s: %s", addr, err)
		return err
	}

	n, err := w.conn.WriteTo(respBytes, addr)
	if n == 0 && isConnClosed(err) {
		log.Printf("Failed to send UDP response to %s: connection closed", addr)
		return err
	}
	if err != nil {
		log.Printf("Failed to send UDP response to %s: %s", addr, err)
		return err
	}
	if n != len(respBytes) {
		log.Printf("Failed to send UDP response to %s: conn.WriteTo() returned with %d != %d", addr, n, len(respBytes))
		return io.ErrShortWrite
	}
	return nil
}

func (ts *Server) defaultHandler(rw dns.ResponseWriter, req *dns.Msg) {
	resp := msgPool.Get().(*dns.Msg)
	resetMsg(resp)
	defer msgPool.Put(resp)

	resp.SetReply(req)

	if len(req.Question) != 1 {
		resp.SetRcodeFormatError(req)
		rw.WriteMsg(resp)
		return
	}

	qtype := req.Question[0].Qtype
	qname := strings.ToLower(req.Question[0].Name)

	// simple linear search, enough for a unit test server
	for _, rr := range ts.records {
		// we're strict with qname
		rrName := rr.Header().Name
		if qname != rrName {
			continue
		}

		// we're less strict with qtype
		if qtype == dns.TypeANY {
			resp.Answer = append(resp.Answer, rr)
			continue
		}

		rrType := rr.Header().Rrtype
		switch qtype {
		case dns.TypeA, dns.TypeAAAA, dns.TypeMX, dns.TypeTXT, dns.TypeSPF, dns.TypeNS, dns.TypeSRV, dns.TypeSOA:
			// simple append with type equality check
			if qtype != rrType {
				continue
			}

			resp.Answer = append(resp.Answer, rr)
		case dns.TypeCNAME:
			// if it's `dig CNAME cdn.example.com`, then just give CNAME and nothing else
			if qtype == rrType {
				resp.Answer = append(resp.Answer, rr)
				continue
			}
		}
	}

	rw.WriteMsg(resp)
}

// NewUnstartedServer returns a new [Server] but doesn't start it.
//
// Currently there is no reason to do this, as Server is not configurable at the moment.
//
// The caller should call Close when finished to shut it down.
func NewUnstartedServer(f dns.HandlerFunc) *Server {
	ts := &Server{
		f: f,
	}

	return ts
}

func (ts *Server) Start() {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	ts.pc = pc
	ts.addr = pc.LocalAddr()
	ts.goServe()
}

func (ts *Server) Close() {
	ts.Lock()
	ts.started = false
	ts.Unlock()
}

func (ts *Server) Addr() string {
	return ts.addr.String()
}

func (ts *Server) goServe() {
	ts.Lock()
	ts.started = true
	ts.Unlock()

	go func() {
		for {
			ts.RLock()
			started := ts.started
			ts.RUnlock()
			if !started {
				return
			}

			b := bufPool.Get().(*[]byte)
			n, addr, err := ts.pc.ReadFrom(*b)
			// documentation says to handle the packet even if err occurs, so do that first
			if n > 0 {
				go ts.handleUDPPacket(b, n, addr) // ignore errors
			} else {
				// Return buffer to pool if no data was read
				bufPool.Put(b)
			}
			if err != nil {
				log.Printf("got error when reading from UDP listen: %s", err)
			}
		}
	}()
}

// Checks if the error signals of a closed server connecting
func isConnClosed(err error) bool {
	if err == nil {
		return false
	}
	nerr, ok := err.(*net.OpError)
	if !ok {
		return false
	}

	if strings.Contains(nerr.Err.Error(), "use of closed network connection") {
		return true
	}

	return false
}

var bufPool = sync.Pool{New: func() any { b := make([]byte, dns.MaxMsgSize); return &b }}
var msgPool = sync.Pool{New: func() any { return &dns.Msg{} }}
var responseWriterPool = sync.Pool{New: func() any { return &udpResponseWriter{} }}

func resetMsg(msg *dns.Msg) { *msg = dns.Msg{} }

// udpResponseWriter implements dns.ResponseWriter for UDP packets
type udpResponseWriter struct {
	conn      net.PacketConn
	addr      net.Addr
	localAddr net.Addr
}

func (w *udpResponseWriter) Write(b []byte) (int, error) { return w.conn.WriteTo(b, w.addr) }
func (w *udpResponseWriter) Close() error                { return nil }
func (w *udpResponseWriter) TsigStatus() error           { return nil }
func (w *udpResponseWriter) TsigTimersOnly(bool)         {}
func (w *udpResponseWriter) Hijack()                     {}
func (w *udpResponseWriter) LocalAddr() net.Addr         { return w.localAddr }
func (w *udpResponseWriter) RemoteAddr() net.Addr        { return w.addr }
