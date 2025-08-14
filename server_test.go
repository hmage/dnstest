package dnstest_test

import (
	"fmt"
	"net"
	"sync"
	"testing"

	"github.com/hmage/dnstest"
	"github.com/miekg/dns"
)

// not using testBind inside ExampleServer() because pkg.go.dev will trim out this const - needs to be self-sufficient
const testBind = "example.com. 104 A 127.0.0.1\nexample.com. 104 MX 10 mail.example.com."

func ExampleServer() {
	ts := dnstest.NewServerBind("example.com. 104 A 127.0.0.1\nexample.com. 104 MX 10 mail.example.com.")
	defer ts.Close()

	q := dns.Msg{}
	q.SetQuestion("example.com.", dns.TypeA)
	resp, err := dns.Exchange(&q, ts.Addr())
	if err != nil {
		panic(err)
	}

	for _, rr := range resp.Answer {
		fmt.Printf("%s\n", rr.String())
	}
	// Output: example.com.	104	IN	A	127.0.0.1
}

func TestRoundtrip(t *testing.T) {
	ts := dnstest.NewServerBind(testBind)
	defer ts.Close()

	testBody(ts, t)
}

func testBody(ts *dnstest.Server, t *testing.T) {
	t.Helper()

	q := &dns.Msg{}
	q.SetQuestion("example.com.", dns.TypeA)

	resp, err := dns.Exchange(q, ts.Addr())
	if err != nil {
		t.Fatal(err)
	}

	if resp == nil {
		t.Fatal("Failed to run DNS query: resp == nil")
	}

	if len(resp.Answer) != 1 {
		t.Fatalf("Unexpected Answer count: %d", len(resp.Answer))
	}

	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("DNS error: %s", dns.RcodeToString[resp.Rcode])
	}

	rr := resp.Answer[0]
	rrA, ok := rr.(*dns.A)
	if !ok {
		t.Fatalf("Answer isn't A: %T", rr)
	}

	if rrA.Hdr.Name != "example.com." {
		t.Fatalf("Answer is A but not example.com: %q", rrA.Hdr.Name)
	}

	if !rrA.A.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Fatalf("Answer is A but not 127.0.0.1: %q", rrA.A)
	}

	if rrA.Hdr.Ttl != 104 {
		t.Fatalf("Answer is A but TTL isn't 104: %d", rrA.Hdr.Ttl)
	}
}

func TestRoundtripHandler(t *testing.T) {
	handler := func(w dns.ResponseWriter, req *dns.Msg) {
		resp := &dns.Msg{}
		resp.SetReply(req)
		rr, err := dns.NewRR("example.com. 104 A 127.0.0.1")
		if err != nil {
			t.Fatalf("Failed to create new RR: %s", err)
		}
		resp.Answer = append(resp.Answer, rr)
		w.WriteMsg(resp)
	}

	ts := dnstest.NewServer(handler)
	defer ts.Close()

	testBody(ts, t)
}

func benchPrep(tb testing.TB, addr string) net.Conn {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		tb.Fatalf("Failed to resolve UDP address %q: %s", addr, err)
	}
	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		tb.Fatalf("failed to dial DNS server: %s", err)
	}
	return conn
}

func benchBody(tb testing.TB, conn net.Conn) {
	m := msgPool.Get().(*dns.Msg)
	resetMsg(m)
	defer msgPool.Put(m)
	m.SetQuestion("example.com.", dns.TypeA)
	m.RecursionDesired = true

	data, err := m.Pack()
	if err != nil {
		tb.Fatalf("Failed to pack DNS message: %s", err)
	}

	_, err = conn.Write(data)
	if err != nil {
		tb.Fatalf("Failed to write to DNS server: %s", err)
	}

	b := bufPool.Get().(*[]byte)
	defer bufPool.Put(b)
	_, err = conn.Read(*b)
	if err != nil {
		tb.Fatalf("Failed to read from DNS server: %s", err)
	}
}

func benchRPS(b *testing.B) {
	if elapsed := b.Elapsed(); elapsed > 0 {
		b.ReportMetric(float64(b.N)/elapsed.Seconds(), "RPS")
	}
}

func BenchmarkRPS(b *testing.B) {
	ts := dnstest.NewServerBind(testBind)
	defer ts.Close()
	addr := ts.Addr()

	// "single-thread" with single goroutine
	b.Run("p=0", func(b *testing.B) {
		conn := benchPrep(b, addr)
		defer conn.Close()
		for b.Loop() {
			benchBody(b, conn)
		}
		benchRPS(b)
	})

	// "multi-thread" with parallelism*GOMAXPROCS goroutines, this emulates load higher than numCPU
	var parallelism = []int{1, 2, 4, 8, 16}
	for _, i := range parallelism {
		b.Run(fmt.Sprintf("p=%d", i), func(b *testing.B) {
			b.SetParallelism(i)
			b.RunParallel(func(pb *testing.PB) {
				conn := benchPrep(b, addr)
				defer conn.Close()
				for pb.Next() {
					benchBody(b, conn)
				}
			})
			benchRPS(b)
		})
	}
}

var bufPool = sync.Pool{New: func() any { b := make([]byte, dns.MaxMsgSize); return &b }}
var msgPool = sync.Pool{New: func() any { return &dns.Msg{} }}

func resetMsg(msg *dns.Msg) { *msg = dns.Msg{} }
