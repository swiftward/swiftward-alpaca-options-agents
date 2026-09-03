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
				// From submission day the page is published through a Funnel, which
				// ends the TLS itself and forwards plain HTTP here - so this process
				// never sees a certificate on the request that a browser loaded over
				// https, and asking req.TLS alone would leave that cookie unmarked.
				Secure: encrypted(req),
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
	// An empty key matches an empty request, so every reader would be admitted -
	// which turns the whole gate into an open door with a lock drawn on it.
	// Handler refuses to start there, and this refuses to be the reason it would
	// not have mattered.
	if key == "" {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(given), []byte(key)) == 1
}

// withoutKey is the same address with the key taken out of it, and it can only
// ever name THIS host.
//
// The obvious version - hand back the request's own URI - is an open redirect,
// which is what the review caught: a link to `//somewhere.else/?key=...` parses
// with that host sitting in the PATH, and a Location beginning with two slashes
// is read by every browser as an address on that other host. So the path is
// rebuilt with exactly one leading slash and nothing that could be read as a
// host survives. The key never travelled there and the cookie is bound to this
// host, so what it cost was a redirect anyone could aim, which is enough.
func withoutKey(from *url.URL) string {
	query := from.Query()
	query.Del(keyQuery)

	local := "/" + strings.TrimLeft(from.EscapedPath(), "/")
	if asked := query.Encode(); asked != "" {
		return local + "?" + asked
	}

	return local
}

// encrypted reports whether the reader's connection is over TLS, including where
// something in front of us ended it and said so.
func encrypted(req *http.Request) bool {
	if req.TLS != nil {
		return true
	}

	return strings.EqualFold(req.Header.Get("X-Forwarded-Proto"), "https")
}

// gate wraps the routes in whatever this deployment decided about who may read
// them. Both places that finish building the mux call this one, so the decision
// cannot differ between serving the page and serving the JSON alone.
//
// Handler has already refused the two states this cannot express: no key and not
// public, and public with a key.
func (r Read) gate(next http.Handler) http.Handler {
	if r.Public {
		r.Log.Warn("PAGE_PUBLIC is set: this page is served to anyone who reaches it, " +
			"with the account's positions, its equity, every order and the agent's own words")

		return next
	}

	return guarded(strings.TrimSpace(r.Key), next)
}
