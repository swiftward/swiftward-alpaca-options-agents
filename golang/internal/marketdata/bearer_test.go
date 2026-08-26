package marketdata

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recorder struct{ seen []*http.Request }

func (r *recorder) RoundTrip(request *http.Request) (*http.Response, error) {
	r.seen = append(r.seen, request)

	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: request}, nil
}

// Every request carries the credential, not only the first: a gateway
// authenticates the initialize, each call and the standing stream separately,
// and refuses whichever arrives without one.
func TestEveryRequestCarriesTheCredential(t *testing.T) {
	under := &recorder{}
	client := &http.Client{Transport: bearer{token: "dk:tenant:0a1b2c3d:secret", next: under}}

	for _, path := range []string{"/mcp/alpaca", "/mcp/alpaca", "/mcp/alpaca"} {
		request, err := http.NewRequest(http.MethodPost, "http://gateway"+path, nil)
		require.NoError(t, err)
		_, err = client.Do(request)
		require.NoError(t, err)
	}

	require.Len(t, under.seen, 3)
	for _, request := range under.seen {
		assert.Equal(t, "Bearer dk:tenant:0a1b2c3d:secret", request.Header.Get("Authorization"))
	}
}

// The caller's request is left as it was. A RoundTripper that writes into the
// request it is given corrupts a retry, which sends the same request again.
func TestTheCallersRequestIsNotModified(t *testing.T) {
	under := &recorder{}
	client := &http.Client{Transport: bearer{token: "secret", next: under}}

	request, err := http.NewRequest(http.MethodPost, "http://gateway/mcp/alpaca", nil)
	require.NoError(t, err)
	_, err = client.Do(request)
	require.NoError(t, err)

	assert.Empty(t, request.Header.Get("Authorization"))
	assert.Equal(t, "Bearer secret", under.seen[0].Header.Get("Authorization"))
}

// Without a token the client is the plain one: the broker's own server asks for
// nothing, and sending it an empty credential would be a header that says the
// caller has one.
func TestWithoutATokenNoHeaderIsAdded(t *testing.T) {
	assert.Empty(t, NewBroker("http://alpaca-mcp:8000/mcp").token)
	assert.Equal(t, "t", NewBrokerWithToken("http://gateway:8095/mcp/alpaca", "t").token)
}
