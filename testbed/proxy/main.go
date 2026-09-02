// The arena's proxy: one process for the whole arena, standing between the
// participants and the real Alpaca MCP.
//
// Four rules, and they are the whole design:
//
//  1. READS go straight through. Quotes, chains, snapshots, the clock, the news -
//     all of it real. Otherwise the instrument would be measuring not the agent
//     but our invention of a market.
//  2. ORDERS are intercepted. They never reach Alpaca at all; the book is kept
//     here, one participant per token, and an order in it STANDS until the market
//     reaches it - the way it does at a real broker, and not the way it does on a
//     paper account.
//  3. Anything not on the list is REFUSED BY NAME. Not out of caution: an arena
//     of a hundred agents must have no physical means of touching a real
//     portfolio, and the only way to guarantee that is a list of what is allowed
//     rather than a list of what is not.
//  4. Only NAMED participants are served. An empty roster serves nobody; an
//     unknown token gets a refusal rather than a fresh book.
package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// passthrough is what goes upstream unchanged.
var passthrough = []string{
	"get_clock",
	"get_stock_latest_trade",
	"get_option_chain",
	"get_option_contracts",
	"get_option_snapshot",
	"get_news",
}

// intercepted is what the book serves.
var intercepted = []string{
	"place_option_order",
	"get_all_positions",
	"get_account_info",
	"get_orders",
	"cancel_order_by_id",
	"replace_order_by_id",
}

type arena struct {
	up    *upstream
	store *Store
	start float64
	// maxQuoteAge is the age up to which a quote counts as a price rather than a
	// memory.
	maxQuoteAge time.Duration
	// ignoreSession lifts both checks on time - the clock and the freshness. For
	// a bench only: in a live arena a fill against a closed market is a trade
	// without risk.
	ignoreSession bool

	// The broker's clock is held apart from everything else, under its own lock:
	// it is read on every tick of the matcher and on every order, and those must
	// not queue behind the roster.
	clockMu   sync.Mutex
	heldClock Clock
	heldAt    time.Time
	// clockFor is how long one reading is reused. Zero means every read goes
	// upstream, which is what a bench wants.
	clockFor time.Duration

	// staged is a market read from a file instead of from the broker. Nil is the
	// normal case and the only one in which a number means anything.
	staged *stage

	// The real price of the overlay's underlying, held for a moment. Every
	// contract repriced in one answer must be repriced against the SAME spot, or
	// the legs of one spread come from two markets a few cents apart and the
	// width of the spread becomes our arithmetic rather than the scenario's.
	spotMu  sync.Mutex
	spot    float64
	spotAt  time.Time
	spotFor time.Duration

	mu sync.Mutex
	// roster holds the hashes of admitted tokens and their names. Empty means
	// NOBODY: refusal by default rather than admission by default.
	roster map[string]string
	books  map[string]*Book
	served map[string]*mcp.Server
	tools  map[string]*mcp.Tool
	said   map[string]bool
}

