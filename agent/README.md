# What the agent reads

Prompts and skills for the trading session. Nothing here holds a credential, and nothing here carries a limit.

Where its orders go is not decided here either. Today the session reaches the broker's own server directly, over `BROKER_MCP_URL`, on the development account: the policy gateway is not yet in front of it. When it is, an address changes and this directory does not - the session holds no broker key either way. `docs/architecture.md` keeps the current state of that.

## Skills

`skills/` holds one directory per skill, each with its own `SKILL.md`. The agent is not given them here: on every start the entrypoint replaces `/work/.agents/skills/` - the directory the agent reads inside the one it works in - with this one. Replaces, not merges: `/work` outlives the image, so a skill deleted here has to disappear from the session too.

A skill carries an instruction the session needs only sometimes. What it needs always stays in `AGENTS.md`, which is read whole.

## Limits

They are not here, and that is the point. No ceiling, no list of underlyings and no permitted expiration appears in any task or any skill in this directory: the session asks `read_envelope` and is told what applies to it.

What it is told comes from `policy/envelope.yaml`, which is the operator's and lives outside this directory because nothing here reads it - not even by accident. The image the session runs in does not carry it.
