// Command backtest runs a strategy declaration over historical data. It reads
// the same declaration the gateway enforces in live trading - there is no second
// copy of the strategy.
package main

import "log"

func main() {
	log.Println("backtest: no declaration given")
}
