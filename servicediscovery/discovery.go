package servicediscovery

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/miekg/dns"
	"net"
)

type consulServiceDiscovery struct {
	dnsServer   string
	dnsSearch   string
	client      DnsClient
	targetCache map[string]net.IP
}

type ServiceInstance struct {
	Ip   string
	Port string
}

func NewConsulServiceDiscovery(dnsServer string) (ServiceDiscovery, error) {

	host, port, err := net.SplitHostPort(dnsServer)
	if err != nil {
		return nil, err
	}

	// If it is not an IP address try to resolve the DNS name.
	// This is used for local development.
	if net.ParseIP(host) == nil {
		addrs, err := net.LookupHost(host)
		if err != nil {
			return nil, err
		}
		if len(addrs) == 0 {
			log.WithField("host", host).Error("No service discovery host could be resolved")
			return nil, fmt.Errorf("No service discovery host could be resolved")
		}
		dnsServer = net.JoinHostPort(addrs[0], port)
	}

	ret := consulServiceDiscovery{
		dnsServer:   dnsServer,
		dnsSearch:   ".service.consul",
		client:      &dns.Client{},
		targetCache: make(map[string]net.IP)}
	return &ret, nil
}

func (s *consulServiceDiscovery) DiscoverService(serviceName string) (ip string, port string, err error) {
	instances, err := s.DiscoverAllServiceInstances(serviceName)
	if err != nil {
		return "", "", err
	}

	if len(instances) == 0 {
		log.WithField("serviceName", serviceName).Error("Service lookup: No SRV entry in DNS response")
		return "", "", fmt.Errorf("Service lookup: No SRV entry in DNS response")
	}

	return instances[0].Ip, instances[0].Port, nil
}

func (s *consulServiceDiscovery) DiscoverAllServiceInstances(serviceName string) (instances []ServiceInstance, err error) {

	instances = make([]ServiceInstance, 0)

	m := new(dns.Msg)
	fqdn := dns.Fqdn(serviceName + s.dnsSearch)
	m.SetQuestion(fqdn, dns.TypeSRV)

	r, _, err := s.client.Exchange(m, s.dnsServer)
	if err != nil {
		log.WithField("serviceName", fqdn).
			WithField("dnsServer", s.dnsServer).
			WithField("error", err).
			Error("Error during connection to DNS server")
		return nil, err
	}

	if r.Rcode != dns.RcodeSuccess {
		log.WithField("serviceName", fqdn).Error("Service lookup: DNS query did not succeed")
		return nil, fmt.Errorf("Service lookup: DNS query did not succeed")
	}

	for _, a := range r.Answer {
		if srv, ok := a.(*dns.SRV); ok {
			target := srv.Target[:len(srv.Target)-1]
			targetIp, err := s.resolveTarget(target)
			if err == nil {
				instances = append(instances, ServiceInstance{
					Ip:   targetIp.String(),
					Port: fmt.Sprintf("%d", srv.Port),
				})
			}
		}
	}

	return instances, nil
}

func (s *consulServiceDiscovery) resolveTarget(target string) (ip net.IP, err error) {

	if val, ok := s.targetCache[target]; ok {
		return val, nil
	}

	fqdn := dns.Fqdn(target)

	m := new(dns.Msg)
	m.SetQuestion(fqdn, dns.TypeA)

	r, _, err := s.client.Exchange(m, s.dnsServer)
	if err != nil {
		log.WithField("fqdn", fqdn).
			WithField("target", target).
			WithField("dnsServer", s.dnsServer).
			WithField("error", err).
			Error("Error during connection to DNS server")
		return nil, err
	}

	if r.Rcode != dns.RcodeSuccess {
		log.WithField("fqdn", fqdn).WithField("target", target).Error("Service lookup: Target DNS query did not succeed")
		return nil, fmt.Errorf("Service lookup: Target DNS query did not succeed")
	}

	for _, a := range r.Answer {
		if srv, ok := a.(*dns.A); ok {
			s.targetCache[target] = srv.A
			return srv.A, nil
		}
	}

	log.WithField("fqdn", fqdn).WithField("target", target).Error("Service lookup: No A entry in DNS response")
	return nil, fmt.Errorf("Service lookup: No A entry in DNS response")

}
