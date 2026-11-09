#!/usr/bin/env python3
"""Validate the required repository REFERENCES.md file."""

from __future__ import annotations

import argparse
import json
import re
import sys
from dataclasses import asdict, dataclass
from pathlib import Path
from urllib.parse import urlsplit, urlunsplit

EMPTY_STATE = "No external references are currently required."
ENTRY = re.compile(r"^\s*[-*]\s+\[([^\]]+)\]\((https?://[^)]+)\)\s+[—-]\s+(.+)$")
LOCAL_PATH = re.compile(r"(?:sandbox:|file:/{1,3}|/mnt/data/|/home/[^/]+/|[A-Za-z]:\\\\Users\\\\)", re.I)
EPHEMERAL_AI = re.compile(
    r"(?:chatgpt\.com/(?:share|c)/|claude\.ai/chat|gemini\.google\.com/app|"
    r"copilot\.microsoft\.com/(?:chats?|share)|perplexity\.ai/search)",
    re.I,
)


@dataclass(frozen=True)
class Issue:
    code: str
    line: int
    message: str


def repo_root(path: Path) -> Path:
    path = path.resolve()
    if path.is_file():
        path = path.parent
    for candidate in (path, *path.parents):
        if (candidate / ".git").exists():
            return candidate
    return path


def normalize_url(url: str) -> str:
    parts = urlsplit(url.strip())
    path = parts.path.rstrip("/") or "/"
    return urlunsplit((parts.scheme.lower(), parts.netloc.lower(), path, parts.query, ""))


def validate(root: Path) -> list[Issue]:
    target = root / "REFERENCES.md"
    if not target.is_file():
        return [Issue("MISSING_REFERENCES", 0, "Repository root must contain REFERENCES.md.")]

    try:
        text = target.read_text(encoding="utf-8")
    except UnicodeDecodeError:
        return [Issue("ENCODING", 0, "REFERENCES.md must be UTF-8 text.")]

    issues: list[Issue] = []
    lines = text.splitlines()
    first = next(((i, line.strip()) for i, line in enumerate(lines, 1) if line.strip()), None)
    if first is None or first[1] != "# References":
        issues.append(Issue("HEADING", first[0] if first else 0, "First non-empty line must be '# References'."))

    has_empty = any(line.strip() == EMPTY_STATE for line in lines)
    entries: list[tuple[int, str]] = []
    seen: dict[str, int] = {}

    for number, line in enumerate(lines, 1):
        if LOCAL_PATH.search(line):
            issues.append(Issue("LOCAL_PATH", number, "Do not use local, sandbox, or file URLs as references."))
        if EPHEMERAL_AI.search(line):
            issues.append(Issue("EPHEMERAL_AI", number, "Do not cite assistant conversations or AI search/chat pages."))
        match = ENTRY.match(line)
        if not match:
            continue
        _title, url, detail = match.groups()
        entries.append((number, url))
        if "Used for:" not in detail:
            issues.append(Issue("MISSING_ROLE", number, "Each reference entry must contain a 'Used for:' statement."))
        key = normalize_url(url)
        if key in seen:
            issues.append(Issue("DUPLICATE", number, f"Duplicate source URL; first entry is on line {seen[key]}."))
        else:
            seen[key] = number

    if has_empty and entries:
        issues.append(Issue("EMPTY_WITH_ENTRIES", 0, "Remove the canonical empty-state sentence when references exist."))
    if not has_empty and not entries:
        issues.append(Issue("NO_ENTRIES", 0, "Add at least one canonical reference entry or the exact empty-state sentence."))

    return issues


def initialize(root: Path) -> None:
    target = root / "REFERENCES.md"
    if target.exists():
        return
    target.write_text(f"# References\n\n{EMPTY_STATE}\n", encoding="utf-8")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Validate the required repository REFERENCES.md page.")
    parser.add_argument("path", nargs="?", default=".", help="Repository root or any path inside the repository.")
    parser.add_argument("--init", action="store_true", help="Create the canonical empty REFERENCES.md when it is missing.")
    parser.add_argument("--json", action="store_true", dest="as_json")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    root = repo_root(Path(args.path))
    if args.init:
        initialize(root)
    issues = validate(root)
    if args.as_json:
        print(json.dumps([asdict(issue) for issue in issues], indent=2))
    elif issues:
        for issue in issues:
            location = f" line {issue.line}" if issue.line else ""
            print(f"ERROR {issue.code}{location}: {issue.message}")
        print(f"Repository references check: {len(issues)} issue(s)")
    else:
        print("Repository references check: PASS")
    return 1 if issues else 0


if __name__ == "__main__":
    raise SystemExit(main())
