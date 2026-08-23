# Strategy declarations

One file per strategy. A declaration says when the strategy applies, what structure it opens, and the maximum loss it accepts.

Two consumers read the same file: the backtester over history, and the policy gateway during live trading. There is no second copy of the strategy in prompt form.
