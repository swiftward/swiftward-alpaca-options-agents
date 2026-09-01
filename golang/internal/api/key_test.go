package api

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/record"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// Nobody reads the account without the key, and the ways in are exactly three.
//
// Until 1 September the read side asked for nothing at all: it is served on a
// port open to the internet, because the platform requires an address a judge
// opens by hand, and it answered every request with the positions, the equity,
// every order, every intent and the agent's own words. Live, during the
// competition.
func TestThePageIsReadWithAKey(t *testing.T) {
	const key = "a-long-random-string"
	served := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("the account"))
	})
	gate := guarded(key, served)

	ask := func(t *testing.T, target string, change func(*http.Request)) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		if change != nil {
			change(req)
		}
		answer := httptest.NewRecorder()
		gate.ServeHTTP(answer, req)

		return answer
	}

	t.Run("nothing at all is refused", func(t *testing.T) {
		answer := ask(t, "/api/money", nil)
		assert.Equal(t, http.StatusUnauthorized, answer.Code)
		assert.NotContains(t, answer.Body.String(), "the account")
	})

	t.Run("a wrong key is refused, and told nothing more", func(t *testing.T) {
		wrongHeader := ask(t, "/api/money", func(r *http.Request) { r.Header.Set(keyHeader, "guess") })
		wrongQuery := ask(t, "/api/money?key=guess", nil)
		assert.Equal(t, http.StatusUnauthorized, wrongHeader.Code)
		assert.Equal(t, http.StatusUnauthorized, wrongQuery.Code)
		assert.Equal(t, wrongHeader.Body.String(), wrongQuery.Body.String(),
			"a refusal that separates a missing key from a wrong one says which half to work on")
	})

	t.Run("a tool carries it in a header", func(t *testing.T) {
		answer := ask(t, "/api/money", func(r *http.Request) { r.Header.Set(keyHeader, key) })
		assert.Equal(t, http.StatusOK, answer.Code)
	})

	// A browser cannot put a header on a link, so a person arrives with the key in
	// the address once and the cookie carries the rest of the visit - including the
	// page's own calls to /api, which is what makes one link enough.
	t.Run("a person arrives with it in the address, is given a cookie, and the address loses the key", func(t *testing.T) {
		answer := ask(t, "/?key="+key, nil)
		require.Equal(t, http.StatusSeeOther, answer.Code)

		cookies := answer.Result().Cookies()
		require.Len(t, cookies, 1)
		assert.Equal(t, keyCookie, cookies[0].Name)
		assert.Equal(t, key, cookies[0].Value)
		assert.True(t, cookies[0].HttpOnly, "the page's own script has no reason to read it")

		// Left in the address the key is written into the browser's history, into
		// any log that keeps a request line, and into the Referer of every link the
		// page leads to.
		assert.NotContains(t, answer.Header().Get("Location"), key,
			"the reader is sent on WITHOUT the key in the address")

		withCookie := ask(t, "/api/state", func(r *http.Request) { r.AddCookie(cookies[0]) })
		assert.Equal(t, http.StatusOK, withCookie.Code)
	})

	// A key travelling in the address is a key in the history and in the Referer,
	// so it buys one arrival and nothing more. Where the connection is encrypted
	// the cookie says so; on plain HTTP it must not, or the browser drops it.
	t.Run("the cookie is marked secure only where the connection is", func(t *testing.T) {
		plain := ask(t, "/?key="+key, nil).Result().Cookies()
		require.Len(t, plain, 1)
		assert.False(t, plain[0].Secure, "on plain HTTP a secure cookie is a cookie the browser throws away")

		encrypted := ask(t, "https://page.example/?key="+key, func(r *http.Request) {
			r.TLS = &tls.ConnectionState{}
		}).Result().Cookies()
		require.Len(t, encrypted, 1)
		assert.True(t, encrypted[0].Secure)
	})

	// A liveness check that needs a secret is a liveness check nobody runs, and it
	// carries nothing about the account.
	t.Run("healthz answers without one", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, ask(t, "/healthz", nil).Code)
	})
}

