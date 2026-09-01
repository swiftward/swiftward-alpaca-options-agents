package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	t.Run("a person arrives with it in the address and is given a cookie", func(t *testing.T) {
		answer := ask(t, "/?key="+key, nil)
		require.Equal(t, http.StatusOK, answer.Code)
		cookies := answer.Result().Cookies()
		require.Len(t, cookies, 1)
		assert.Equal(t, keyCookie, cookies[0].Name)
		assert.Equal(t, key, cookies[0].Value)
		assert.True(t, cookies[0].HttpOnly, "the page's own script has no reason to read it")

		withCookie := ask(t, "/api/state", func(r *http.Request) { r.AddCookie(cookies[0]) })
		assert.Equal(t, http.StatusOK, withCookie.Code)
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
