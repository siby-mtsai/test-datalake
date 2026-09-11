# Erasure runbook

Per brief Section 9. Outline only — to be rehearsed in Dev against synthetic accounts during
Phase 3, with timings and any deviations recorded back into this file.

**Trigger:** a verified erasure request for an account or vehicle, received through the identity
service process.
**Input:** the blind index or hashed identifier, the jurisdiction, and the request reference.

1. Record the request reference in `export-manifests/erasure/{date}/` before touching data.
2. For each raw and curated table listed in the classification table as containing personal data,
   run `DELETE FROM table WHERE identifier = :id` in Athena. Capture rows affected per table.
3. Run `expire_snapshots` with retention zero on those tables so time travel can't resurface the
   rows, then `remove_orphan_files`.
4. Verify: `SELECT count(*)` per table returns zero for the identifier. Record results against the
   request reference.
5. Confirm Postgres erasure was performed by the identity service for the hot window, and that any
   Glacier-tier objects containing the rows have been rewritten or expired.

**Trade-off:** erasure with time travel disabled loses the ability to reproduce historic reports
containing that identifier. This is the accepted trade-off.

## Rehearsal log

Not yet rehearsed. Fill in date, identifier used (synthetic), tables affected, and end-to-end time
once run in Dev (Phase 3).
