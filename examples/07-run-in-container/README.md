# 07 - running in a pinned container

Needs a container engine. It fails rather than skips without one: a skip is how a suite reports
success having exercised nothing.

With `execution.image` set to a digest-pinned reference, `cockpit_publish` runs the command inside
it. The string handed to docker and the string pinned into the foton are the *same string*, so what
ran and what is recorded cannot diverge. Without execution configured, `envRef` is whatever the
caller says it is — a claim, like `cmd` — and that is a real difference in what the record is worth.

The image must carry a digest. A tag names whatever it points at today, so a reproduction committing
to "that image" would commit to nothing.

## Two things the example checks that are easy to miss

**An undeclared output is reported, not adopted.** The cockpit knows what the run changed, so it
says when a file appeared that the publish did not name. It does not add it: output paths and hashes
are part of the foton's identity, so an incidental file would make two identical runs produce
different records — destroying the property the container was for.

**A failed run publishes nothing.** The example takes the commit sha before and after and requires
it unchanged. "The call failed" and "the call failed and left nothing behind" are different
statements.
