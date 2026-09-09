# Deferred specifications

Everything in this directory is **an open idea, not scheduled work**.

Nothing here is authoritative. Nothing here should be implemented, cited as a
contract, or treated as a decision the project has taken. A spec moves here
when it is still under discussion and its presence alongside active work was
causing it to be read as a commitment.

## What is authoritative

| Question | Where the answer lives |
|---|---|
| What the language means today | `docs/reference.md` |
| What is being worked on now | `docs/status.md` and the specs directly under `docs/specs/` |
| What was decided and built | the code, and the commit that landed it |
| Why something was decided | Git history |

If a deferred spec disagrees with `docs/reference.md`, the reference wins.

## Reading a status line

Each spec's `Status:` reads:

```text
Open Discussion; not scheduled. Design state: <its own maturity>
```

The first half is the same for every file here and says only that the idea is
not on the schedule. The second half is that spec's actual design maturity, and
it varies — some are early sketches, some have settled designs that were
descheduled rather than unresolved. Two examples worth not misreading:

- `0134` and `0139` say `Implementation-ready` in their design state. That is
  accurate: their designs are settled. They are here because they are not
  scheduled, not because anything about them is unresolved.
- `0152` says `Design settled; implementation blocked`. Its blocker is a spec
  outside this directory.

Deferring is a scheduling decision. It says nothing about whether a design is
finished.

## Two files here are not open discussion

`0148-span-and-mutable-span.md` and `0150-slice-syntax.md` are **superseded** —
0148 by 0150, and 0150 in turn by the active RFC 0153. They are closed history,
not ideas awaiting a decision, and their status lines say so rather than
carrying the `Open Discussion` prefix.

They are misfiled here. Superseded specs belong in `docs/specs/archive/`, or
deleted outright with Git as the record. Left in place they invite exactly the
confusion this directory exists to remove: a reader scanning for open ideas
finds two documents describing designs that have already lost.

## Picking something back up

Move the file to `docs/specs/`, replace the `Open Discussion; not scheduled.
Design state:` prefix with the design state alone, and add a `docs/status.md`
row. Verify its claims against the current tree first — a deferred spec may
have gone stale while the code moved underneath it.
