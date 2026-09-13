package webapp

import "strings"

// uaInfo is the coarse classification of a User-Agent string the dashboard
// breaks traffic down by.
type uaInfo struct {
	Browser string
	OS      string
	Device  string
}

// parseUA classifies a raw User-Agent header into browser, OS, and device
// buckets. It is deliberately small: dashboards need "Chrome / macOS /
// Desktop", not full UA fidelity. Match order matters throughout — e.g.
// Chrome's UA contains "Safari", Edge's contains "Chrome".
func parseUA(ua string) uaInfo {
	l := strings.ToLower(ua)
	info := uaInfo{Browser: "Other", OS: "Other", Device: "Desktop"}

	if isBot(l) {
		return uaInfo{Browser: botName(ua), OS: "Bot", Device: "Bot"}
	}

	switch {
	case strings.Contains(l, "edg/") || strings.Contains(l, "edge/"):
		info.Browser = "Edge"
	case strings.Contains(l, "opr/") || strings.Contains(l, "opera"):
		info.Browser = "Opera"
	case strings.Contains(l, "samsungbrowser"):
		info.Browser = "Samsung Internet"
	case strings.Contains(l, "firefox/") || strings.Contains(l, "fxios/"):
		info.Browser = "Firefox"
	case strings.Contains(l, "chrome/") || strings.Contains(l, "crios/"):
		info.Browser = "Chrome"
	case strings.Contains(l, "safari/"):
		info.Browser = "Safari"
	case strings.Contains(l, "msie") || strings.Contains(l, "trident/"):
		info.Browser = "Internet Explorer"
	}

	switch {
	case strings.Contains(l, "windows"):
		info.OS = "Windows"
	case strings.Contains(l, "android"):
		info.OS = "Android"
	case strings.Contains(l, "iphone"), strings.Contains(l, "ipad"), strings.Contains(l, "ipod"):
		info.OS = "iOS"
	case strings.Contains(l, "cros"):
		info.OS = "ChromeOS"
	case strings.Contains(l, "mac os x"), strings.Contains(l, "macintosh"):
		info.OS = "macOS"
	case strings.Contains(l, "linux"):
		info.OS = "Linux"
	}

	switch {
	case strings.Contains(l, "ipad"),
		strings.Contains(l, "android") && !strings.Contains(l, "mobile"):
		info.Device = "Tablet"
	case strings.Contains(l, "mobi"), strings.Contains(l, "iphone"), strings.Contains(l, "ipod"):
		info.Device = "Mobile"
	}
	return info
}

func isBot(l string) bool {
	for _, marker := range []string{
		"bot", "crawler", "spider", "curl/", "wget/", "python-requests",
		"go-http-client", "headlesschrome", "monitor", "probe",
	} {
		if strings.Contains(l, marker) {
			return true
		}
	}
	return false
}

// botName returns a short label for a bot UA: the product token before the
// first slash (e.g. "curl/8.4.0" -> "curl"). Browser-impersonating bots
// ("Mozilla/5.0 (compatible; Googlebot/...)") and anything else unparseable
// fall back to "Bot".
func botName(ua string) string {
	name, _, ok := strings.Cut(ua, "/")
	name = strings.TrimSpace(name)
	if !ok || name == "" || strings.EqualFold(name, "mozilla") || strings.ContainsAny(name, " ();") {
		return "Bot"
	}
	return name
}
