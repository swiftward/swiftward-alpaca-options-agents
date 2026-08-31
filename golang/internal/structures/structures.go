// Package structures names every option structure our declarations can open.
//
// It exists because a guard written for the structures of the day keeps running
// unchanged when a new one arrives, and says nothing. That is not a hypothetical:
// the profit watch decided what to close by counting LEGS, which is right while
// only verticals are held and wrong from the first backspread - and on 31 August
// it put a closing order against a convexity layer three minutes after the layer
// was opened.
//
// So the shapes live in one list, and every guard that treats shapes differently
// is tested against all of them. Adding a shape here fails those tests until
// somebody says what each guard does with it, which is the decision that was
// missed before.
package structures

// Shape is one structure a declaration can open, described by what it holds
// rather than by what it is called: the guards see quantities, not names.
type Shape struct {
	// Name is what the team calls it in the declarations and in chat.
	Name string
	// Sold and Bought are the contracts per set on each side. A credit vertical
	// is one and one; a backspread sells one and buys two.
	Sold, Bought int
	// ClosedByTheProfitWatch says whether the watch that buys structures back at
	// a share of their credit may act on this shape.
	//
	// False for anything that is not a credit vertical, and the reason is not
	// caution: a backspread is not paid by decay at all. Its money is in a move
	// that has not happened yet, and the session that opened it declared how long
	// to hold it, so buying it back the moment its small credit returns throws the
	// position away for a few dollars.
	ClosedByTheProfitWatch bool
}

// All is every shape, and the list a guard's test must cover completely.
var All = []Shape{
	{Name: "credit vertical", Sold: 1, Bought: 1, ClosedByTheProfitWatch: true},
	{Name: "backspread", Sold: 1, Bought: 2, ClosedByTheProfitWatch: false},
	{Name: "ratio spread", Sold: 2, Bought: 1, ClosedByTheProfitWatch: false},
}
