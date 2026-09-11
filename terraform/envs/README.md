# terraform/envs

One directory per environment: `dev/`, `test/`, `preprod/`, `prod/`. Each will hold the root
module invocation and per-environment `.tfvars` (account ID, region, tags) per brief Section 8
conventions — no hard-coded account IDs. Work happens first in Dev
(`517293881120`, `miracletraffic-india-dev`) and promotes through Test and pre-prod via
feature → develop → main. Prod promotion is a Section 12 checkpoint.

Not yet implemented — blocked on AWS access (see [`../../docs/STATUS.md`](../../docs/STATUS.md)).
