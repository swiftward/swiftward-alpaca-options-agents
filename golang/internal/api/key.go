package api

import (
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"
)

// The page is served on a port open to the internet, because the platform
// requires an address a judge opens by hand. Until 31 August it asked for
// nothing, so anyone who reached the port read the account's positions, its
// equity, every order, every intent and the agent's own words - live, while the
// competition was running.
//
// It is a READ side and it can order nothing: the credential it holds to the
// broker has no method that changes anything, and that stays the control that
// matters. What was missing is a control on who may READ.
const (
	// keyHeader carries the key for a tool. A browser cannot set a header on a
	// link, which is why the query below exists at all.
	keyHeader = "X-Page-Key"
	// keyQuery is how a person arrives: one link, once. The key is then kept in a
	// cookie so the page's own calls carry it without appearing in every address.
	keyQuery = "key"
	// keyCookie is that cookie.
	keyCookie = "page_key"
)

// guarded refuses a request that does not carry the key, and turns a key given in
// the address into a cookie so the rest of the visit needs no address at all.
//
// `/healthz` stays open on purpose: it answers whether the process is alive and
// carries nothing about the account, and a health check that needs a secret is a
// health check that stops being run.
func guarded(key string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/healthz" {
			next.ServeHTTP(w, req)
			return
		}

		if fromQuery := req.URL.Query().Get(keyQuery); fromQuery != "" {
			if !same(fromQuery, key) {
				refuse(w)
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name: keyCookie, Value: key, Path: "/",
				HttpOnly: true, SameSite: http.SameSiteLaxMode,
				// Only where the connection is encrypted: set on plain HTTP the
				// browser drops the cookie and the visit ends at the first click.
				Secure: req.TLS != nil,
			})
			// The key does NOT stay in the address. Left there it is written into
			// the browser's history, into any log that records a request line, and
			// into the Referer header of every link the page leads to. The cookie
			// is set and the reader is sent to the same place without it - which is
			// also why `?key=` is for a PERSON arriving and a tool uses the header.
			http.Redirect(w, req, withoutKey(req.URL), http.StatusSeeOther)

			return
		}

		if same(strings.TrimSpace(req.Header.Get(keyHeader)), key) {
			next.ServeHTTP(w, req)
			return
		}
		if cookie, err := req.Cookie(keyCookie); err == nil && same(cookie.Value, key) {
			next.ServeHTTP(w, req)
			return
		}

		refuse(w)
	})
}

// refuse says the same thing to everyone and names nothing. A refusal that
// distinguishes "no key" from "wrong key" tells a stranger which half to work on.
func refuse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte("this page is read with a key\n"))
}

// same compares in constant time. A plain comparison returns sooner the earlier
// it finds a difference, and a secret compared that way can be read a character
// at a time by anyone patient enough to measure.
func same(given, key string) bool {
	return subtle.ConstantTimeCompare([]byte(given), []byte(key)) == 1
}

// withoutKey is the same address with the key taken out of it.
func withoutKey(from *url.URL) string {
	stripped := *from
	query := stripped.Query()
	query.Del(keyQuery)
	stripped.RawQuery = query.Encode()
	if stripped.Path == "" {
		stripped.Path = "/"
	}

	return stripped.RequestURI()
}
