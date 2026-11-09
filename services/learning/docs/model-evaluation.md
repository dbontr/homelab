# Blinded tutoring-model evaluation

This benchmark measures observable tutoring responses, not changes in a human learner's ability. It is an offline research tool and does not use or modify the learner's Noesis database. The optional model runner uses an explicitly supplied OpenAI-compatible chat-completions endpoint. It does not create an MCP-host session, call Noesis tools or claim that a prompt-only condition represents the complete ChatGPT integration.

## Experimental design

The learner script (`eval/fixtures/learner-cases.json`) and the reviewer rubric (`eval/fixtures/review-rubrics.json`) are separate. The runtime sends only the learner turns and the chosen system teaching policy. It never loads the rubric when calling a model. The rubric contains the misconception to detect, required reasoning, prohibited false or unsafe statements, and a transfer target. Keep genuine holdout material outside model context, published source code and training data. The checked-in examples are **illustrative cases**, not undisclosed holdouts.

The supplied conditions are a generic tutoring policy and a foundation-first tutoring policy. Both can be tested with the same model, inference configuration, and learner turns. Pair by case and change only the policy for the simplest comparison. Include the exact model identifier, revision and invocation mode in the private run artifact. If comparing distinct model families, interpret the result as a combined model/policy difference rather than an isolated prompt effect.

The runner sends two scripted learner messages per case and records the actual assistant text returned by each model call. It uses each backend's default sampling configuration: reproducibility therefore requires retaining backend settings externally, and two runs may produce different responses even for the same inputs. These scripts are predetermined: they do not adapt to the tutor's questions. A response can demonstrate a useful *tutoring move*, but it does not establish that a real learner understood or retained anything.

## Reproducible execution

Build with `go build -o teaching-eval ./cmd/teaching-eval`, or use `go run ./cmd/teaching-eval`. Validate the suite and precommitted review criteria:

```sh
./teaching-eval validate -suite eval/fixtures/learner-cases.json -rubrics eval/fixtures/review-rubrics.json
```

Connect to a model endpoint you control. HTTP is allowed only on loopback; non-loopback endpoints require HTTPS. Supply the model token through a local environment variable, never through command arguments or JSON fixtures. Replace the example endpoint and model with an actual authorized endpoint and model identifier:

```sh
./teaching-eval run -suite eval/fixtures/learner-cases.json \
  -policy eval/policies/baseline.txt -condition baseline \
  -endpoint http://127.0.0.1:8000/v1/chat/completions -model local-model \
  -out baseline-run.json

./teaching-eval run -suite eval/fixtures/learner-cases.json \
  -policy eval/policies/foundation.txt -condition foundation \
  -endpoint http://127.0.0.1:8000/v1/chat/completions -model local-model \
  -out foundation-run.json
```

Use different output filenames for each run. The tool writes new owner-only files (mode `0600`), never overwrites existing results, and records request failures explicitly as missing outcomes. The runner does not send evaluator rubrics, API keys, or Noesis private learner data to the model. It does send the full scripted learner turns to the configured model provider; choose that provider according to your data policy.

## Blinded independent review

Create a reviewer package and a private unblinding map:

```sh
./teaching-eval blind \
  -suite eval/fixtures/learner-cases.json \
  -rubrics eval/fixtures/review-rubrics.json \
  -runs baseline-run.json,foundation-run.json \
  -out blind-bundle.json -key private-map.json -template blank-reviews.json
```

Provide **only** `blind-bundle.json` to two independent subject-qualified reviewers. Keep `private-map.json`, policy files, model identifiers and source run files away from them until review is locked. The blinded bundle contains a randomized ID, the learner's exchanges, the tutor's exact responses and the applicable reference rubric; it contains no condition label or system prompt. The optional template intentionally contains invalid scores (`-1`) until reviewers fill it. Reviewer identities are an administrative assertion and are not authenticated by the software.

Each dimension is rated 0, 1, or 2:

| Dimension | 0 | 1 | 2 |
| --- | --- | --- | --- |
| `diagnosis` | Misses or reinforces the misconception | Notices the error without locating its premise | Identifies the specific mistaken inference and correct alternative |
| `accuracy` | Contains a material false, hazardous, or unsupported claim | Mostly sound but materially incomplete | Correct, appropriately qualified, and safe within scope |
| `repair` | Substitutes assertion or an answer for explanation | Provides partial justification | Reconstructs a defensible mechanism and asks the learner to justify a step |
| `transfer` | No useful new-context question | Repeats a template or supplies the solution | Invites a genuinely different application without giving away its result |
| `restraint` | Awards mastery prematurely, fabricates actions or encourages an unsafe shortcut | Avoids the worst error but overstates certainty or scope | Distinguishes answer from understanding and respects uncertainty and evidence limits |

For every dimension, provide a substantive `explanation`. For a score of 1 or 2, supply a short verbatim `quote` present in the tutor's response; a zero can use an empty quote with a specific explanation. Each sample requires two different reviewers. A disagreement remains unresolved until a third independent adjudicator rates the same dimensions; the tool never silently averages conflicting judgments.

Generate the private report after independent review:

```sh
./teaching-eval report \
  -bundle blind-bundle.json -key private-map.json \
  -reviews completed-reviews.json -out assessment-report.json
```

The report distinguishes model failures, missing ratings, unresolved judgments, and fully reviewed cases. For two conditions it computes paired differences only for cases scored in both conditions. A high score means the tutor displayed the specified behaviors; it is **not** a validated educational effect. Record actual reviewer independence and any adjudication outside the tool.

## Reliability and release standards

The suite, policy text, exact model ID, runtime configuration, and raw assistant responses should be retained in an access-controlled experiment archive. The unblinding map is sensitive and must not be included in the blinded reviewer packet. The run does not prove that a grader's opinion is correct, that a language model's explanation caused learning, or that a privately administered model equals ChatGPT's native MCP-host integration.

Do not use an automatic model grader as a replacement for independent assessment. Do not publish private learner transcripts or credentials as fixtures. Real educational effectiveness still requires an observed learner baseline, comparable instruction condition, unfamiliar immediate transfer, delayed unassisted retesting, and blinded assessment of the learner's own work. See [educational evaluation](education-evaluation.md).
