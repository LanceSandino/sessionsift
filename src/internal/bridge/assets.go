// @module HostedAssets serves immutable shared safe-content assets without private session data.
package bridge

import (
	"crypto/sha256"
	"encoding/base64"
	"github.com/LanceSandino/sessionsift/src/webui"
	"io/fs"
	"net/http"
	"strings"
)

// Only the fixed, compiled bootstrap scripts may execute inline.
func hostedCSP() string {
	policy := "default-src 'none'; style-src 'self' 'unsafe-inline'; script-src 'self'"
	for _, page := range []string{connectPage, invitePage, supportPage} {
		_, script, ok := strings.Cut(page, "<script>")
		if !ok {
			continue
		}
		script, _, _ = strings.Cut(script, "</script>")
		digest := sha256.Sum256([]byte(script))
		policy += " 'sha256-" + base64.StdEncoding.EncodeToString(digest[:]) + "'"
	}
	return policy + "; connect-src 'self'; img-src 'self'; font-src 'self'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
}

func hostedPage(page string) string {
	prefix := "/"
	if page == invitePage {
		prefix = "/invite/"
	}
	return hostedPageAt(page, prefix)
}

// hostedPageAt injects the shared safe-content bundle under a path prefix the
// serving Access application already covers.
func hostedPageAt(page, prefix string) string {
	assets, err := fs.Sub(webui.Assets, "dist")
	if err != nil {
		return page
	}
	names, _ := fs.Glob(assets, "assets/hosted-*.js")
	if len(names) != 1 {
		return page
	}
	return strings.Replace(page, "</head>", `<script type="module" src="`+prefix+names[0]+`"></script></head>`, 1)
}

func serveAsset(w http.ResponseWriter, r *http.Request) {
	for _, alias := range []string{"/invite", "/support"} {
		if strings.HasPrefix(r.URL.Path, alias+"/assets/") {
			r = r.Clone(r.Context())
			u := *r.URL
			u.Path = strings.TrimPrefix(u.Path, alias)
			r.URL = &u
			break
		}
	}
	assets, err := fs.Sub(webui.Assets, "dist")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// Only emitted hashed assets; never expose an index fallback or filesystem paths.
	name := strings.TrimPrefix(r.URL.Path, "/")
	if !strings.HasPrefix(name, "assets/") || strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	if _, err = fs.Stat(assets, name); err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.FileServer(http.FS(assets)).ServeHTTP(w, r)
}
