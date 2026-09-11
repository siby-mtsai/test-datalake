# module: athena-workgroup

Phase 1. Provisions one Athena workgroup per consumer (analytics, forecasting, audit), each with
its own results location (`athena-results/{workgroup}/`, 7-day lifecycle), a per-query
bytes-scanned limit, and a monthly cost alarm (brief Sections 4 and 6).

Not yet implemented — blocked on AWS access (see [`../../../docs/STATUS.md`](../../../docs/STATUS.md)).
