# Data Classification and Retention

Per brief Section 5. This table is a Section 12 checkpoint: sign-off required from Rafeeq (CTO)
and whoever owns the Budapest (GDPR) / Toronto (PIPEDA) compliance question **before Phase 1
starts for real**. Bracketed values are placeholders to confirm against the live system and
Phase 0 inventory.

| Class | Examples | Postgres retention | Lake retention | Personal data? |
|---|---|---|---|---|
| Hot operational | Active accounts, wallets, current zone configuration, rules | Indefinite | Nightly snapshot to raw for reporting only | Yes |
| Warm events | Trip events, ANPR camera events, reward and penalty ledger entries | [90] days | Per jurisdiction policy, default [7] years for ledger, [12] months for raw camera events | Yes (vehicle and account identifiers) |
| Cold reference feeds | MapmyIndia, TomTom, Google Routes responses, weather | [30] days | [3] years | No |
| Derived aggregates | Hourly zone occupancy, congestion indices | Rolling [1] year | Indefinite | No |
| Test and synthetic | CITY_ZZ / Cityville fixtures | As needed | Excluded from the lake entirely | No |

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

Status: **draft — pending Phase 0 inventory to confirm placeholders, not yet circulated.**