func main() {
	listen := flag.String("listen", env("ARENA_LISTEN", "127.0.0.1:8100"), "the proxy's address")
	broker := flag.String("broker", env("BROKER_MCP_URL", "http://127.0.0.1:8000/mcp"), "the real Alpaca MCP")
	start := flag.Float64("start", 100000, "a participant's starting balance")
	file := flag.String("store", env("ARENA_STORE", "arena.db"), "the SQLite file holding the books")
	// Tokens are better passed through ARENA_PARTICIPANTS than through a flag:
	// argv is visible through ps to every process on the machine, and these are
	// the access keys to the books.
	who := flag.String("participants", os.Getenv("ARENA_PARTICIPANTS"), "participants' tokens, comma separated (better through ARENA_PARTICIPANTS: argv is visible in ps)")
	every := flag.Duration("match", 2*time.Second, "how often the matcher looks at the market")
	age := flag.Duration("max-quote-age", 2*time.Minute, "the age up to which a quote is good enough to fill against")
	parallel := flag.Int("parallel", 4, "how many calls go upstream at once")
	quoteTTL := flag.Duration("quote-ttl", 1500*time.Millisecond, "how long a quote lives in the cache")
	ignore := flag.Bool("ignore-session", false, "fill against a closed market and stale quotes (bench only)")
	scene := flag.String("scenario", env("ARENA_SCENARIO", ""), "a staged market from this file instead of live prices (bench only)")
	flag.Parse()

	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	up, err := dial(ctx, *broker, *parallel, *quoteTTL)
	if err != nil {
		log.Fatalf("the broker %s did not answer: %v", *broker, err)
	}
	defer up.Close()

	tools, err := up.Tools(ctx)
	if err != nil {
		log.Fatalf("the list of tools was not read: %v", err)
	}

	store, err := OpenStore(*file)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	a := &arena{
		up:            up,
		store:         store,
		start:         *start,
		maxQuoteAge:   *age,
		ignoreSession: *ignore,
		clockFor:      clockHeldFor,
		// One spot per few seconds. Long enough that a snapshot of forty contracts
		// is repriced against a single price, short enough that a walk of the real
		// market is not lagged behind the overlay laid on top of it.
		spotFor: 3 * time.Second,
		roster:  map[string]string{},
		books:   map[string]*Book{},
		served:  map[string]*mcp.Server{},
		tools:   tools,
		said:    map[string]bool{},
	}

	// A participant is given as name:token or as a bare token. The name is for
	// the judge: books are told apart by the hash of a token and records by the
	// name of a database, and without a name the only way to join them is the
	// order they were created in. Order is not a join but a coincidence: create
	// them in a different order and the judge honestly stitches the wrong pair.
	for _, entry := range strings.Split(*who, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		name, token, ok := strings.Cut(entry, ":")
		if !ok {
			name, token = "", entry
		}
		a.roster[hashOf(strings.TrimSpace(token))] = strings.TrimSpace(name)
	}
	if len(a.roster) == 0 {
		log.Printf("the roster is empty: there is nobody to serve. Set -participants or ARENA_PARTICIPANTS")
	}

	// The upstream list is checked BEFORE the first participant: a name that
	// changed up there would otherwise surface in the middle of a trading day as
	// a refusal nobody expected.
	var missing []string
	for _, name := range append(append([]string{}, passthrough...), intercepted...) {
		if tools[name] == nil {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		log.Fatalf("the broker serves no tools named %s: the lists of names have parted ways", strings.Join(missing, ", "))
	}

	// The books are raised BEFORE anyone connects: a participant that arrives in
	// the first second must see yesterday's positions rather than emptiness.
	for hash := range a.roster {
		if _, err := a.bookFor(hash); err != nil {
			log.Fatalf("the book %s was not raised: %v", short(hash), err)
		}
	}

	m := &matcher{a: a, every: *every}
	go m.run(ctx)

	handler := mcp.NewStreamableHTTPHandler(a.serverFor, nil)

	log.Printf("the arena listens on %s, broker %s, start %.0f, participants %d, tick %s",
		*listen, *broker, *start, len(a.roster), *every)

	// A mode in which filling stops being honest is required to shout about
	// itself in the log - at startup and on every fill. Otherwise a measurement
	// taken at a weekend against a frozen book looks the next day exactly like a
	// real one: the price is guaranteed not to move, and every order "earns".
	if *scene != "" {
		loaded, err := LoadScenario(*scene)
		if err != nil {
			log.Fatalf("the scenario was not loaded: %v", err)
		}
		a.staged = newStage(loaded, time.Now())
		if loaded.Mode == "overlay" {
			// An overlay does NOT lift the session checks. Its clock, its quote
			// timestamps and its session are the real ones, so a closed market
			// must refuse exactly as it would outside - that refusal is often the
			// very thing being measured.
			log.Printf("WARNING: an OVERLAID MARKET is on - %s, from %s. Every read goes to the real "+
				"broker and the underlying is moved along that curve; each contract is repriced from "+
				"its own live volatility. The clock, the chain and the account are real. Every fill "+
				"is marked in the book: these numbers are NOT a measurement.", loaded.describe(), *scene)
			go func() {
				for range time.Tick(5 * time.Minute) {
					log.Printf("REMINDER: still on the overlaid market %q, %s now %+.2f from the real one. "+
						"Nothing here is a measurement.", loaded.Name, loaded.Underlying, a.staged.shiftNow())
				}
			}()
		} else {
			// A staged market must fill: the scenario carries its own clock and its own
			// timestamps, and refusing those as stale or shut would leave every scenario
			// unable to stage anything at all.
			a.ignoreSession = true
			log.Printf("WARNING: a STAGED MARKET is on - %q from %s. The prices, the clock and the "+
				"underlying come from that file rather than from the broker. Every fill is marked in "+
				"the book: these numbers are NOT a measurement.", loaded.Name, *scene)
			go func() {
				for range time.Tick(5 * time.Minute) {
					log.Printf("REMINDER: still on the staged market %q. Nothing here is a measurement.", loaded.Name)
				}
			}()
		}
	}

	if a.ignoreSession {
		log.Printf("WARNING: -ignore-session is on. Filling against a closed market and stale quotes. " +
			"This is bench mode: numbers obtained in it are NOT a measurement.")

		// And it is repeated every five minutes for as long as it is on. One line
		// at startup is lost in a day's log, and a bench mode forgotten on a live
		// market turns a measurement into an invention that looks like one.
		go func() {
			for range time.Tick(5 * time.Minute) {
				log.Printf("REMINDER: still -ignore-session. If the market is open, these numbers are not a measurement.")
			}
		}()
	}
	srv := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// serverFor hands a participant its own server. The identity is the token in the
// header, the same one the participant presents to the envelope.
//
// An unknown token IS NOT SERVED. An earlier version created a fresh book for
// it, which was the exact opposite of the design: a typo in a token became a
// button that reset the account, and an empty answer reads to an agent as "there
// are no positions", on which it starts trading. Their lesson, bought dearly.
func (a *arena) serverFor(r *http.Request) *mcp.Server {
	token := bearerOf(r)
	if token == "" {
		return nil
	}

	hash := hashOf(token)

	a.mu.Lock()
	allowed := false
	for known := range a.roster {
		// A constant-time comparison of hashes: their length is the same, so
		// there is nothing to leak through the time the comparison takes.
		if subtle.ConstantTimeCompare([]byte(known), []byte(hash)) == 1 {
			allowed = true
		}
	}
	served := a.served[hash]
	a.mu.Unlock()

	if !allowed {
		log.Printf("refused: the token %s... is not on the roster", short(hash))

		return nil
	}
	if served != nil {
		return served
	}

	book, err := a.bookFor(hash)
	if err != nil {
		log.Printf("the book %s was not raised: %v", short(hash), err)

		return nil
	}

	s := mcp.NewServer(&mcp.Implementation{Name: "arena-broker", Version: "v0.2.0"}, nil)
	a.register(s, book)

	a.mu.Lock()
	a.served[hash] = s
	a.mu.Unlock()

	log.Printf("participant %s... has connected", short(hash))

	return s
}

// bookFor raises a book from the file or creates a new one.
func (a *arena) bookFor(hash string) (*Book, error) {
	a.mu.Lock()
	if b := a.books[hash]; b != nil {
		a.mu.Unlock()

		return b, nil
	}
	a.mu.Unlock()

	book := NewBook(hash, a.start, a.store)
	a.mu.Lock()
	book.Name = a.roster[hash]
	a.mu.Unlock()
	found, err := a.store.Load(book)
	if err != nil {
		return nil, err
	}
	if !found {
		if err := book.persist(); err != nil {
			return nil, err
		}
		log.Printf("the book %s was created with a start of %.0f", short(hash), a.start)
	} else {
		// The name may have appeared after the book was created: the column did
		// not exist, or the participant was renamed. It is written at once rather
		// than at the first trade - otherwise, until then, the judge sees a book
		// with no name and stitches by order.
		if err := book.persist(); err != nil {
			return nil, err
		}
		log.Printf("the book %s (%s) was raised: cash %.2f, positions %d, orders %d",
			short(hash), book.Name, book.Cash, len(book.Positions), len(book.Orders))
	}

	a.mu.Lock()
	a.books[hash] = book
	a.mu.Unlock()

	return book, nil
}

func (a *arena) allBooks() []*Book {
	a.mu.Lock()
	defer a.mu.Unlock()

	out := make([]*Book, 0, len(a.books))
	for _, b := range a.books {
		out = append(out, b)
	}

	return out
}

// servable is every tool the arena answers. A scenario naming anything else in a
// fault is refused at load rather than kept: a fault on a tool nobody calls
// never fires, and it would sit in the file looking like a test that passed.
var servable = func() map[string]bool {
	all := map[string]bool{}
	for _, name := range append(append([]string{}, passthrough...), intercepted...) {
		all[name] = true
	}

	return all
}()

func (a *arena) register(s *mcp.Server, book *Book) {
	for _, name := range passthrough {
		s.AddTool(a.tools[name], a.faulted(name, a.relay(name)))
	}
	s.AddTool(a.tools["place_option_order"], a.faulted("place_option_order", a.placeOrder(book)))
	s.AddTool(a.tools["get_all_positions"], a.faulted("get_all_positions", a.positions(book)))
	s.AddTool(a.tools["get_account_info"], a.faulted("get_account_info", a.account(book)))
	s.AddTool(a.tools["get_orders"], a.faulted("get_orders", a.getOrders(book)))
	s.AddTool(a.tools["cancel_order_by_id"], a.faulted("cancel_order_by_id", a.cancelOrder(book)))
	s.AddTool(a.tools["replace_order_by_id"], a.faulted("replace_order_by_id", a.replaceOrder(book)))
}

// faulted lets a scenario take one tool away for a stretch of the run.
//
// It wraps every tool and not only the reads, because the two halves of the
// question are different. A read that stops answering asks whether the agent
// notices it is deciding without data. An ORDER that is refused asks whether it
// changes its approach or repeats the same refused call - which is what both an
// arena participant and one of the team's own agents did, thirty-two times and
// twice respectively.
func (a *arena) faulted(name string, next mcp.ToolHandler) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if why, out := a.staged.refuses(name); out {
			return refuse("%s", why)
		}

		return next(ctx, req)
	}
}

