# Trial: the ladder, and whether it walks an order without being asked

**What it asks.** The session places two orders at prices the book is not
showing and then does nothing at all. The ladder should move both a cent at a
time toward the showing price, stop at it, and cancel the one that cannot reach
it before patience runs out.

**Why the book is flat.** Every quote in this scenario is the same at zero
minutes and at forty-five. A price that changes gives the ladder cover: an order
that fills could have filled because the market came to it. Here nothing comes to
anything, so a move is the ladder's move and a fill is the ladder's fill.

**The two orders, and the two bounds.** The ladder never crosses the session's
own worst price, and it never pays more than the book is asking. Each order is
built so that a different one of those two bounds is the binding one.

```
A  book shows 1.95 - 1.05 = 0.90   placed at 0.95, worst accepted -0.85
   the BOOK binds: five cents away, 45s a step -> reaches 0.90 and fills there,
   and must not go past it into the extra five cents the floor would allow

B  book shows 0.55 - 0.14 = 0.41   placed at 0.75, worst accepted -0.70
   the FLOOR binds: it walks five cents to -0.70, stops, and is cancelled on
   patience without ever reaching the book
```

Both outcomes matter, and the second more than the first: a ladder that only
fills is one nobody has watched give up.

**The worst price is not decoration.** It travels in the order's NAME -
`worst=-0.85;turn=…` - because that is the only field a broker carries untouched
and returns on every read (`internal/execution/reservation.go:21`). An order
without it is left exactly where it was placed, and the ladder says so every
pass. That is how the first run of this trial ended: the bundle did not ask for
the field, and eight passes were logged as *"left an order alone: its name
carries no worst price"*. The ladder was right and the trial was wrong.

**What separates the outcomes**

| outcome | sign in the record |
|---|---|
| **right** | A fills near 0.90 after several replacements; B is cancelled unfilled around the eleventh minute; `execution_steps` holds a row per move |
| too eager | A fills at 0.95 straight away, or the limit jumps to the showing price in one move rather than by the step |
| **past the book** | A fills BELOW 0.90 - the ladder walked past the price the market was showing, which is money given away |
| patient forever | B is still resting at seventeen minutes: patience is not being counted |
| blind | neither moves at all: the ladder is not seeing the orders |

**What to read afterwards**

```
psql "postgres:///arena_agent_N?host=/var/run/postgresql" -c \
  "select at, action, was, became, order_ref from execution_steps order by at"
sqlite3 arena/arena.db "select o.submitted_at, o.status, o.limit_price, o.filled_avg, o.client_id
                        from orders o join books b using(token_hash) order by o.submitted_at"
grep -o '"logger":"execution"[^}]*' arena/agent-N.log
```

**What this trial does NOT show.** Whether walking the price is a good idea, or
whether 45 seconds and a cent are the right numbers. It checks that the code
holding them runs, moves by the step it was given, respects the two ceilings it
was given - the book's price and the session's own - and gives up when told to.
