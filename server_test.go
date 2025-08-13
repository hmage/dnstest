package dnstest_test

import (
	"fmt"
	"net"
	"testing"

	"github.com/hmage/dnstest"
	"github.com/miekg/dns"
)

const testBind = "example.com. 104 A 127.0.0.1\nexample.com. 104 MX 10 mail.example.com."

func ExampleServer() {
	ts := dnstest.NewServerBind(testBind)
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

	q := dns.Msg{}
	q.SetQuestion("example.com.", dns.TypeA)
	resp, err := dns.Exchange(&q, ts.Addr())
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

func benchPrep(tb testing.TB, addr string) (*dns.Client, *dns.Conn) {
	client := &dns.Client{}
	conn, err := client.Dial(addr)
	if err != nil {
		tb.Fatalf("failed to dial DNS server: %s", err)
	}
	return client, conn
}

func benchBody(tb testing.TB, client *dns.Client, conn *dns.Conn) {
	m := &dns.Msg{}
	m.SetQuestion("example.com.", dns.TypeA)
	m.RecursionDesired = true

	_, _, err := client.ExchangeWithConn(m, conn)
	if err != nil {
		tb.Fatalf("Failed to exchange with DNS server: %s", err)
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
		client, conn := benchPrep(b, addr)
		defer conn.Close()
		for b.Loop() {
			benchBody(b, client, conn)
		}
		benchRPS(b)
	})

	// "multi-thread" with parallelism*GOMAXPROCS goroutines, this emulates load higher than numCPU
	var parallelism = []int{1, 2, 4, 8, 16}
	for _, i := range parallelism {
		b.Run(fmt.Sprintf("p=%d", i), func(b *testing.B) {
			b.SetParallelism(i)
			b.RunParallel(func(pb *testing.PB) {
				client, conn := benchPrep(b, addr)
				defer conn.Close()
				for pb.Next() {
					benchBody(b, client, conn)
				}
			})
			benchRPS(b)
		})
	}
}
