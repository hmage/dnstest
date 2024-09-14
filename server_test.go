package dnstest_test

import (
	"fmt"
	"net"
	"testing"

	"github.com/hmage/dnstest"
	"github.com/miekg/dns"
)

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
	ts := dnstest.NewServerBind("example.com. 104 A 127.0.0.1\nexample.com. 104 MX 10 mail.example.com.")
	defer ts.Close()

	q := dns.Msg{}
	q.SetQuestion("example.com.", dns.TypeA)
	resp, err := dns.Exchange(&q, ts.Addr())
	if err != nil {
		t.Fatal(err)
	}

	if len(resp.Answer) != 1 {
		t.Fatalf("Unexpected Answer count: %d", len(resp.Answer))
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
