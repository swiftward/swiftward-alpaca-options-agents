# What the agent reads

Prompts, skills and MCP configuration for the trading session. Nothing here holds credentials: the session reaches Alpaca only through the policy gateway, and the gateway holds the broker keys.

## Skills

`skills/` holds one directory per skill, each with its own `SKILL.md`. The agent is not given them here: on every start the entrypoint replaces `/work/.agents/skills/` - the directory the agent reads inside the one it works in - with this one. Replaces, not merges: `/work` outlives the image, so a skill deleted here has to disappear from the session too.

A skill carries an instruction the session needs only sometimes. What it needs always stays in `AGENTS.md`, which is read whole.
