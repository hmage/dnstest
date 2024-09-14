[![](https://godoc.org/github.com/hmage/dnstest?status.svg)](https://godoc.org/github.com/hmage/dnstest)

# dnstest

This is a spiritual equivalent of Go's `net/http/httptest` package - a small server that you can boot up during unit tests when you need to talk to an outside DNS server but want everything to be local.

## Example

Here's how you can use `dnstest` in your tests:

```go
package main

import (
    "fmt"

    "github.com/hmage/dnstest"
    "github.com/miekg/dns"
)

func main() {
    ts := dnstest.NewServerBind("example.com. 104 A 127.0.0.1\nexample.com. 104 MX 10 mail.example.com.")
    defer ts.Close()

    q := dns.Msg{}
    q.SetQuestion("example.com.", dns.TypeA)
    resp, err := dns.Exchange(&q, ts.Addr())
    if err != nil {
        panic(err)
    }

    fmt.Println(resp)
}
```

Output:
```
;; opcode: QUERY, status: NOERROR, id: 15858
;; flags: qr rd; QUERY: 1, ANSWER: 1, AUTHORITY: 0, ADDITIONAL: 0

;; QUESTION SECTION:
;example.com.   IN  A

;; ANSWER SECTION:
example.com.    104 IN  A   127.0.0.1
```

## Notes

- If you use `NewServerBind()`, it generates a _very_ simple DNS handler, which should be good enough to talk to during unit tests.
- `NewServerBind()` uses linear search when matching records, so don't include millions of records unnecessarily; otherwise, your tests will be slower.
- Only basic record types like A, AAAA, MX, TXT, SPF, NS, SRV, and SOA are supported.

## Contributing

Contributions are welcome! Feel free to open issues or submit pull requests.

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.
