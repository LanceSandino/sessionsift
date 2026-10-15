#!/usr/bin/env python3
"""Keep one release version across VERSION, package files, the in-app catalog and CHANGELOG.

Usage:
  python3 scripts/release.py version [--expect X.Y.Z]   print the checked version
  python3 scripts/release.py check                       fail if any source disagrees
  python3 scripts/release.py notes [--output FILE]       print this version's CHANGELOG section
"""
import argparse
import json
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[1]
SEMVER = re.compile(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"
                    r"(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?",
                    re.ASCII)
HEADING = re.compile(r"^## \[([^\]\n]+)\] — (\d{4}-\d{2}-\d{2})$", re.M)


class ReleaseError(Exception):
    """A release metadata mismatch, described without file contents."""


def read(root, name):
    try:
        return (root / name).read_text(encoding="utf-8")
    except (OSError, UnicodeError):
        raise ReleaseError(f"cannot read {name}") from None


def read_version(root):
    text = read(root, "VERSION")
    value = text.removesuffix("\n")
    match = SEMVER.fullmatch(value)
    if text != value + "\n" or not match:
        raise ReleaseError("VERSION must hold one SemVer version and a final newline")
    if match.group(4) and any(p.isdigit() and len(p) > 1 and p.startswith("0") for p in match.group(4).split(".")):
        raise ReleaseError("numeric SemVer prerelease identifiers cannot have leading zeros")
    return value


def changelog_head(root):
    headings = HEADING.findall(read(root, "CHANGELOG.md"))
    if not headings:
        raise ReleaseError("CHANGELOG.md needs '## [X.Y.Z] — YYYY-MM-DD' release headings")
    return headings


def catalog(root):
    source = read(root, "src/internal/release/catalog.go")
    current = re.search(r'^const currentVersion = "([^"]+)"$', source, re.M)
    first = re.search(r'\{Version: "([^"]+)", Date: "([^"]+)"', source)
    if not current or not first:
        raise ReleaseError("cannot find the current version in the in-app release catalog")
    return current.group(1), first.group(1), first.group(2)


def check(root=ROOT):
    version = read_version(root)
    problems = []
    headings = changelog_head(root)
    if headings[0][0] != version:
        problems.append("CHANGELOG.md must start with the VERSION release")
    if [v for v, _ in headings].count(version) != 1:
        problems.append("CHANGELOG.md must list the VERSION release exactly once")
    try:
        package = json.loads(read(root, "package.json"))
        lock = json.loads(read(root, "package-lock.json"))
    except json.JSONDecodeError:
        raise ReleaseError("package metadata is not valid JSON") from None
    if package.get("version") != version:
        problems.append("package.json version differs from VERSION")
    if lock.get("version") != version or lock.get("packages", {}).get("", {}).get("version") != version:
        problems.append("package-lock.json version differs from VERSION")
    current, newest, date = catalog(root)
    if current != version or newest != version:
        problems.append("in-app release catalog version differs from VERSION")
    if headings[0][0] == version and date != headings[0][1]:
        problems.append("in-app release date differs from CHANGELOG.md")
    if problems:
        raise ReleaseError("; ".join(problems))
    return version


def notes(root=ROOT):
    version = check(root)
    text = read(root, "CHANGELOG.md")
    starts = [m for m in HEADING.finditer(text)]
    first = starts[0]
    end = starts[1].start() if len(starts) > 1 else len(text)
    body = text[first.end():end].strip()
    if version != first.group(1) or not body:
        raise ReleaseError("CHANGELOG.md has no notes for VERSION")
    return body + "\n"


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    commands = parser.add_subparsers(dest="command", required=True)
    version_parser = commands.add_parser("version")
    version_parser.add_argument("--expect")
    commands.add_parser("check")
    notes_parser = commands.add_parser("notes")
    notes_parser.add_argument("--output", type=Path)
    args = parser.parse_args(argv)
    try:
        if args.command == "version":
            value = check()
            if args.expect is not None and args.expect != value:
                raise ReleaseError("expected version does not match VERSION")
            print(value)
        elif args.command == "check":
            print(f"Release metadata agrees on {check()}")
        else:
            body = notes()
            if args.output:
                args.output.write_text(body, encoding="utf-8")
            else:
                sys.stdout.write(body)
    except ReleaseError as error:
        print(f"release check failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
