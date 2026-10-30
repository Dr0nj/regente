# Enterprise compatibility contracts

This track extends existing scheduling, policies, diagnostics and executors. It
does not certify equivalence with another scheduler or a production environment.
Open work and status are maintained in [the roadmap](roadmap.md).

## First increment: offline AAPI inventory

From `server`, run:

```sh
go run ./cmd/inventoryaapi -in /private/definitions -source-version YOUR_SOURCE_VERSION > /private/inventory.json
```

The input can be a JSON file or a directory. Directory traversal includes hidden
`.folder.json` files and processes JSON documents independently; it does **not**
infer how split files are composed. The report contains relative filenames,
SHA-256 provenance and JSON pointers for every value, including empty containers,
arrays, unknown properties and unknown declared types. Array positions preserve
duplicate names. JSON keys with `/` or `~` use JSON Pointer escaping. Duplicate
object keys, trailing content, invalid Type values, nesting beyond 128 levels,
documents larger than 16 MiB and symbolic links are rejected. Input must be a
trusted local export; keep the directory unchanged during inventory.

All nodes are `inventoried-unverified`; `executable` is always false. No workspace
definitions, calendar defaults, remote requests or executable jobs are created.
No partial report is emitted when a document fails. Sorting and content hashes
make reports reproducible for unchanged inputs. A source-version label records
the operator's declaration; it does not validate the schema of that release.

Scalar values are omitted except declared Type labels. **This is not an
anonymizer:** filenames, pointers and Type labels can identify an organization.
Keep real inputs and reports private. Public examples and tests must be entirely
synthetic, including names, topology, counts and operational references.

The structural reference is the public BMC
[Job Properties documentation](https://documents.bmc.com/supportu/API/Monthly/en-US/Documentation/API_CodeRef_JobProperties.htm),
consulted on 2026-10-30. It documents named objects and array-based definitions.
Monthly is a moving reference, not a pinned compatibility certification. This
increment inventories JSON only; version-specific conversion needs a pinned
contract and corpus. Parsing coverage is not semantic or execution coverage.

## Completion gates for subsequent increments

| Area | Required evidence |
| --- | --- |
| Conversion | Every material field mapped or blocked with a reason; provenance and original identity retained; no partial executable definitions |
| Planning | Folder/date variable isolation, event date and boolean fidelity, imported calendars and inheritance, cycle and resource semantics |
| Observation | Immutable snapshots, source timestamps, missing evidence explicit; compare eligibility, readiness and admission separately |
| Shadow | No dispatch, remote writes, operational event publication or real notifications; opaque types remain unknown, never simulated success |
| Operations | Profiles and secret rotation, least privilege, folder authorization, audit export, proxy qualification and independent recovery |
| Adapters | Submission identity, terminal evidence, cancellation, lost-response/restart handling and no blind replay |
| Usage | Versioned definitions of order, attempt, rerun and shadow; reproducible aggregates; estimated cost distinguished from invoices |

Site-specific naming, calendar and alert rules are configuration, not universal
defaults. HTTP 200 alone is not SOAP success; shell return values alone do not
establish DataStage success; a stop request alone does not prove remote completion.
Read-only pilots still require authorization. Human operational acceptance is
separate from CI and synthetic tests.

## AWS Glue: local contract, no paid infrastructure

The existing `GLUE` / `AWS_GLUE` adapter submits an existing job with StartJobRun,
then observes GetJobRun. It supports job name, arguments, worker type/count and
the existing AWS connection parameters. It does not create Glue jobs, IAM roles,
buckets or infrastructure. CI uses loopback HTTP fixtures and synthetic
credentials; no AWS account, cloud endpoint or billable workload is needed.

The local contract now covers WAITING before completion, all documented terminal
states including EXPIRED, one submission when the start response is ambiguous,
and per-run stop acknowledgement. BatchStopJobRun can return HTTP 200 with errors.
Only an acknowledgement matching both job and run is described as accepted;
even then remote completion remains unconfirmed after a timeout.

This is a legacy adapter under qualification. Durable JobRun persistence,
restart reconciliation, context cancellation, modern credential-provider support
and network/secret policy integration remain GLUE-02. The restricted execution
policy remains unchanged. Do not enable cloud execution or automatic replay on
the strength of local mocks. GLUE-03 requires a separately authorized AWS
environment and cost budget; it remains deferred. No exactly-once guarantee.

Primary AWS contracts, consulted on 2026-10-30:

- [StartJobRun](https://docs.aws.amazon.com/glue/latest/webapi/API_StartJobRun.html)
- [JobRun states](https://docs.aws.amazon.com/glue/latest/webapi/API_JobRun.html)
- [BatchStopJobRun](https://docs.aws.amazon.com/glue/latest/webapi/API_BatchStopJobRun.html)

Job arguments can be logged by AWS; do not put plaintext secrets in arguments.
