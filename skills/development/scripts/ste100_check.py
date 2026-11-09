#!/usr/bin/env python3
"""Mechanical ASD-STE100 precheck for technical prose.

This checker does not validate the controlled dictionary or prove compliance.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from dataclasses import asdict, dataclass
from pathlib import Path

FENCED_CODE = re.compile(r"```[\s\S]*?```|~~~[\s\S]*?~~~")
INLINE_CODE = re.compile(r"`[^`\n]+`")
URL = re.compile(r"https?://\S+")
PARENTHETICAL = re.compile(r"\([^()]*\)")
QUOTED = re.compile(r'"[^"\n]+"')
NUMBER_UNIT = re.compile(r"\b\d+(?:\.\d+)?\s*(?:%|[A-Za-z°]+)\b")
SENTENCE_SPLIT = re.compile(r"(?<=[.!?])\s+(?=[A-Z0-9\"'(\[])" )
WORD = re.compile(r"\b(?:[A-Za-z]+(?:-[A-Za-z]+)*|[A-Za-z]*\d+[A-Za-z0-9._/-]*)\b")
CONTRACTION = re.compile(r"\b(?:can't|won't|isn't|aren't|wasn't|weren't|don't|doesn't|didn't|hasn't|haven't|hadn't|shouldn't|wouldn't|couldn't|mustn't|it's|that's|there's|we're|you're|they're|I've|we've|you've|they've|I'll|we'll|you'll|they'll)\b", re.I)
SEMICOLON = re.compile(r";")

COMPLEX_VERB = re.compile(
    r"\b(?:has|have|had)\s+\w+(?:ed|en)\b|"
    r"\b(?:is|are|was|were|be|been|being)\s+\w+ing\b",
    re.I,
)
PASSIVE = re.compile(
    r"\b(?:is|are|was|were|be|been|being|will be|can be|must be|may be|should be)\s+"
    r"\w+(?:ed|en)\b",
    re.I,
)
PHRASAL_VERB = re.compile(
    r"\b(?:spin up|reach out|kick off|take down|set up|shut down|log in|log out|"
    r"look up|check out|carry out|back up|roll back|clean up|tear down)\b",
    re.I,
)
BRITISH_SPELLING = re.compile(
    r"\b(?:colour|colours|favour|favours|centre|centres|behaviour|behaviours|"
    r"organisation|organisations|optimise|optimised|optimising|analyse|analysed|analysing)\b",
    re.I,
)


@dataclass(frozen=True)
class Issue:
    code: str
    severity: str
    line: int
    message: str
    text: str


def scrub_literals(text: str) -> str:
    text = FENCED_CODE.sub(" ", text)
    text = INLINE_CODE.sub(" LITERAL ", text)
    text = URL.sub(" URL ", text)
    return text

def ste_word_count(sentence: str) -> int:
    """Approximate Issue 9 word-count rules without the controlled dictionary."""
    working = PARENTHETICAL.sub(" GROUP ", sentence)
    working = QUOTED.sub(" QUOTE ", working)
    working = NUMBER_UNIT.sub(" VALUE ", working)
    return len(WORD.findall(working))


def sentence_records(text: str) -> list[tuple[int, str]]:
    records: list[tuple[int, str]] = []
    for line_number, raw_line in enumerate(text.splitlines(), 1):
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        line = re.sub(r"^[-*+]\s+|^\d+[.)]\s+", "", line)
        for sentence in SENTENCE_SPLIT.split(line):
            sentence = sentence.strip()
            if sentence:
                records.append((line_number, sentence))
    return records


def paragraph_records(text: str) -> list[tuple[int, str]]:
    records: list[tuple[int, str]] = []
    line_number = 1
    for block in re.split(r"\n\s*\n", text):
        lines = [line.strip() for line in block.splitlines() if line.strip()]
        is_list = any(re.match(r"^(?:[-*+]\s+|\d+[.)]\s+)", line) for line in lines)
        clean = " ".join(lines)
        if clean and not clean.startswith("#") and not is_list:
            records.append((line_number, clean))
        line_number += block.count("\n") + 2
    return records


def add_match_issue(issues: list[Issue], pattern: re.Pattern[str], code: str, severity: str, line: int, text: str, message: str) -> None:
    if pattern.search(text):
        issues.append(Issue(code, severity, line, message, text))

def check_text(text: str, mode: str) -> list[Issue]:
    clean = scrub_literals(text)
    issues: list[Issue] = []
    limit = 20 if mode in {"procedure", "safety"} else 25

    for line, sentence in sentence_records(clean):
        count = ste_word_count(sentence)
        if count > limit:
            issues.append(Issue(
                "SENTENCE_LENGTH",
                "error",
                line,
                f"Sentence has {count} words; {mode} limit is {limit}.",
                sentence,
            ))
        add_match_issue(issues, SEMICOLON, "SEMICOLON", "error", line, sentence, "Semicolons are not permitted in STE.")
        add_match_issue(issues, CONTRACTION, "CONTRACTION", "error", line, sentence, "Do not use a contraction to shorten technical text.")
        add_match_issue(issues, COMPLEX_VERB, "COMPLEX_VERB", "warning", line, sentence, "Review this complex verb construction against Issue 9 verb-form rules.")
        add_match_issue(issues, PASSIVE, "PASSIVE_VOICE", "warning", line, sentence, "Review possible passive voice. Descriptions permit it only when the agent is unknown.")
        add_match_issue(issues, PHRASAL_VERB, "PHRASAL_VERB", "warning", line, sentence, "Replace a phrasal verb with an approved single verb when possible.")
        add_match_issue(issues, BRITISH_SPELLING, "SPELLING", "warning", line, sentence, "Issue 9 uses American English spelling unless another directive applies.")

    if mode == "description":
        for line, paragraph in paragraph_records(clean):
            sentence_count = len(sentence_records(paragraph))
            if sentence_count > 6:
                issues.append(Issue(
                    "PARAGRAPH_LENGTH",
                    "error",
                    line,
                    f"Descriptive paragraph has {sentence_count} sentences; maximum is 6.",
                    paragraph,
                ))
    return issues

def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Mechanical ASD-STE100 Issue 9 precheck.")
    parser.add_argument("path", nargs="?", help="UTF-8 text or Markdown file. Reads stdin when omitted.")
    parser.add_argument("--mode", choices=("procedure", "description", "safety"), default="description")
    parser.add_argument("--json", action="store_true", dest="as_json")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    if args.path:
        text = Path(args.path).read_text(encoding="utf-8")
    else:
        text = sys.stdin.read()

    issues = check_text(text, args.mode)
    if args.as_json:
        print(json.dumps([asdict(issue) for issue in issues], indent=2))
    elif not issues:
        print("ASD-STE100 mechanical precheck: PASS")
    else:
        for issue in issues:
            print(f"{issue.severity.upper()} {issue.code} line {issue.line}: {issue.message}")
            print(f"  {issue.text}")
        print(f"ASD-STE100 mechanical precheck: {len(issues)} issue(s)")

    return 1 if issues else 0


if __name__ == "__main__":
    raise SystemExit(main())
