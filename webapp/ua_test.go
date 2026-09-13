package webapp

import "testing"

func TestParseUA(t *testing.T) {
	for ua, want := range map[string]uaInfo{
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36": {"Chrome", "macOS", "Desktop"},
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0": {"Edge", "Windows", "Desktop"},
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.1 Mobile/15E148 Safari/604.1": {"Safari", "iOS", "Mobile"},
		"Mozilla/5.0 (iPad; CPU OS 17_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.1 Mobile/15E148 Safari/604.1":         {"Safari", "iOS", "Tablet"},
		"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36":                  {"Chrome", "Android", "Mobile"},
		"Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0":                                                        {"Firefox", "Linux", "Desktop"},
		"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)":                                                              {"Bot", "Bot", "Bot"},
		"curl/8.4.0":       {"curl", "Bot", "Bot"},
		"Go-http-client/2": {"Go-http-client", "Bot", "Bot"},
		"weird thing":      {"Other", "Other", "Desktop"},
	} {
		if got := parseUA(ua); got != want {
			t.Errorf("parseUA(%q):\ngot  %+v\nwant %+v", ua, got, want)
		}
	}
}
