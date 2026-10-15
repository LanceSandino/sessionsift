#!/usr/bin/env python3
"""@module LocalDockerSources emits explicit read-only transcript-leaf Compose mounts.

Dependencies: Python3 standard library. Never creates a source path or edits clients.
Output: a new owner-only JSON Compose override; paths stay on the user's machine.
"""
import argparse
import json
import os
from pathlib import Path
import stat
import sys

CREDENTIAL_NAMES = {'auth.json', 'credentials.json', '.credentials.json', 'id_rsa',
                    'id_ed25519', '.env', 'config.toml', 'settings.json'}


def no_symlinks(path):
    for part in (path, *path.parents):
        if part.is_symlink():
            raise ValueError('symlink source/output paths are not allowed')


def selected_mount(value, leaf_name, target):
    path = Path(os.path.abspath(os.path.expanduser(value)))
    if path.name != leaf_name:
        raise ValueError('select only the requested transcript leaf: ' + leaf_name)
    no_symlinks(path)
    if not path.exists():
        print('Optional ' + leaf_name + ' directory absent; omitted without creating it.', file=sys.stderr)
        return None
    if not path.is_dir():
        raise ValueError('source must be an existing directory')
    # Validate without opening any transcript or credentials. Refuse unsafe trees.
    for directory, dirs, files in os.walk(path, followlinks=False, onerror=lambda error: (_ for _ in ()).throw(error)):
        for name in dirs + files:
            entry = Path(directory) / name
            if entry.is_symlink():
                raise ValueError('symlink entries are not allowed inside transcript sources')
            if name.lower() in CREDENTIAL_NAMES or name.lower().startswith('.env.'):
                raise ValueError('credential/configuration files are not allowed in transcript mounts')
            mode = entry.stat().st_mode
            if not stat.S_ISDIR(mode) and not stat.S_ISREG(mode):
                raise ValueError('special files are not allowed in transcript mounts')
    return {'type': 'bind', 'source': str(path), 'target': target, 'read_only': True,
            'bind': {'create_host_path': False}}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--codex-sessions')
    parser.add_argument('--codex-archived-sessions')
    parser.add_argument('--claude-projects')
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    mounts = []
    for value, leaf, target in ((args.codex_sessions, 'sessions', '/sources/codex/sessions'),
                               (args.codex_archived_sessions, 'archived_sessions', '/sources/codex/archived_sessions'),
                               (args.claude_projects, 'projects', '/sources/claude/projects')):
        if value:
            mount = selected_mount(value, leaf, target)
            if mount:
                mounts.append(mount)
    output = Path(os.path.abspath(args.output)); no_symlinks(output)
    # Exclusive creation preserves any earlier configuration; no automatic overwrite.
    descriptor = os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, 'w') as stream:
        json.dump({'services': {'agent': {'volumes': mounts}}}, stream, indent=2)
        stream.write('\n')
    print('Created private Compose override; review it before using -f with Compose.')


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError) as error:
        print('Source selection refused: ' + str(error), file=sys.stderr)
        sys.exit(1)
