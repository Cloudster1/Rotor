# Working on Rotor

The rules every contributor follows are in `CONTRIBUTING.md`, imported here;
this file adds what is for an agent.

@CONTRIBUTING.md

## Before changing anything

Read `.claude/skills/gh/SKILL.md`. Branches are created with
`gh issue develop`; agree on issues and pull requests with the user before
creating them, and the user approves every merge. `.claude/hooks/branch-guard.sh`
enforces the branch rules and, before every push, runs the layers of
`./run-tests.sh` that the pushed files can affect: format, Go, Python, web and
the shell tests by default, and the stack, integration and browser layers with
`ROTOR_GATE=full`. CI runs every layer, so a quick local gate says what it
skipped. A gate that does not finish blocks the push.

## Exciter, Armature and Stator

Local checkouts sit beside this one, as `~/repos/Exciter` and
`~/repos/Armature`, and Stator from `praetorianer777/Stator`. Most of Rotor's
tooling and backend skeleton is adapted from Exciter (see `NOTICE`); read how a
sibling does a thing before doing it here.

## Toolchain

Go is pinned to 1.26 and Python to 3.13, both run in containers through the
`Makefile`. `docs/decisions.md` records why the control plane is Go and the
workflow runner is Python.
