// Package reconcile compares the Postgres source against the Iceberg destination for the batch
// just exported (brief Section 7, Phase 2 step 3: "compare row counts and a checksum of the
// primary key set... any mismatch fails the run").
//
// The checksum is SUM(primary_key) rather than a cryptographic hash of the sorted key set: it's
// trivially computable identically on both sides (a plain Go sum over the rows already in memory
// on the Postgres side, a plain SQL SUM(...) on the Athena side) without needing to replicate a
// custom hash function in SQL. It's not collision-proof, but combined with an exact row-count
// match it catches the failure modes that matter here (dropped rows, duplicated rows, corrupted
// key values) without the complexity of matching a bespoke hash across two different engines.
package reconcile

import "fmt"

type Result struct {
	PostgresRowCount int64
	PostgresChecksum int64
	LakeRowCount     int64
	LakeChecksum     int64
}

func (r Result) Match() bool {
	return r.PostgresRowCount == r.LakeRowCount && r.PostgresChecksum == r.LakeChecksum
}

func (r Result) Error() error {
	if r.Match() {
		return nil
	}
	return fmt.Errorf(
		"reconciliation mismatch: postgres(count=%d, checksum=%d) != lake(count=%d, checksum=%d)",
		r.PostgresRowCount, r.PostgresChecksum, r.LakeRowCount, r.LakeChecksum,
	)
}
