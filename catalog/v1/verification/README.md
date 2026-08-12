# catalog/v1/verification/ — signed audit reports

This directory holds the **signed verification reports** that
promote a track from `license_status: "NEEDS CONFIRMATION"` to
`"VERIFIED"`. Each report is a Markdown file keyed by track id,
sitting beside the bundled catalog at `catalog/v1/manifest.json`.

## Path contract

Every file in this directory follows:

```
catalog/v1/verification/<track-id>.md
```

The loader matches a track to its report by filename equality
against `Track.ID` per spec #287 / PR-D #1.4.

## Required sections

A valid report MUST include the three sections below. The parser
in `internal/license/parser.go` reads them line-by-line and
rejects the report if any required field is missing or malformed.

| Section | Field | Shape |
|---|---|---|
| `## License Claim` | `License URL` | absolute URL (`scheme + host`) |
| `## License Claim` | `HTML snapshot` | repo-relative path to the cached license HTML |
| `## Audio Integrity` | `Re-hashed SHA-256` | exactly 64 lowercase hex chars (`^[0-9a-f]{64}$`) |
| `## Operator Signature` | `Name`, `Email`, `Date` | name + email + ISO 8601 date (`YYYY-MM-DD`) |

The first H1 (`# Verification Report: <id>`) carries the track id
the report applies to. A leading `# Verification Report:`
followed by the id is the only header the parser reads at the
top level; the rest of the report is bullets under `## ` sections.

## Optional fields

Anything not in the table above is **ignored** by the parser and
may be added freely (e.g. an explanatory `Note:` under the
operator signature). The placeholder report in this directory
uses a `Note:` field to flag itself as not yet real.

## What this report does NOT do

Per spec #287, the parser does **not** verify signatures
cryptographically. The report is a human-checked contract and
`Track.LicenseStatus` is the loader's signal of trust: a track
without a report (or with an invalid one) stays under
`NEEDS CONFIRMATION` in the TUI's "Unverified licenses" group.

## How to validate a report

Run the parser's test suite. It exercises every required-field
shape plus the placeholder contract from spec #287 (the 64-zero
SHA-256 of the empty input, which the audio checksum layer
rejects separately).

```
go test ./internal/license/...
```

A future `lofi verify-report <path>` may surface this logic at
the CLI; today the test suite is the authoritative contract.

## Reference implementation

The canonical parser lives at
[`internal/license/parser.go`](../../internal/license/parser.go).
Changes to the report format MUST land there first (RED test →
GREEN implementation) so the test fixtures and the on-disk
placeholder stay in sync.