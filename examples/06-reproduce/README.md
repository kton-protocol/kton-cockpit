# 06 - reproducing adds a signature, not a record

Publish the same work twice and the registry does not grow. The second publish returns the *same*
foton id.

A foton id is the hash of the descriptor — the input and output names and hashes, and the command.
Identical work therefore has an identical id, and a second producer's signature is unioned onto the
record that already exists. So reproducibility here is not a report that says "matched"; it is the
absence of a new record.

Change one byte of the output and the id changes, and *that* is a new record. Without this control
the demonstration would be worthless: if identical work collapsed into one record for any reason
other than being identical, different work would collapse too.

## The count

`ask reproductions` reports signers whose signature actually verified against the keys this repo
configures — not the `keyid` an envelope declares about itself, which its author wrote and which
proves nothing. A count taken from declared keyids can be inflated by relabelling.