// A read side with no key does not start. The alternative - empty means open - is
// a knob that must never be turned, and this is the state it was already in.
func TestAReadSideWithNoKeyRefusesToStart(t *testing.T) {
	_, err := Read{WebDir: "", Log: nil}.Handler()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PAGE_KEY")
}

// The redirect can only ever name this host.
//
// Found by review: handing back the request's own URI is an open redirect. A
// link to `//somewhere.else/?key=...` parses with that host sitting in the PATH,
// and a Location that begins with two slashes is read by every browser as an
// address on that other host. The key never travels there and the cookie is bound
// to this host, so what it cost was a redirect anyone could aim - which is enough
// to matter and enough for a scanner to report.
func TestTheRedirectCannotBeAimedAtAnotherHost(t *testing.T) {
	const key = "a-long-random-string"
	gate := guarded(key, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	for _, target := range []string{
		"//attacker.example/?key=" + key,
		"///attacker.example/?key=" + key,
		"//attacker.example/path?key=" + key + "&kept=yes",
	} {
		t.Run(target, func(t *testing.T) {
			answer := httptest.NewRecorder()
			gate.ServeHTTP(answer, httptest.NewRequest(http.MethodGet, target, nil))
			require.Equal(t, http.StatusSeeOther, answer.Code)

			sent := answer.Header().Get("Location")
			assert.False(t, strings.HasPrefix(sent, "//"),
				"a Location beginning with two slashes is an address on somebody else's host: %q", sent)

			// The name may survive as a PATH segment and that is harmless. What
			// must not survive is a host: parsed, this names none.
			where, err := url.Parse(sent)
			require.NoError(t, err)
			assert.Empty(t, where.Host, "the redirect names a host: %q", sent)
			assert.Empty(t, where.Scheme)
			assert.True(t, strings.HasPrefix(where.Path, "/"))
			assert.NotContains(t, sent, key)
		})
	}
}

// The cookie is marked secure behind something that ended the TLS for us. From
// submission day the page is published through a Funnel, which does exactly that
// and forwards plain HTTP here - so req.TLS is nil on a request the browser
// loaded over https, and asking it alone would leave the cookie unmarked on the
// only day the page is public.
func TestTheCookieIsSecureBehindSomethingThatEndedTheTLS(t *testing.T) {
	const key = "a-long-random-string"
	gate := guarded(key, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	answer := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/?key="+key, nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	gate.ServeHTTP(answer, req)

	cookies := answer.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.True(t, cookies[0].Secure)
}

// Every route the read side serves is behind the gate, not only the ones a test
// remembered to name. The review's point: a test that drives `guarded` directly
// proves the wrapper and says nothing about what got wrapped.
func TestEveryRouteOfTheReadSideIsBehindTheGate(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>the page</html>"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.js"), []byte("// the built page"), 0o600))

	handler, err := Read{Record: record.NewMemory(), WebDir: dir, Key: testKey, Log: zaptest.NewLogger(t)}.Handler()
	require.NoError(t, err)

	for _, route := range []string{
		"/", "/app.js", "/index.html", "/api/state", "/api/money", "/api/equity",
		"/api/limits", "/api/sweep", "/healthz/", "/HEALTHZ", "/nothing-here",
	} {
		answer := httptest.NewRecorder()
		handler.ServeHTTP(answer, httptest.NewRequest(http.MethodGet, route, nil))
		assert.Equal(t, http.StatusUnauthorized, answer.Code, "%s answered without the key", route)
		assert.NotContains(t, answer.Body.String(), "the page", "%s served the page itself", route)
	}

	// And the one deliberate exemption still answers, or nothing checks liveness.
	alive := httptest.NewRecorder()
	handler.ServeHTTP(alive, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, http.StatusOK, alive.Code)
}
