# What the agent reads

Prompts and skills for the trading session. Nothing here holds a credential, and nothing here carries a limit.

Where its orders go is not decided here either. Today the session reaches the broker's own server directly, over `BROKER_MCP_URL`, on the development account: the policy gateway is not yet in front of it. When it is, an address changes and this directory does not - the session holds no broker key either way. `docs/architecture.md` keeps the current state of that.

## Skills

`skills/` holds one directory per skill, each with its own `SKILL.md`. A skill carries an instruction the session needs only sometimes. What it needs always stays in `AGENTS.md`, which is read whole.

Which of them an agent gets is its declaration's to say, under `skills:`, by the name in the skill's own front matter. The set is worth narrowing because the description of every skill an agent carries goes into the prompt of every turn, so an agent should carry its own and nobody else's. It is a property of the agent rather than of a session: the agent reads its skills directory once when it starts, and one process serves every session, so there is no directory to narrow for a single window. Which technique a session uses is still chosen the way it always was - the task asks for it by name.

On every start the process replaces `/work/.agents/skills/` - the directory the agent reads inside the one it works in - with the skills the declaration named. Replaces, not merges: `/work` outlives the image, so a skill deleted here has to disappear from the session too. A name nothing answers to is a failure to start, not a session quietly missing an instruction.

**Unless that directory is mounted.** Mount a checkout over it and the process leaves it alone, because the files in it are not its to delete. That is worth doing while a skill is being written: the session reads `SKILL.md` from disk as it works, so an edit reaches a session already running - no rebuild, no restart. The local stack in `compose.yaml` mounts it read-only for exactly that.

### Numbers

A skill holds the technique and an example number. The number actually used comes from the declaration, under `parameters:`, and stands at the top of every task. A skill says in its front matter which ones it cannot work without:

```yaml
name: playbook-premium-harvest
requires: [short_leg_delta, min_edge_points, min_edge_points_borrowed]
```

A declaration that does not give one of them is refused, by name. This is not tidiness: two accounts run the same playbook side by side precisely so those numbers can differ, and a skill quietly falling back on its own example is how the two become one account run twice.

## Limits

They are not here, and that is the point. No ceiling, no list of underlyings and no permitted expiration appears in any task or any skill in this directory: the session asks `read_envelope` and is told what applies to it.

What it is told comes from `policy/envelope.yaml`, which is the operator's and lives outside this directory because nothing here reads it - not even by accident. The image the session runs in does not carry it.
