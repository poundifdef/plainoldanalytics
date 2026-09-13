package webapp

import (
	"fmt"
	"net/http"
)

// settingsPage is a display-only skeleton: it shows the real, active
// configuration (Config, wired at Capturer construction) and the snippet
// to paste into frontend pages. There is no settings storage yet, so
// nothing here is editable.
func (a *App) settingsPage(w http.ResponseWriter, r *http.Request) {
	root := mountRoot(r)
	snippet := fmt.Sprintf("<script src=%q></script>\n<script>\n  plainoldanalytics.init_session_recording();\n  plainoldanalytics.track('signup', {plan: 'pro'});\n</script>", root+"e.js")

	a.render(w, r, "settings", map[string]any{
		"Nav":     "settings",
		"Snippet": snippet,
		"Config":  a.config,
	})
}
