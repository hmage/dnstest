// dnstest tries to do a DNS version of [net/http/httptest] package - which is useful inside unit tests.
package dnstest

import (
	"net"
	"strings"

	"github.com/miekg/dns"
)

type Server struct {
	f    dns.HandlerFunc
	s    dns.Server
	addr net.Addr
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

	ts := NewServer(func(rw dns.ResponseWriter, req *dns.Msg) {
		resp := &dns.Msg{}
		resp.SetReply(req)

		if len(req.Question) != 1 {
			resp.SetRcodeFormatError(req)
			rw.WriteMsg(resp)
			return
		}

		qtype := req.Question[0].Qtype
		qname := strings.ToLower(req.Question[0].Name)

		// simple linear search, enough for a unit test server
		for _, rr := range records {
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
	})

	return ts
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
	ts.s.Handler = f

	return ts
}

func (ts *Server) Start() {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	ts.s.PacketConn = pc
	ts.addr = pc.LocalAddr()
	ts.goServe()
}

func (ts *Server) Close() {
	err := ts.s.Shutdown()
	if err != nil {
		panic(err)
	}
}

func (ts *Server) Addr() string {
	return ts.addr.String()
}

func (ts *Server) goServe() {
	go func() {
		err := ts.s.ActivateAndServe()
		if err != nil {
			panic(err)
		}
	}()
}
