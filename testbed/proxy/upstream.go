// The link to the real Alpaca MCP: one connection, a priority queue and a cache.
//
// There is one connection for the whole arena - the keys there belong to one
// account, and every participant's reads are the same. Both other parts follow
// from that. The queue: a hundred agents asking for a chain at once would crowd
// out the filling of orders if everyone were let through, so orders and the
// matcher go first, account snapshots next, and browsing the market gives way.
// The cache: the same chain asked for by ten participants within one second goes
// upstream once.
//
// A participant is NEVER refused because of the queue - it waits. An agent reads
// a refusal as "the broker is unavailable" and starts behaving along its own
// branch for a failure, when all that happened is that we were busy.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Priorities. Lower goes earlier.
const (
	prioTrade   = 0 // orders and the matcher: money depends on them
	prioAccount = 1 // snapshots of the account and its positions
	prioBrowse  = 2 // everything else: chains, news, looking around
)

// upstream is the connection to the real Alpaca MCP.
type upstream struct {
	endpoint string

	mu      sync.Mutex
	session *mcp.ClientSession

	gate  *gate
	cache *cache
}

func dial(ctx context.Context, endpoint string, parallel int, quoteTTL time.Duration) (*upstream, error) {
	u := &upstream{endpoint: endpoint, gate: newGate(parallel), cache: newCache(quoteTTL)}
	if _, err := u.connect(ctx); err != nil {
		return nil, err
	}

	return u, nil
}

func (u *upstream) connect(ctx context.Context) (*mcp.ClientSession, error) {
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.session != nil {
		return u.session, nil
	}

	// The limits go on the transport, not on http.Client: the client holds a
	// persistent stream, and a blanket Timeout would cut it every so often. What
	// has to be stopped is a server that accepted the connection and went
	// quiet.
	transport := &mcp.StreamableClientTransport{
		Endpoint: u.endpoint,
		HTTPClient: &http.Client{Transport: &http.Transport{
			ResponseHeaderTimeout: 30 * time.Second,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConnsPerHost:   8,
		}},
	}

	session, err := mcp.NewClient(&mcp.Implementation{Name: "arena-proxy", Version: "v0.2.0"}, nil).
		Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to the broker: %w", err)
	}
	u.session = session

	return session, nil
}

// drop discards the connection so the next call raises a new one. Called on a
// transport error: a tool's error arrives as a result with IsError and does not
// spoil the connection.
func (u *upstream) drop(bad *mcp.ClientSession) {
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.session == bad {
		u.session = nil
		go bad.Close() //nolint:errcheck // closing what was orphaned, the answer is not needed
	}
}

// Tools reads what is served upstream once: an MCP client asks for the list once
// per connection too, so there is nobody to change it on the fly for.
func (u *upstream) Tools(ctx context.Context) (map[string]*mcp.Tool, error) {
	s, err := u.connect(ctx)
	if err != nil {
		return nil, err
	}

	list, err := s.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}

	out := make(map[string]*mcp.Tool, len(list.Tools))
	for _, t := range list.Tools {
		out[t.Name] = t
	}

	return out, nil
}

// Call goes upstream through the queue and the cache.
func (u *upstream) Call(ctx context.Context, prio int, name string, args any) (*mcp.CallToolResult, error) {
	if u.cache == nil {
		return u.direct(ctx, prio, name, args)
	}

	return u.cache.through(ctx, name, args, func() (*mcp.CallToolResult, error) {
		return u.direct(ctx, prio, name, args)
	})
}

func (u *upstream) direct(ctx context.Context, prio int, name string, args any) (*mcp.CallToolResult, error) {
	if err := u.gate.enter(ctx, prio); err != nil {
		return nil, err
	}
	defer u.gate.leave()

	s, err := u.connect(ctx)
	if err != nil {
		return nil, err
	}

	res, err := s.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		u.drop(s)

		return nil, err
	}

	return res, nil
}

// CallJSON goes upstream and parses the answer into a struct.
// CallJSONDirect goes upstream WITHOUT the cache and parses the answer.
//
// It exists for the one answer a cache cannot serve: the current time. Every
// other answer is either still true or plainly stale, but a timestamp served
// from a cache is neither - it is wrong by exactly the age of the entry, and it
// looks perfectly well-formed while being so.
func (u *upstream) CallJSONDirect(ctx context.Context, prio int, name string, args any, into any) error {
	res, err := u.direct(ctx, prio, name, args)

	return parseInto(name, res, err, into)
}

func (u *upstream) CallJSON(ctx context.Context, prio int, name string, args any, into any) error {
	res, err := u.Call(ctx, prio, name, args)

	return parseInto(name, res, err, into)
}

func parseInto(name string, res *mcp.CallToolResult, err error, into any) error {
	if err != nil {
		return err
	}
	if res.IsError {
		return fmt.Errorf("%s answered with an error: %s", name, firstText(res))
	}

	for _, c := range res.Content {
		if text, ok := c.(*mcp.TextContent); ok {
			return json.Unmarshal([]byte(text.Text), into)
		}
	}

	return fmt.Errorf("%s returned no text", name)
}

func firstText(res *mcp.CallToolResult) string {
	for _, c := range res.Content {
		if text, ok := c.(*mcp.TextContent); ok {
			return text.Text
		}
	}

	return ""
}

func (u *upstream) Close() error {
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.session == nil {
		return nil
	}
	s := u.session
	u.session = nil

	return s.Close()
}

// gate is how many calls go upstream at once and who goes first.
//
// Waiters stand in three queues by priority. A freed place goes to the most
// important non-empty queue; within a queue it is first come, first served.
// Nobody is displaced and nobody is refused: the only way to leave here without
// a place is the caller's own context dying.
type gate struct {
	mu     sync.Mutex
	free   int
	queues [3][]chan struct{}
}

