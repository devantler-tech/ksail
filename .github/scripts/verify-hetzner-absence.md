# Verify Hetzner resource absence

After a failed provider trial, a successful fallback cleanup proves only its own
labelled resource families. To obtain a broader observation, run **Verify Hetzner
Resource Absence** on `main`, entering `VERIFY_HETZNER_ABSENCE` as confirmation.
It uses the existing dedicated CI test project, creates nothing and deletes nothing.

The workflow lists servers, primary IPs, floating IPs, volumes, load balancers,
networks, firewalls, placement groups, SSH keys, and snapshot/backup images. It
prints only family counts, the observation's source/run/time and an aggregate verdict.

It reads the raw [Cloud API](https://docs.hetzner.cloud/reference/cloud) rather
than a CLI-normalized list. Every page must contain the resource array and valid
pagination metadata. Missing/null arrays, repeated IDs, inconsistent totals and
failed reads are unverified. Each family is bounded to 100 pages of 50 entries;
exceeding that bound is also unverified. Deprecated snapshots/backups are included.
The workflow's overall deadline still bounds an interrupted observation.

| Verdict               | Meaning                                                            |
|-----------------------|--------------------------------------------------------------------|
| `EMPTY` (exit 0)      | All ten complete inventories were empty at observation time.       |
| `NONEMPTY` (exit 1)   | Every read completed, but at least one family contained resources. |
| `UNVERIFIED` (exit 2) | A prerequisite, provider read or response validation failed.       |

A timeout is incomplete evidence. The workflow retains no resource identifiers or
provider error payloads in logs or artifacts. It excludes vendor system/app images.

An empty project establishes absence without guessing ownership labels. A nonempty
project does **not** establish that a resource leaked: shared assets or another run
may account for it. Resolve ownership privately and use the existing scoped cleanup
path; this workflow never authorizes a broad deletion.

The observation does not automatically close a cleanup blocker or permit a new
trial. Recheck competing Hetzner and Omni runs, cleanup ownership, the daily trial
limit, the candidate head and its acceptance gates immediately before admission.
The shared Hetzner concurrency group prevents overlap with that workflow; it does
not serialize Omni. Keep the original trial's teardown owner until cleanup is
accepted. Source merge alone supplies no provider evidence.
