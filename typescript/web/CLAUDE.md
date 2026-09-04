# The page

The page a judge opens by hand. React, Tailwind, TypeScript, the Inter and Geist
Mono fonts. Built into `dist/`, served by the same process that serves the data.

## Colours and roles

`src/theme.css`, written for this page. Two layers, because they answer
different questions. The grey scale and the two meaning-carrying colours are raw
values with no opinion about where they go; the roles under them (`--bg`,
`--surface`, `--text-primary`, `--border`, `--accent`) say what a surface or a
border IS, and a theme redefines roles rather than values.

**Components take roles only.** Writing `bg-neutral-900` instead of
`bg-surface-raised` puts down a colour that does not know which theme it is in,
and one day it is black text on black.

The roles are bound to Tailwind utilities in `src/style.css`, in the
`@theme inline` block. That is where `bg-surface`, `text-muted`, `border-line`,
`rounded-xl` and the rest come from.

### Profit and loss are not the accent

`--gain` and `--loss` live in `src/style.css`, in a block of their own. The
accent is the SIGNAL - a live dot, a thing to click - and colouring profit with
the same green makes a green page mean both "it is running" and "we are up".

### The theme is fixed light

`data-theme="light"` sits on `<html>` and does not follow the reader's setting: a
judge opens this page and it must look the same for everyone. The dark values are
still in the files - the roles can do it - but they are never switched on.

**Tailwind's `dark:` variant is forbidden.** It fires on the reader's OS setting
and knows nothing about `data-theme`. On 28 August the equity curve carried
`bg-white dark:bg-neutral-900`: for a reader with a dark system the chart's
backing went black in the middle of a light page. That is also how to test it -
open with `colorScheme: 'dark'`, or the defect is invisible.

## When to make a reusable part

**Asked for more than once, it moves into `src/parts.tsx`.** One place means it
is written in place.

The rule is not about order but about divergence: from the second place onwards
two copies of the same thing start living their own lives, and a week later a
card has radius 12 in one section and 16 in another, and nobody remembers which
is right.

The reverse holds too: **do not make a part in advance**, for one case. A part
guessed before its second use almost always gets the wrong set of properties, and
it then gets broken rather than used.

Already extracted: `Section`, `Card`, `Chip`, `Table`, `Figure`, `Figures`,
`Empty`, `Unavailable`.

## Rules that outrank taste

**Emptiness and failure look different.** `Empty` means "nothing has happened
yet" and the reader will not refresh. `Unavailable` means "it did not answer" and
they will. One look for both cases leaves the reader guessing.

The same holds in the response codes, and it is worth remembering while reading
the page: `501` means "not configured here", our concern; `503` means "it did not
answer", somebody else's.

**Five requests, not one combined.** A combined answer would give one failure for
everything: the broker went down three times in a day, and in such a minute the
page would show nothing - not the agent's decisions, not the limits, not the
curve. Five give five independent failures, each with its own message. The
browser sends them in parallel.

**Tabular figures across the whole page.** Without them a column of numbers reads
as a staircase, and the eye recounts instead of comparing.

**The markup in the agent's words is parsed by us, not by a library.** Counted
across the lines collected so far: `**bold**` 15 times, `` `code` `` 14, two list
items. No headings, no links, no tables, no code blocks. A full parser costs a
hundred kilobytes on top of the hundred and eighty already added - a bad trade
for two constructs. Anything unfamiliar stays as text and looks no worse than it
did before parsing. If a third construct appears in any number, measure again and
decide again.

There can be no HTML injection here: React nodes are built, not a markup string,
and the text is escaped on output.

**An icon goes where it carries meaning.** The open and struck-through eye beside
a limit show whether the number is disclosed or only its existence - that is the
mechanism we are demonstrating. The arrow beside a change is a second cue next to
colour, for readers who do not distinguish it. An icon beside a section heading
carries no meaning and is not used.

**The chart does not draw data in.** Two prohibitions, and both cost an error on
the live page.

Smoothing (`type="monotone"`) is forbidden; `linear` only. A fill lifts the
account instantly; smoothing turned a vertical step into a gentle rise over six
hours - earnings spread across time that never happened.

A gap in the record is joined rather than broken, and what makes that honest is
the WINDOW rather than the drawing. The chart starts where the judged week starts,
and inside that week there is no gap: the only two this record holds are the hours
the deployment was down before trading began, on days when the account stood at
its opening balance and nothing was happening to miss.

It was the other way round until 4 September, and the reason is worth keeping: on
27 August ten hours passed between two neighbouring rows, and a line through the
hole showed steady growth where we measured nothing. A break said "we do not know
here", which was true. It also left a white notch that reads as a chart which
failed to draw, and a reader cannot tell that from a deployment that was down.
The cost of the trade is real and named in `Equity.tsx`: if the deployment goes
down mid-week, the segment across the hole will claim a measurement nobody took.

A judge reads the chart as evidence. A pretty curve drawn in between the points
is numbers that were never on the account.

**The chart comes from a library (recharts), not from hand.** The decision
changed on 28 August: a bare polyline really is twenty lines, but axes and a
tooltip under the cursor mean scale parsing, ticks, hitting the nearest point
with the mouse, and a popup that does not run off the edge. By hand that is
already a small chart of one's own, and it will need fixing. The price is known
and accepted: +120 KB compressed, and the page is opened once.

The axes are not decoration. Without them the reader sees the shape and not the
magnitude: the caption said "low $99,954.75 · high $100,074.40" and the eye had
nowhere to put those numbers, so every jump looked equally large.

## Looking at it locally

```
npx vite dev
```

It opens on 5173 and the data is proxied to 8080, where Go serves it. The proxy
is in `vite.config.ts` and it is required: without it a request to `api/money`
goes to 5173, Vite answers with its own `index.html`, and the page trips over
`<!doctype` while parsing JSON. It looks like "nothing works" when it is really
knocking at the wrong door.

The stack has to be up for this: `docker compose up -d` in the root.

## Check before shipping

```
npx tsc --noEmit && npx vite build
```
