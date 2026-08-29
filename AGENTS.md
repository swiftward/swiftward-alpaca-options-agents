# Working in this repository

## Language

**English only. This repository holds no text in any other language.** Code, comments, commits, pull requests, documentation, declarations, task text, session transcripts, file names, branch names, commit messages.

This is a hard rule with no exceptions and no "we will clean it up later": the repository opens with its whole history on submission day, so a line written in another language is published the moment it is committed. If you find one, translate it in the same change rather than reporting it.

The single thing that keeps its original wording is a quotation: a source quoted word for word stays as the source wrote it, because a translated quotation is no longer a quotation.

## Before you push

Run `make check`. It is the whole gate in one command: the style check, the language check above, the tests, the race detector, and both builds. Push only on a green one.

Do not lean on anything that runs after the push. The language check in particular is the one no other gate can stand in for - `go vet` and the tests pass happily on a comment in another language, and a line that reaches the history is published, not queued.

## Clients

No client of ours is named in this repository, and nothing is copied here out of a client's repository or folder - code, styles, documents, data. This repository is published whole, and what is committed stays in the history after it is deleted. Write our own.

## What is here

`agent/` - the declarations the trading session runs from, and `agent/AGENTS.md`, which is the session's own instruction, not this one.
`golang/` - the harness, the record and the gateway client.
`typescript/web/` - the page a judge opens.
`docs/` - how the submitted system is built.
Commands are in the `Makefile`; setup is in `README.md`.
