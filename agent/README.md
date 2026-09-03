# What the agent reads

Prompts and skills for the trading session. Nothing here holds a credential, and nothing here carries a limit.

Where its orders go is not decided here either. `BROKER_MCP_URL` is the policy gateway, which stands in front of the broker's own server and is the only thing that can refuse an order and record that it happened. The session holds a gateway token and no broker key. `docs/architecture.md` keeps the current state of that.

## Skills

`skills/` holds one directory per skill, each with its own `SKILL.md`. A skill carries an instruction the session needs only sometimes. What it needs always stays in `AGENTS.md`, which is read whole.

Which of them an agent gets is its declaration's to say, under `skills:`, by the name in the skill's own front matter. The set is worth narrowing because the description of every skill an agent carries goes into the prompt of every turn, so an agent should carry its own and nobody else's. It is a property of the agent rather than of a session: the agent reads its skills directory once when it starts, and one process serves every session, so there is no directory to narrow for a single window. Which technique a session uses is still chosen the way it always was - the task asks for it by name.

Putting them in place is part of putting a declaration in force, not a step beside it: `/work/.agents/skills/` - the directory the agent reads inside the one it works in - is made to hold exactly the skills the declaration named, and a declaration whose skills cannot be laid out never goes into force. Replaced whole, not merged into: `/work` outlives the image, so a skill deleted here has to disappear from the session too. A name nothing answers to is a failure to start, not a session quietly missing an instruction.

### An edit while a session runs

The clock asks once a minute, and the same tick that re-reads the declaration brings that directory level with what the source holds now. A session opens `SKILL.md` while it works, so **editing the text of a technique reaches a session that is already running** - no image rebuilt, no process restarted. A pass that finds nothing moved writes nothing: the set is fingerprinted by its contents, not by a modification time.

The way in is to mount a checkout over the directory the skills are **read from**, and to say so with `SKILLS_DIR`. The local stack in `compose.yaml` mounts `./agent/skills` at `/mnt/skills`, read-only, and points `SKILLS_DIR` at it.

Mounting over `/work/.agents/skills` instead - the directory the session reads - is refused, and the refusal says this. It would hand the session every skill in the checkout rather than the ones its declaration named, so the same declaration would behave one way on a developer machine and another on a deployment; and with two agents on one checkout, neither would get what it asked for. Tuning is done locally, so local has to be what ships.

### What may be deleted

That directory is rebuilt, which means deleted, and the only thing that entitles the process to delete it is a mark it left there itself: `.laid-by-the-agent`, holding the fingerprint of the set it wrote. **A directory that is there and carries no mark is left alone and the process refuses to start**, saying so.

The mark rather than the look of the contents, because a copy that looks like ours may not be ours. The mark rather than the mount table, because the mount table is not always there to read - on a machine without `/proc` "nothing is mounted" is a guess, and a guess in favour of deleting somebody's files is not one to make. The mount table is still read where it can be: it gives the more exact reason when a mount is what happened.

There is a second way to prove it, and it exists so that losing the mark cannot cost a week: when the directory already holds, byte for byte, what this pass would put there, whoever wrote it wrote what we would have written. Then the mark is written back, **nothing is deleted**, and the log says the directory was adopted rather than rebuilt. Contents that are anything else prove nothing about who put them there, and the refusal stands.

**A deployment that predates this** meets one of those two. Its work volume outlives the image, so it already holds a `/work/.agents/skills` copied by the entrypoint of an older version, with no mark in it. If that copy is exactly what its declaration names, it is adopted on the first start and there is nothing to do. If it is not - the declaration narrows to fewer skills than the image carried, say - the agent refuses to start and names the directory. It lives in a named volume, so removing it takes a container of its own; the stack is down anyway, since the agent is the thing that will not start:

```
docker compose --env-file .env down
docker run --rm -v swiftward-alpaca-options-agents_agent-work:/work alpine rm -rf /work/.agents/skills
docker run --rm -v swiftward-alpaca-options-agents_agent-near-work:/work alpine rm -rf /work/.agents/skills
docker compose --env-file .env up -d
```

The next start builds it from `SKILLS_DIR`. Nothing is deleted on the operator's behalf without being asked, which is the point of the rule.

### Numbers

A skill holds the technique and an example number. The number actually used comes from the declaration, under `parameters:`, and stands at the top of every turn - whichever of the three causes woke it, a scheduled window, the session's own wake-up or a person in the chat. A skill says in its front matter which ones it cannot work without:

```yaml
name: playbook-premium-harvest
requires: [short_leg_delta, min_edge_points, min_edge_points_borrowed]
```

A declaration that does not give one of them is refused, by name. This is not tidiness: two accounts run the same playbook side by side precisely so those numbers can differ, and a skill quietly falling back on its own example is how the two become one account run twice.

## Limits

They are not here, and that is the point. No ceiling, no list of underlyings and no permitted expiration appears in any task or any skill in this directory: the session asks `read_envelope` and is told what applies to it.

What it is told comes from `policy/envelope.yaml`, which is the operator's and lives outside this directory because nothing here reads it - not even by accident. The image the session runs in does not carry it.
