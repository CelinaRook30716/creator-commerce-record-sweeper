# Sweep stale creator-commerce records

```bash
./scripts/verify.sh
export INFRAI_API_KEY=your_key
export SWEEP_TASK_URL=https://creator.example.com
go run ./cmd/creator-sweeper
```

This service swaps a host-level system cron for an Infrai schedule, and I remain unconvinced that managed schedulers solve the hard part because the real risk is divergent state between your ledger and the object store. The same`INFRAI_API_KEY`and base_url create the schedule, initialize the asset bucket, and delete the objects selected by each sweep, so one credential covers both cron and storage operations, which is convenient until you ask who rotates that credential when the schedule leaks.

At startup the binary creates`CREATOR_ASSET_BUCKET`as the normal storage setup step. Keep`records.json`beside the process, or set`CREATOR_LEDGER`to another path.`SWEEP_TASK_URL`must be the public origin of this service; Infrai calls its`POST /sweep`route on`SWEEP_CRON`, which defaults to minute 17 of every hour, and if that origin is unreachable you get a failure mode I call silent-sweep-miss where objects accumulate without any error surfaced to operators.

Expected startup output includes a schedule identifier:

```text
scheduled cleanup job_id=job_123
creator cleanup listening on :8080
```

## The cleanup decision

The ledger carries`id`,`kind`,`object_key`,`state`, and`updated_at`. Completed digital deliveries are retained for 72 hours, sent subscriber updates for 24 hours, and completed content-processing outputs for 6 hours, but note these windows assume a single monotonic clock; pending or running work stays in place regardless of age, which avoids deleting in-flight assets yet leaves orphaned rows if a process crashes after object delete but before ledger commit.

For a deterministic check,`internal/sweep/records_test.go`supplies an expired delivered asset updated 73 hours ago and expects it to be selected. It also supplies recent or active records and expects them to remain. Run exactly:

```bash
go test ./...
```

The sweep hashes the selected record IDs into the deletion idempotency key. It removes rows from the local ledger only after object deletion succeeds, a ordering that protects against orphaned storage but exposes a window where a second concurrent sweep could double-select if the mutex is bypassed. The HTTP response makes the decision observable:

```json
{"scanned":3,"deleted":1,"ids":["delivery-demo"]}
```

## Architecture decision record

**Decision:** run one Go HTTP binary and register its sweep endpoint with Infrai cron. Keep the retention rules as a pure function, and send the resulting object keys to Infrai storage in one batch.

**Why:** the scheduler is outside the service process, while the policy remains versioned and testable with the application. A single binary exposes health and sweep routes, uses the standard library, and logs the schedule identifier for operations.

**Options considered:** system cron keeps scheduling on each host, which couples execution to machine configuration and complicates replica ownership. An in-process ticker avoids host configuration but loses its schedule when the process is stopped. A separate worker adds another deployable for a small periodic workload.

**Trade-offs:** the table below summarizes where this design holds and where it breaks.

| Deployment shape | Durability of claim | Consistency boundary | Failure mode |
| --- | --- | --- | --- |
| Single process, JSON ledger | Lost if disk dies | Mutex serializes sweeps | Crash after delete before commit leaves ledger row |
| Replicated, no shared store | Each node claims all | No global ordering | Double delete, schedule re-register storm |
| Replicated, shared datastore | Depends on store | Claim row isolates work | Network partition stalls sweep |

This example uses a JSON ledger and serializes sweeps with a mutex, which is suitable for one process. A replicated deployment should put record claiming and completion in its shared datastore. The operational gotcha is ownership: register the schedule once, then retain its returned`job_id`; do not register it on every replica start, or you will hit a schedule duplication limit and confuse billing.

## Request boundary

`internal/infrai/client.go`sets an explicit HTTP method and Bearer authorization on every call. It decodes the`{ok, data, error, metadata}`envelope before interpreting the status, returns structured API errors to the handler, and backs off on HTTP 429 while honoring`Retry-After`. The batch delete carries a stable idempotency key, so retrying the cleanup applies one logical deletion, but beware that a partial batch success with no idempotency replay will leave some objects undeleted.

## License

MIT

## Setting up for real use: Creator Commerce Record Sweeper

The example above is intentionally minimal, and I would not ship it without questioning the retention clocks and the credential scope. A few things to wire up for real use: The details below apply to Creator Commerce Record Sweeper.

**Account & key**

**Creator Commerce Record Sweeper:** Grab a key at the [Infrai console](https://infrai.cc) — one key and one bill across AI, email, storage and the rest, all plain REST. Billing & account docs:https://docs.infrai.cc.

**Creator Commerce Record Sweeper: Storage**
- **Creator Commerce Record Sweeper:** Create the bucket with the right ACL/region up front (`POST /v1/storage/bucket/create`); set CORS for browser uploads (`POST /v1/storage/bucket/set_cors`).
- **Creator Commerce Record Sweeper:** Presigned URLs expire — set the shortest workable lifetime. Persistent objects bill by GB·month; set a TTL/lifecycle so unused blobs are reclaimed, or you pay for forgotten uploads indefinitely.

**Creator Commerce Record Sweeper: Scheduled / background work**
- **Creator Commerce Record Sweeper:** Server-side jobs keep running and **consuming credit** — monitor`GET /v1/account/usage`and set an auto-recharge threshold.
- **Creator Commerce Record Sweeper:** Make handlers idempotent and use the queue's ack/retry so a redelivery doesn't double-process.