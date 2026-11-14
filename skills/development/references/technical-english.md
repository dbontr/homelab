# ASD-STE100 for Software Technical English

Use ASD-STE100 Simplified Technical English, Issue 9 (January 2025), as the normative language standard for software text that a person, agent, or tool must interpret correctly.

Typical surfaces include prompts, tool descriptions, API descriptions, CLI help, errors, warnings, runbooks, procedures, status text, operational comments, and technical documentation.

Do not apply STE mechanically to source-code syntax, identifiers, protocol literals, file paths, CLI flags, URLs, schema fields, or quoted third-party text. Treat those items as protected literals unless the owning specification permits a change.

## Compliance status

Use one of these statuses when compliance matters:

- **ASD-STE100 targeted**: The writer applied the applicable Issue 9 rules but did not complete the dictionary and technical-term checks.
- **ASD-STE100 verified**: The text passed the applicable Issue 9 writing rules, dictionary checks, and the approved project terminology check with no unresolved deviations.
- **ASD-STE100 not verified**: The authoritative references or required terminology evidence were unavailable, or the review is incomplete.

Never describe a tool, Skill, model, or checker as ASD-certified, ASD-approved, or ASD-authorized. ASD and STEMG do not certify software tools.

A model judgment alone is not sufficient evidence for verified compliance. Use the official current ASD-STE100 document as the primary reference.

## Verification prerequisites

Before you use the `verified` status, have access to:

1. The authoritative ASD-STE100 Issue 9 writing rules.
2. The authoritative Issue 9 controlled dictionary.
3. The applicable company, project, industry, or subject-field glossary for technical nouns and technical verbs.
4. The final text after all edits, not an earlier draft.

## Software terminology policy

Use ASD-STE100 technical-noun and technical-verb allowances for software domain terms that the base dictionary cannot represent accurately.

Approve terminology from an enduring source such as an external specification, public API contract, repository glossary, architecture contract, or organization term base. Use one approved term for one concept.

Do not invent a simpler synonym when it changes the technical meaning. Do not rotate between names such as `job`, `task`, `worker`, and `process` unless they represent different concepts.

Treat exact code and protocol tokens as protected literals. Explain them with STE prose when necessary, but do not rewrite the tokens themselves.

## Writing pass

Classify each passage before you write it:

- **Procedure or instruction**: use imperative instructions, active voice, and a maximum of 20 words per sentence.
- **Description**: use active voice unless the agent is genuinely unknown. Use a maximum of 25 words per sentence.
- **Safety instruction**: apply the procedural sentence limit and the applicable Issue 9 safety rules.
- **Note**: give information only. Do not hide an instruction, requirement, or limit in a note.

Across all types:

- Use approved words only in the approved meaning and part of speech when you perform a verified lexical check.
- Use only the verb forms and tenses permitted by Issue 9.
- Avoid complex auxiliary constructions and phrasal verbs.
- Use an approved verb for an action instead of a nominalized action when possible.
- Keep multi-word nouns to three words unless an approved technical noun requires a longer full form.
- Do not omit necessary sentence parts or use contractions to shorten text.
- Use one stable technical noun for one item.
- Use American English spelling unless an applicable directive requires another spelling.
- Do not use semicolons.
- Keep each descriptive paragraph to one topic and no more than six sentences.

## Verification workflow

Use this sequence for text that must be ASD-STE100 verified:

1. Identify the passage type and applicable Issue 9 rules.
2. Mark protected literals and approved technical nouns and technical verbs.
3. Rewrite the prose to the applicable structural rules.
4. Run `go run scripts/ste100_check.go` as a mechanical precheck when Go is available.
5. Review every applicable writing rule against the authoritative Issue 9 standard.
6. Check each non-technical word against the authoritative Issue 9 dictionary for approval, meaning, part of speech, and permitted form.
7. Check every technical noun and technical verb against the approved project or subject-field terminology source.
8. Resolve every violation or document an externally required literal that cannot change.
9. Re-run the checks against the final text.
10. Report `ASD-STE100 verified` only when no unresolved compliance issue remains.

The bundled checker does not prove compliance. It detects mechanical defects that are practical to test without redistributing ASD's dictionary.

## Preserve meaning

Compliance work must not change technical truth.

Preserve conditions, uncertainty, exceptions, safety limits, identifiers, numbers, units, version constraints, and causal scope. Do not turn `may fail` into `fails`. Do not invent a cause to make a sentence easier to write.

If an externally defined phrase must stay unchanged and conflicts with STE, keep the literal when the owning contract requires it. Do not report the surrounding text as fully verified unless the standard permits that use.

## Source boundary

ASD owns ASD-STE100. Do not copy its controlled dictionary or substantial portions of the standard into this Skill or another repository.

Use the official standard as a reference during verification. Keep only original guidance, project terminology, and checker logic in the repository.

Official references:

- https://www.asd-ste100.org/
- https://www.asd-ste100.org/STE_downloads.html
- https://www.asd-ste100.org/STEsoftware.html
