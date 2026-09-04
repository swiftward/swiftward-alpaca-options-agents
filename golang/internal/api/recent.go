package api

import (
	"context"
	"sync"
	"time"
)

// The broker's answer, kept for a moment.
//
// Measured on the stand on 4 September: /api/money answered in 1.29 seconds while
// the four routes beside it answered in 0.6, and the page waits for all five. The
// difference is the broker, which the other four never touch.
//
// Nothing else is stored here and nothing outside this process knows about it -
// it is a field on the handler, in memory, in the same binary. A cache of one
// value that lives ten seconds does not need a server of its own, and a reader
// running this from a clone would then need that server too.
//
// TWO THINGS THIS IS NOT. It does not answer from an old copy when the copy has
// expired: the reader waits for the live answer, because a page that shows a
// figure from a minute ago while claiming to be live has stopped being evidence.
// And it never keeps a failure - if the broker is down, every reader is told, and
// the page has a shape for exactly that.

// How long an answer stands. The page re-reads every fifteen seconds, so the
// window is deliberately shorter than that: a reader watching alone always gets a
// live answer, and what this collapses is the case judging day actually produces
// - several people on the same page at the same moment, asking the broker the
// same question within the same second.
//
// A variable rather than a constant so a test can shorten it: the alternative is
// a test that sleeps for ten seconds to watch a copy go stale.
var keepsFor = 10 * time.Second

// A call that never returns would otherwise hold every later reader behind it.
// The broker's own limit is ninety seconds per call and this asks three, so this
// is the ceiling on the whole question rather than on any one part.
const asksWithin = 2 * time.Minute

type recent struct {
	mu    sync.Mutex
	holds money
	at    time.Time
	// The call in flight, if there is one. Ten readers arriving together make one
	// question of it and share the answer: without this, ten readers meant ten
	// broker calls, all asking what the first one was already asking.
	asking *asked
}

type asked struct {
	done  chan struct{}
	holds money
	err   error
}

// read answers from the kept copy while it is young enough, and otherwise asks.
func (c *recent) read(ctx context.Context, ask func(context.Context) (money, error)) (money, error) {
	c.mu.Lock()

	if !c.at.IsZero() && time.Since(c.at) < keepsFor {
		held := c.holds
		c.mu.Unlock()

		return held, nil
	}

	call := c.asking
	if call == nil {
		call = &asked{done: make(chan struct{})}
		c.asking = call

		// The reader who started the question is not the only one waiting on it, so
		// their leaving must not cancel it: WithoutCancel keeps the values their
		// context carries and drops its deadline.
		go c.pull(context.WithoutCancel(ctx), call, ask)
	}

	c.mu.Unlock()

	select {
	case <-call.done:
		return call.holds, call.err
	case <-ctx.Done():
		// This reader gave up; the question stays asked for whoever else is waiting.
		return money{}, ctx.Err()
	}
}

func (c *recent) pull(ctx context.Context, call *asked, ask func(context.Context) (money, error)) {
	ctx, stop := context.WithTimeout(ctx, asksWithin)
	defer stop()

	held, err := ask(ctx)

	c.mu.Lock()
	call.holds, call.err = held, err

	if err == nil {
		c.holds, c.at = held, time.Now()
	}

	c.asking = nil
	c.mu.Unlock()

	close(call.done)
}
