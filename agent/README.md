# What the agent reads

Prompts, skills and MCP configuration for the trading session. Nothing here holds credentials: the session reaches Alpaca only through the policy gateway, and the gateway holds the broker keys.

## Skills

`skills/` holds one directory per skill, each with its own `SKILL.md`. The agent is not given them here: on every start the entrypoint replaces `/work/.agents/skills/` - the directory the agent reads inside the one it works in - with this one. Replaces, not merges: `/work` outlives the image, so a skill deleted here has to disappear from the session too.

A skill carries an instruction the session needs only sometimes. What it needs always stays in `AGENTS.md`, which is read whole.

## Limits

`envelope.yaml` holds the limits in force and who is under them. The session never reads it: it asks `read_envelope` and is told what applies to it, which is why no ceiling, no list of underlyings and no permitted expiration appears in any task or any skill here. Lowering one is an edit to that file, and the session reads the new number on its next question.

What is NOT in it is the point of it. How a trade is chosen - which leg to sell, what a structure must pay, when to close - is strategy, nobody grants it, and it lives in the playbook skill. Keeping the two apart is what stops the file from quietly becoming the strategy.
