# Data Classification and Retention

Per brief Section 5. This table is a Section 12 checkpoint: sign-off required from Rafeeq (CTO)
and whoever owns the Budapest (GDPR) / Toronto (PIPEDA) compliance question **before Phase 1
starts for real**. Bracketed values are placeholders to confirm against the live system and
Phase 0 inventory.

> **Table names below are from `mtsai-api-sim`** (synthetic stand-in, `docs/STATUS.md`), mapped
> onto the brief's five classes to prove the classification scheme actually covers a real (if
> synthetic) schema end-to-end. Retention periods are still bracketed placeholders — those are
> business/legal decisions, not something Phase 0 discovery can determine from a schema alone,
> synthetic or real. Table names need re-confirming against the real `mtsai-api` schema.

| Class | Examples | Tables (`mtsai-api-sim`) | Postgres retention | Lake retention | Personal data? |
|---|---|---|---|---|---|
| Hot operational | Active accounts, wallets, current zone configuration, rules | `accounts` | Indefinite | Nightly snapshot to raw for reporting only | Yes |
| Warm events | Trip events, ANPR camera events, reward and penalty ledger entries | `trip_events`, `anpr_camera_events`, `reward_ledger` | [90] days | Per jurisdiction policy, default [7] years for ledger, [12] months for raw camera events | Yes (vehicle and account identifiers) |
| Cold reference feeds | MapmyIndia, TomTom, Google Routes responses, weather | `reference_feed_cache` | [30] days | [3] years | No |
| Derived aggregates | Hourly zone occupancy, congestion indices | `zone_occupancy_hourly` | Rolling [1] year | Indefinite | No |
| Test and synthetic | CITY_ZZ / Cityville fixtures | Rows with `city_code = 'CITY_ZZ'` inside the tables above (~5% of rows) | As needed | Excluded from the lake entirely | No |

## Rules that must hold

- Any table containing personal data must carry a stable, indexed identifier (`account_id` or
  `vehicle_id` hash) so erasure can be executed as a single Iceberg `DELETE` per identifier.
- Where the platform already uses HMAC blind indexes or per-account DEKs (see LLD-003), the lake
  stores the blind index or ciphertext, never the plaintext identifier.
- Retention periods are per jurisdiction. Store the jurisdiction on every row (a `jurisdiction`
  column derived from `city_code`) so lifecycle and erasure jobs can be written once.
- Synthetic and test data never enters the production lake. Filter on `city_code` and on the
  environment the export runs in.

## Sign-off

- [ ] Rafeeq Ebrahim (CTO)
- [ ] Budapest (GDPR) compliance owner
- [ ] Toronto (PIPEDA) compliance owner

Status: **draft — table-to-class mapping demonstrated against a synthetic stand-in schema
(`mtsai-api-sim`); retention placeholders and table names both still need confirming against the
real `mtsai-api` schema once access exists. Not yet circulated for sign-off.**
