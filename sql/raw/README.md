# sql/raw

Iceberg DDL, one file per raw table. Schema mirrors the corresponding Postgres table (brief
Section 4.1: raw is a faithful copy, never modified except for erasure). Populate once the Phase 0
inventory and classification table are complete.
