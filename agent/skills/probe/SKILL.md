---
name: probe
description: Answer whether skills reached this session and where they came from. Use when you are asked what skills you have, whether you can see a skill at all, or where a skill is read from.
---

# Probe

This file exists so that "does the session see its skills?" is answered by reading rather than by assuming.

The image carries skills in `/agent/skills/`. On every start the entrypoint replaces `/work/.agents/skills/` with that directory, and `/work` is where you work - which is why they reach you.

Asked about this, say three things: that you can see this skill, the path you are reading it from, and the name of every other skill you were given. Take the path from your own list of skills, not from this sentence.

It is a probe and nothing else. It says nothing about the broker, the market or what to trade, and it will be replaced by the first skill that does.
