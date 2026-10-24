#!/usr/bin/env python3
"""Regenerate NOTICE from the locked Go modules and frontend packages.

NOTICE carries the upstream license and notice texts of every Go module linked
into the released binaries, a conservative frontend closure (the packages the
web entry points import, plus their locked dependencies) and the Go toolchain
license. Run it after any dependency change.

Usage:
  python3 scripts/notices.py --write   rewrite NOTICE
  python3 scripts/notices.py --check   fail when NOTICE is out of date

Needs the Go module cache and an installed node_modules (npm ci).
"""
import argparse
import json
import os
from pathlib import Path, PurePosixPath
import re
import stat
import subprocess
import sys
import zipfile

ROOT = Path(__file__).resolve().parents[1]
TARGETS = (("darwin", "amd64"), ("linux", "amd64"))
PREFIXES = ("LICENSE", "LICENCE", "NOTICE", "COPYING", "COPYRIGHT")
ENTRIES = ("src/web/main.tsx", "src/web/hosted.tsx")
IMPORT = re.compile(r'(?m)^\s*(?:import|export)\b[^;]*?\bfrom\s*["\']([^"\'\n]+)["\']')
HEADER = (b"Required Notice: Copyright 2026 Lance Sandino.\n\n"
          b"SessionSift is source-available under PolyForm Noncommercial 1.0.0; see LICENSE.\n"
          b"Third-party components retain their respective copyright notices and licenses below.\n"
          b"These notices do not replace or extend the SessionSift license.\n\n"
          b"This collection covers linked Go dependencies and a conservative frontend dependency\n"
          b"closure, including potentially unused or type-only dependencies. Inclusion is not a\n"
          b"claim that every listed frontend package appears in the compiled application.\n"
          b"The Go toolchain license is included. Container base licenses are provided by their\n"
          b"respective upstream distributions; no prebuilt container image is distributed here.\n\n")


class NoticeError(Exception):
    """A dependency whose notice text cannot be collected safely."""


def notice_name(name):
    return name.upper().startswith(PREFIXES)


def go(root, *args, env=None):
    result = subprocess.run(["go", *args], cwd=root, env={**os.environ, **(env or {})},
                            capture_output=True, text=True, timeout=300)
    if result.returncode:
        raise NoticeError(f"go {args[0]} failed: {result.stderr.strip()[:400]}")
    return result.stdout


def linked_modules(root):
    """Modules that provide a package linked into any released binary."""
    modules = set()
    template = "{{.ImportPath}}\t{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}}{{end}}{{end}}\t{{with .Error}}{{.Err}}{{end}}"
    for goos, goarch in TARGETS:
        output = go(root, "list", "-e", "-deps", "-f", template, "./src/cmd/sessionsift",
                    env={"GOOS": goos, "GOARCH": goarch, "CGO_ENABLED": "0", "GOFLAGS": "-mod=readonly"})
        for line in output.splitlines():
            package, module, error = line.split("\t")
            # The embedded web build may be absent; it never changes imports.
            if error and not (package.endswith("/src/webui") and "no matching files found" in error):
                raise NoticeError(f"cannot list {package}: {error[:300]}")
            if module:
                modules.add(tuple(module.split(" ")))
    return sorted(modules)


def go_notices(root, module, version):
    info = json.loads(go(root, "mod", "download", "-json", f"{module}@{version}"))
    if info.get("Path") != module or info.get("Version") != version or info.get("Error"):
        raise NoticeError(f"module download mismatch for {module}@{version}")
    prefix = f"{module}@{version}/"
    parts = []
    with zipfile.ZipFile(info["Zip"]) as archive:
        for member in archive.infolist():
            if not member.filename.startswith(prefix):
                raise NoticeError(f"unexpected path in {module}@{version}")
            name = member.filename[len(prefix):]
            path = PurePosixPath(name)
            if not name or path.is_absolute() or ".." in path.parts:
                raise NoticeError(f"unsafe path in {module}@{version}")
            if member.is_dir() or not notice_name(path.name):
                continue
            if stat.S_ISLNK(member.external_attr >> 16):
                raise NoticeError(f"symlinked notice in {module}@{version}")
            parts.append((f"GO {module}@{version} / {name}", archive.read(member)))
    if not parts:
        raise NoticeError(f"no license text in {module}@{version}")
    return parts


