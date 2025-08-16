package main

import "testing"

func TestPriorityStrategy(t *testing.T) {
	p1 := &Proxy{Name: "p1", Host: "h1", Port: 1, Priority: 1}
	p2 := &Proxy{Name: "p2", Host: "h2", Port: 2, Priority: 1}
	p3 := &Proxy{Name: "p3", Host: "h3", Port: 3, Priority: 2}
	cfg := Config{Chains: []UserChain{{Chain: []*Hop{{Strategy: "priority", Proxies: []*Proxy{p1, p2, p3}}}}}}
	initProxies(&cfg)
	hop := cfg.Chains[0].Chain[0]
	res1 := hop.orderedProxies()
	if len(res1) != 3 || res1[0] != p3 || res1[1] != p1 || res1[2] != p2 {
		t.Fatalf("unexpected order1: %v", res1)
	}
	res2 := hop.orderedProxies()
	if len(res2) != 3 || res2[0] != p3 || res2[1] != p2 || res2[2] != p1 {
		t.Fatalf("unexpected order2: %v", res2)
	}
}
