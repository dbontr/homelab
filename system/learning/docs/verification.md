# Authenticated verification receipts

The learning service stores model evaluations and third-party statements as provisional claims. Only a receipt signed by an operator-trusted checker can satisfy the external-corroboration requirement for the highest mastery target.

## Trust boundary

Configure `ADAPTIVE_TEACHING_VERIFIER_KEYS` on the service as a JSON object mapping verifier identifiers to base64-encoded 32-byte Ed25519 **public** keys, for example `{"isolated-checker":"BASE64_PUBLIC_KEY"}`. A missing configuration means that no external verifier is trusted. Invalid configuration prevents startup. Do not expose verifier private keys to ChatGPT, this service, its database, or a learner's work container.

A separately administered checker must verify the actual submitted work in an approved isolated environment, retain its run report and test inputs, and sign a receipt only after the outcome is known. No untrusted learner code is executed by the learning server. The teaching server itself never executes learner code. A separate operator-controlled polynomial checker is built from `Dockerfile.verifier`; it checks exact polynomial identities and polynomial derivatives without executing submitted code. This limited mathematical verifier does not validate general proofs, experimental claims, programs or the learner's explanation. Registering a verifier key without independently reviewing the checker and its fixtures is not sufficient.

## Receipt format

The `record_verified_problem_check` tool accepts a `receipt` and `signature`. The receipt contains these required JSON fields:

```json
{
  "version": 1,
  "verifier_id": "isolated-checker",
  "problem_id": "uuid",
  "problem_sha256": "64-character-lowercase-hex",
  "draft_id": "uuid",
  "draft_sha256": "64-character-lowercase-hex",
  "method": "executable",
  "outcome": "pass",
  "evidence_reference": "artifact-reference",
  "evidence_sha256": "64-character-lowercase-hex",
  "toolchain": "checker and version",
  "checked_at": "2026-11-03T10:00:00Z"
}
```

`method` is one of `symbolic`, `formal`, `executable`, `experimental`, or `expert`. `outcome` is `pass`, `fail`, or `inconclusive`. The draft SHA-256 covers the **exact UTF-8 bytes** stored in `problem_drafts.body`; the problem SHA-256 covers the exact stored statement. The evidence digest identifies the independently retained report. The server checks digest syntax but does not fetch the evidence artifact.

The signed bytes are the UTF-8 prefix `adaptive-teaching/problem-verification/v1\n` followed by Go `encoding/json.Marshal` of the `verification.Receipt` struct in `internal/verification/receipt.go`. The `signature` is canonical standard base64 of the 64-byte Ed25519 signature over those bytes. Independent signers must serialize fields in this exact order without extra whitespace. An equivalent JSON object with a different byte representation is not a valid signature payload.

The server verifies the signature against its configured public key, checks the problem and newest draft identifiers and digests, rejects future timestamps or reports that predate the draft, and stores the original signed receipt with a durable check record. Re-submission of the same signed receipt is idempotent. A draft revision invalidates an earlier check for mastery calculation. Contradictory authenticated pass/fail receipts for the same draft block corroboration until a subsequent valid attempt resolves the disagreement.

## Operator-controlled polynomial verification

The `polynomial-verifier` executable is compiled independently from the learning service. Its `verify` subcommand takes two JSON files: a trusted fixture and an exact exported learner attempt. The fixture includes the stored problem ID and SHA-256, `kind` (`polynomial_identity` or `polynomial_derivative`), `expression` (an operator-reviewed reference expression or expression to differentiate), and a single lowercase variable, such as `x`. The attempt includes `problem_id`, `statement`, `draft_id`, and `draft`. The attempt must refer to the same saved problem and latest draft in the learning service. The operator, not the learner or AI host, supplies and reviews the fixture.

The expression language supports exact rational constants, a single variable, parentheses, addition, subtraction, multiplication, scalar division, and `^` with nonnegative integer exponents. Multiplication must be written explicitly (`2*x`). Expression length, exponent, polynomial degree, and coefficient sizes are bounded. A pass proves that the submitted *expression* is equal to the trusted polynomial (or its exact derivative) over rational coefficients. Unsupported notation yields an inconclusive result; unequal polynomials yield failure. It does not validate prose reasoning, assumptions or all cases of differentiation.

Generate a verifier key pair on a separately administered machine, with outputs outside source control:

```sh
polynomial-verifier keygen -id math-checker -private /secure/key.secret -public /secure/public.json
```

Only the public JSON belongs in `ADAPTIVE_TEACHING_VERIFIER_KEYS` on the teaching server. Keep the private file owner-only (`0600`). Run the checker in an isolated, non-root, read-only container with networking disabled, capabilities dropped, resource limits, and explicit read-only input and writable report mounts:

```sh
docker run --rm --network none --read-only --cap-drop ALL --security-opt no-new-privileges --pids-limit 32 --memory 128m --cpus 1 \
  --mount type=bind,src=/secure/inputs,dst=/input,readonly \
  --mount type=bind,src=/secure/key.secret,dst=/signer.key,readonly \
  --mount type=bind,src=/secure/outputs,dst=/output \
  adaptive-polynomial:reviewed verify -id math-checker -private /signer.key \
  -fixture /input/fixture.json -attempt /input/attempt.json \
  -report /output/report.json -receipt /output/receipt.json
```

The container image is built using `docker build -f Dockerfile.verifier`; it contains a static Go binary with no general-purpose shell, network client or Python runtime. The signer key must be readable only by the checker identity inside the container and is mounted read-only, separate from the writable output directory. The two output files must not exist before execution. The checker creates an immutable report and an Ed25519-signed receipt; the report's SHA-256 is included in the receipt. After verifying the fixture, toolchain, report and exact saved draft, an authorized host may submit the receipt and signature with `record_verified_problem_check`. Nothing automatically registers the checker as trusted or imports its result.

For higher-assurance deployment, manage signer keys outside mutable job workspaces and retain the signed report and trusted fixture under independently backed-up private storage. A compromised signer or tampered trusted fixture can invalidate the assurance provided by the signature.

## Meaning of a check

Authentication establishes which trusted issuer produced the report; it does **not** prove that the checker was correctly implemented, that the evidence archive exists, or that the claimed result applies beyond its stated test conditions. The operator must review verifier independence, toolchain, artifacts and coverage, especially for formal or scientific claims.

The `record_problem_check` tool remains available for provisional notes from models or reported external tests. These notes are never counted as authenticated corroboration. In the absence of a trusted check, the highest mastery target remains unmet. Revoke compromised verifier credentials at the operator boundary and audit receipts issued under them; existing records are not automatically invalidated by a key rotation.