// relay carries a call upstream as it is - through the cache and the queue.
// Browsing the market stands last in the queue: orders and the matcher matter
// more.
func (a *arena) relay(name string) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Under a staged market the three reads that carry a PRICE are answered from
		// the scenario. The rest still go to the broker: a scenario has no business
		// inventing which contracts exist or what was in the news, and an agent that
		// reads a staged price against a real chain is reading the same world the
		// book fills against.
		if a.staged.staging() {
			if res, ok := a.stagedAnswer(name, req.Params.Arguments); ok {
				return res, nil
			}
		}
		// Under an overlay the same read goes upstream and comes back corrected:
		// the reality is the real one everywhere except in the one number the
		// scenario moves.
		if a.staged.overlaying() {
			if res, ok := a.overlaidAnswer(ctx, name, req.Params.Arguments); ok {
				return res, nil
			}
		}

		return a.up.Call(ctx, prioBrowse, name, req.Params.Arguments)
	}
}

// stagedAnswer serves one of the three price reads from the scenario, in the
// broker's own shape. The second value says whether this tool is staged at all.
func (a *arena) stagedAnswer(name string, args any) (*mcp.CallToolResult, bool) {
	var in struct {
		Symbols string `json:"symbols"`
	}
	_ = remarshal(args, &in)

	var body map[string]any
	switch name {
	case "get_clock":
		body = a.staged.clockJSON()
	case "get_stock_latest_trade", "get_option_snapshot":
		// A call that named no symbol is REFUSED, not answered emptily. Measured
		// on 31 August: a participant asked every market read with `symbol`
		// rather than `symbols` for a whole trial, got {"trades":{}} and
		// {"snapshots":{}} back, and read that as a market with no data in it -
		// it reported the position blind and unprotected for eight windows while
		// the staged price walked six dollars through its short strike. The
		// broker was answering the whole time. An empty success turns the
		// agent's own mistake into news about the world, which is the one thing
		// an instrument for measuring blindness must never do itself.
		syms := symbolsOf(in.Symbols)
		if len(syms) == 0 {
			res, _ := refuse("the staged market takes its symbols in `symbols`, comma separated, "+
				"and this call to %s named none%s. This is a refusal and not an empty market: "+
				"the price is there, the call did not ask for it.", name, argNames(args))

			return res, true
		}
		if name == "get_stock_latest_trade" {
			body = a.staged.tradeJSON(syms)
		} else {
			body = a.staged.snapshotJSON(syms)
		}
	default:
		return nil, false
	}

	res, err := answer(name, body)
	if err != nil {
		return nil, false
	}

	return res, true
}

// argNames lists the argument names a call actually carried, so a refusal can
// show the agent the word it wrote instead of the word the tool takes. Naming
// the missing parameter alone is not enough: the mistake that produced this
// refusal was a near-synonym, and the agent looked straight past it twice.
func argNames(args any) string {
	var got map[string]any
	if remarshal(args, &got) != nil || len(got) == 0 {
		return " and carried no arguments at all"
	}

	names := make([]string, 0, len(got))
	for k := range got {
		names = append(names, k)
	}
	sort.Strings(names)

	return " (it carried: " + strings.Join(names, ", ") + ")"
}

// hashOf is how a participant is named inside. The token is never stored in the
// clear: not in the file and not in the log.
func hashOf(token string) string {
	sum := sha256.Sum256([]byte(token))

	return hex.EncodeToString(sum[:])
}

func bearerOf(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const p = "Bearer "
	// The prefix is not a secret, and there is no reason to compare it in
	// constant time - the secret is what follows it, and that is what we compare
	// against the admitted hashes.
	if len(h) <= len(p) || !strings.EqualFold(h[:len(p)], p) {
		return ""
	}

	return strings.TrimSpace(h[len(p):])
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}

	return fallback
}
