package api

import (
	"net/http"
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
			if fromQuery != key {
				refuse(w)
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name: keyCookie, Value: key, Path: "/",
				HttpOnly: true, SameSite: http.SameSiteLaxMode,
			})
			next.ServeHTTP(w, req)

			return
		}

		if strings.TrimSpace(req.Header.Get(keyHeader)) == key {
			next.ServeHTTP(w, req)
			return
		}
		if cookie, err := req.Cookie(keyCookie); err == nil && cookie.Value == key {
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
