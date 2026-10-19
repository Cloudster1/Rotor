package tools

import (
	"slices"
	"testing"
)

func TestAToolsLinkHostsAreItsAddressesHosts(t *testing.T) {
	tool := Tool{BaseURL: "https://Exciter.Example:8443/t/acme", Resource: "https://exciter.example/t/acme/mcp"}
	if got := tool.LinkHosts(); !slices.Equal(got, []string{"exciter.example"}) {
		t.Errorf("link hosts = %v", got)
	}
	split := Tool{BaseURL: "https://wiki.example", Resource: "https://mcp.wiki.example/mcp"}
	if got := split.LinkHosts(); !slices.Equal(got, []string{"wiki.example", "mcp.wiki.example"}) {
		t.Errorf("link hosts of a tool on two hosts = %v", got)
	}
	if got := (Tool{BaseURL: "not a url"}).LinkHosts(); len(got) != 0 {
		t.Errorf("link hosts of no address = %v", got)
	}
}