func newGate(parallel int) *gate {
	if parallel < 1 {
		parallel = 1
	}

	return &gate{free: parallel}
}

func (g *gate) enter(ctx context.Context, prio int) error {
	if prio < 0 || prio > 2 {
		prio = prioBrowse
	}

	g.mu.Lock()
	if g.free > 0 {
		g.free--
		g.mu.Unlock()

		return nil
	}
	wait := make(chan struct{})
	g.queues[prio] = append(g.queues[prio], wait)
	g.mu.Unlock()

	select {
	case <-wait:
		return nil
	case <-ctx.Done():
		// The place may have been handed to us at exactly this moment; then it
		// has to be given back, or the queue loses a slot for good.
		g.mu.Lock()
		taken := g.remove(prio, wait)
		g.mu.Unlock()
		if taken {
			g.leave()
		}

		return ctx.Err()
	}
}

// remove takes a waiter out of the queue. It returns true when the waiter was no
// longer there - meaning a place had already been handed to it.
func (g *gate) remove(prio int, wait chan struct{}) bool {
	for i, w := range g.queues[prio] {
		if w == wait {
			g.queues[prio] = append(g.queues[prio][:i], g.queues[prio][i+1:]...)

			return false
		}
	}

	return true
}

func (g *gate) leave() {
	g.mu.Lock()
	defer g.mu.Unlock()

	for prio := range g.queues {
		if len(g.queues[prio]) == 0 {
			continue
		}
		wait := g.queues[prio][0]
		g.queues[prio] = g.queues[prio][1:]
		close(wait)

		return
	}
	g.free++
}

// cache holds upstream answers for exactly as long as they remain true.
//
// The lifetimes differ by the nature of the data rather than by taste: a quote
// ages in seconds, a list of contracts lives for minutes, and the clock changes
// not with time at all but at a boundary - and it names that boundary itself.
type cache struct {
	quote time.Duration
	// clockCap is the ceiling on the clock's answer. The answer holds until the
	// next session boundary, but it also carries a timestamp field, and a time
	// frozen for four hours would deceive an agent that counts from it how much
	// of the day is left. It is a separate field so a bench can set its own.
	clockCap time.Duration

	mu  sync.Mutex
	hot map[string]*entry
}

type entry struct {
	ready chan struct{}
	res   *mcp.CallToolResult
	err   error
	until time.Time
}

func newCache(quote time.Duration) *cache {
	if quote <= 0 {
		quote = 1500 * time.Millisecond
	}

	return &cache{quote: quote, clockCap: 15 * time.Second, hot: map[string]*entry{}}
}

// ttl is how long this tool's answer lives.
func (c *cache) ttl(name string) time.Duration {
	switch name {
	case "get_option_snapshot", "get_option_chain", "get_stock_latest_trade",
		"get_stock_latest_quote", "get_option_latest_quote", "get_option_latest_trade",
		"get_stock_snapshot":
		return c.quote
	case "get_option_contracts", "get_option_contract", "get_all_assets", "get_asset", "get_calendar":
		return 5 * time.Minute
	case "get_news", "get_corporate_action_announcements":
		return time.Minute
	case "get_clock":
		// The real lifetime is computed from the answer itself, below. This is
		// only the flag saying "cache it".
		return time.Second
	default:
		return 0
	}
}

// through serves the answer from the cache, and when there is none goes upstream
// ONCE on behalf of everyone who asked for the same thing while the call ran.
func (c *cache) through(ctx context.Context, name string, args any, call func() (*mcp.CallToolResult, error)) (*mcp.CallToolResult, error) {
	ttl := c.ttl(name)
	if ttl <= 0 {
		return call()
	}

	raw, err := json.Marshal(args)
	if err != nil {
		return call()
	}
	key := name + "|" + string(raw)

	c.mu.Lock()
	if e := c.hot[key]; e != nil {
		c.mu.Unlock()
		select {
		case <-e.ready:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if e.err == nil && time.Now().Before(e.until) {
			return e.res, nil
		}
		// The answer went stale while we waited our turn: round again, where we
		// either start a new entry or join somebody else's fresh call.
		c.mu.Lock()
		if c.hot[key] == e {
			delete(c.hot, key)
		}
	}
	fresh := &entry{ready: make(chan struct{})}
	c.hot[key] = fresh
	c.mu.Unlock()

	fresh.res, fresh.err = call()
	fresh.until = time.Now().Add(ttl)
	if name == "get_clock" && fresh.err == nil {
		fresh.until = clockUntil(fresh.res, time.Now(), c.clockCap)
	}
	close(fresh.ready)

	if fresh.err != nil {
		// The error is not kept: the next asker should ask the market rather than
		// be handed yesterday's dropped connection.
		c.mu.Lock()
		if c.hot[key] == fresh {
			delete(c.hot, key)
		}
		c.mu.Unlock()
	}

	return fresh.res, fresh.err
}

// clockUntil is how long the clock's answer still holds: until the next session
// boundary, which it names itself, but no longer than the ceiling.
func clockUntil(res *mcp.CallToolResult, now time.Time, cap time.Duration) time.Time {
	limit := now.Add(cap)

	var answer struct {
		Data struct {
			NextOpen  string `json:"next_open"`
			NextClose string `json:"next_close"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(firstText(res)), &answer); err != nil {
		return limit
	}

	for _, raw := range []string{answer.Data.NextOpen, answer.Data.NextClose} {
		at, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			continue
		}
		if at.After(now) && at.Before(limit) {
			limit = at
		}
	}

	return limit
}
