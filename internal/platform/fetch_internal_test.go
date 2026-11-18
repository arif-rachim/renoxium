package platform

import (
	"net"
	"strings"
	"testing"
)

func TestFetchRefusesPrivateAddresses(t *testing.T) {
	for ip, want := range map[string]bool{"8.8.8.8": true, "2606:4700::1111": true, "127.0.0.1": false, "10.1.2.3": false, "169.254.169.254": false,
		"192.168.1.1": false, "100.64.0.1": false, "::1": false, "fd00::1": false, "::ffff:127.0.0.1": false, "0.0.0.0": false} {
		if publicIP(net.ParseIP(ip)) != want {
			t.Errorf("publicIP(%s) != %v", ip, want)
		}
	}
	p := &Platform{}
	for _, u := range []string{"https://127.0.0.1/", "https://169.254.169.254/latest/meta-data/", "https://10.0.0.1/"} {
		_, err := p.fetchClient().Get(u)
		if err == nil || !strings.Contains(err.Error(), "is not a public internet address") {
			t.Errorf("%s: %v", u, err)
		}
	}
	if _, err := p.fetchClient().Get("https://[::1]/"); err == nil { // refused, or no IPv6 here at all
		t.Error("::1 was reached")
	}
}