def frontend_imports(root):
    """Bare package names reachable from the web entry points."""
    web = (root / "src/web").resolve()
    queue = [root / entry for entry in ENTRIES]
    seen, names = set(), set()
    while queue:
        source = queue.pop()
        if source in seen:
            continue
        if not source.is_file() or not source.resolve().is_relative_to(web):
            raise NoticeError(f"frontend source outside src/web: {source}")
        seen.add(source)
        if source.suffix not in (".ts", ".tsx"):
            continue
        for specifier in IMPORT.findall(source.read_text(encoding="utf-8")):
            if specifier.startswith("."):
                base = source.parent / specifier.split("?", 1)[0]
                choices = (base, base.with_suffix(".tsx"), base.with_suffix(".ts"), base / "index.tsx", base / "index.ts")
                found = next((choice for choice in choices if choice.is_file()), None)
                if found is None:
                    raise NoticeError(f"unresolved import {specifier} in {source.name}")
                queue.append(found)
            elif not specifier.startswith(("/", "node:")):
                bits = specifier.split("/")
                names.add("/".join(bits[:2]) if specifier.startswith("@") else bits[0])
    return sorted(names)


def resolve(key, name, packages):
    parent = PurePosixPath(key)
    while parent.parts:
        candidate = str(parent / "node_modules" / name)
        if candidate in packages:
            return candidate
        parent = parent.parent
    candidate = "node_modules/" + name
    if candidate in packages:
        return candidate
    raise KeyError(name)


def frontend_closure(root):
    lock = json.loads((root / "package-lock.json").read_text(encoding="utf-8"))["packages"]
    declared = json.loads((root / "package.json").read_text(encoding="utf-8")).get("dependencies", {})
    direct = frontend_imports(root)
    if not direct or any(name not in declared for name in direct):
        raise NoticeError("every frontend import must be a declared runtime dependency")
    queue, selected = ["node_modules/" + name for name in direct], set()
    while queue:
        key = queue.pop()
        if key in selected:
            continue
        if key not in lock:
            raise NoticeError(f"{key} is missing from package-lock.json")
        selected.add(key)
        for name in lock[key].get("dependencies", {}):
            try:
                queue.append(resolve(key, name, lock))
            except KeyError:
                raise NoticeError(f"missing locked dependency {name} of {key}") from None
        for name in lock[key].get("optionalDependencies", {}):
            try:
                queue.append(resolve(key, name, lock))
            except KeyError:
                pass
    return [(key, lock[key]) for key in sorted(selected)]


def npm_notices(root, key, row):
    package = root / key
    metadata = json.loads((package / "package.json").read_text(encoding="utf-8"))
    name = row.get("name") or key.split("node_modules/")[-1]
    if metadata.get("name") != name or metadata.get("version") != row["version"]:
        raise NoticeError(f"installed {key} does not match package-lock.json; run npm ci")
    parts = []
    for directory, dirs, files in os.walk(package, followlinks=False):
        dirs[:] = sorted(entry for entry in dirs if entry not in ("node_modules", ".git"))
        for entry in sorted(files):
            source = Path(directory, entry)
            if not notice_name(entry):
                continue
            if source.is_symlink() or not source.is_file():
                raise NoticeError(f"notice in {key} is not a regular file")
            parts.append((f"NPM {name}@{row['version']} / {source.relative_to(package).as_posix()}", source.read_bytes()))
    if not parts:
        raise NoticeError(f"no license text in {name}@{row['version']}")
    return parts


def render(root=ROOT):
    parts = []
    for module, version in linked_modules(root):
        parts.extend(go_notices(root, module, version))
    for key, row in frontend_closure(root):
        parts.extend(npm_notices(root, key, row))
    body = b"".join(title.encode() + b"\n" + text + b"\n\n" for title, text in parts)
    version = go(root, "env", "GOVERSION").strip()
    goroot = Path(go(root, "env", "GOROOT").strip())
    # Official toolchains keep LICENSE in GOROOT; Homebrew keeps it one level up.
    toolchain = next((path for path in (goroot / "LICENSE", goroot.parent / "LICENSE") if path.is_file()), goroot / "LICENSE")
    body += f"GO TOOLCHAIN {version} LICENSE\n".encode() + toolchain.read_bytes() + b"\n"
    return HEADER + body[:-1]


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--write", action="store_true")
    mode.add_argument("--check", action="store_true")
    args = parser.parse_args(argv)
    try:
        expected = render()
    except (NoticeError, OSError, ValueError) as error:
        print(f"notice generation failed: {error}", file=sys.stderr)
        return 1
    target = ROOT / "NOTICE"
    if args.write:
        target.write_bytes(expected)
        print(f"NOTICE written ({len(expected)} bytes)")
        return 0
    if target.read_bytes() != expected:
        print("NOTICE is out of date; run python3 scripts/notices.py --write", file=sys.stderr)
        return 1
    print("NOTICE matches the locked dependencies")
    return 0


if __name__ == "__main__":
    sys.exit(main())
