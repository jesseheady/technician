package check

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/m0nkey/technician/internal/config"
)

type DNSChecker struct {
	mu        sync.Mutex
	resolvers map[string]*net.Resolver // keyed by DNS server address
}

func NewDNSChecker() *DNSChecker {
	return &DNSChecker{
		resolvers: make(map[string]*net.Resolver),
	}
}

// getResolver returns a cached *net.Resolver for the given DNS server address,
// reusing the resolver across check runs to benefit from connection reuse.
func (p *DNSChecker) getResolver(server string) *net.Resolver {
	p.mu.Lock()
	defer p.mu.Unlock()
	if r, ok := p.resolvers[server]; ok {
		return r
	}
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{}
			return d.DialContext(ctx, "udp", server)
		},
	}
	p.resolvers[server] = r
	return r
}

func (p *DNSChecker) Type() config.CheckType {
	return config.CheckTypeDNS
}

func (p *DNSChecker) Run(ctx context.Context, cfg *config.CheckConfig, site *config.Site) *Result {
	result := NewResult(cfg.Name, config.CheckTypeDNS, site)

	if cfg.DNS == nil {
		result.Error = "missing DNS check configuration"
		return result
	}

	dcfg := cfg.DNS
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resolver := p.getResolver(dcfg.Server)

	domain := dcfg.Domain
	recordType := strings.ToUpper(dcfg.RecordType)

	var answers []string
	var queryErr error

	start := time.Now()

	switch recordType {
	case "A":
		var addrs []string
		addrs, queryErr = resolver.LookupHost(ctx, domain)
		if queryErr == nil {
			for _, addr := range addrs {
				if net.ParseIP(addr) != nil && strings.Contains(addr, ".") {
					answers = append(answers, addr)
				}
			}
		}

	case "AAAA":
		var addrs []string
		addrs, queryErr = resolver.LookupHost(ctx, domain)
		if queryErr == nil {
			for _, addr := range addrs {
				if net.ParseIP(addr) != nil && strings.Contains(addr, ":") {
					answers = append(answers, addr)
				}
			}
		}

	case "MX":
		var mxs []*net.MX
		mxs, queryErr = resolver.LookupMX(ctx, domain)
		if queryErr == nil {
			for _, mx := range mxs {
				answers = append(answers, fmt.Sprintf("%d %s", mx.Pref, mx.Host))
			}
		}

	case "TXT":
		answers, queryErr = resolver.LookupTXT(ctx, domain)

	case "CNAME":
		var cname string
		cname, queryErr = resolver.LookupCNAME(ctx, domain)
		if queryErr == nil {
			answers = append(answers, cname)
		}

	case "NS":
		var nss []*net.NS
		nss, queryErr = resolver.LookupNS(ctx, domain)
		if queryErr == nil {
			for _, ns := range nss {
				answers = append(answers, ns.Host)
			}
		}

	case "SRV":
		var srvs []*net.SRV
		_, srvs, queryErr = resolver.LookupSRV(ctx, "", "", domain)
		if queryErr == nil {
			for _, srv := range srvs {
				answers = append(answers, fmt.Sprintf("%d %d %d %s", srv.Priority, srv.Weight, srv.Port, srv.Target))
			}
		}

	case "SOA":
		// SOA isn't directly supported by net.Resolver. As a fallback we verify
		// the domain resolves via LookupHost. Full SOA support requires miekg/dns.
		var addrs []string
		addrs, queryErr = resolver.LookupHost(ctx, domain)
		if queryErr == nil {
			answers = addrs
		}

	default:
		result.Duration = time.Since(start)
		result.Error = fmt.Sprintf("unsupported record type: %s", recordType)
		return result
	}

	queryTime := time.Since(start)
	result.Duration = queryTime
	result.DNSQueryTime = queryTime

	if queryErr != nil {
		result.Error = fmt.Sprintf("DNS query failed for %s %s: %v", recordType, domain, queryErr)
		return result
	}

	result.DNSAnswers = answers

	// Verify expected values if configured.
	if len(dcfg.Expected) > 0 {
		answerSet := make(map[string]struct{}, len(answers))
		for _, a := range answers {
			answerSet[a] = struct{}{}
		}

		var missing []string
		for _, exp := range dcfg.Expected {
			if _, ok := answerSet[exp]; !ok {
				missing = append(missing, exp)
			}
		}
		if len(missing) > 0 {
			result.Error = fmt.Sprintf("expected values not found in answers: %s", strings.Join(missing, ", "))
			return result
		}
	}

	result.Success = true

	slog.Debug("DNS check completed",
		"name", cfg.Name,
		"domain", domain,
		"server", dcfg.Server,
		"record_type", recordType,
		"answers", answers,
		"query_time", queryTime,
	)

	return result
}
