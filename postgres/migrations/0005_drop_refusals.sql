-- The refusals table had no writer at all: a refusal comes from the gateway, and
-- the gateway is a separate service with no route into this database. An empty
-- section on the page reads as "the agent was never stopped", which is not the
-- same as "we do not know". It comes back together with the gateway that fills it.
--
-- What an order runs into today is visible in tool_calls: a failed call carries
-- the broker's message.

DROP TABLE IF EXISTS refusals;
