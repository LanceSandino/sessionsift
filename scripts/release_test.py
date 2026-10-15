#!/usr/bin/env python3
"""Tests for release.py and homebrew.py. Run: python3 -m unittest scripts/release_test.py"""
import hashlib
import io
import json
from pathlib import Path
import shutil
import sys
import tarfile
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parent))
import homebrew  # noqa: E402
import release  # noqa: E402

ROOT = Path(__file__).resolve().parents[1]
FILES = ("VERSION", "CHANGELOG.md", "package.json", "package-lock.json", "src/internal/release/catalog.go")


class ReleaseMetadataTest(unittest.TestCase):
    def copy(self):
        folder = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, folder)
        for name in FILES:
            (folder / name).parent.mkdir(parents=True, exist_ok=True)
            shutil.copy(ROOT / name, folder / name)
        return folder

    def test_repository_agrees(self):
        self.assertEqual(release.check(ROOT), (ROOT / "VERSION").read_text().strip())

    def test_drift_is_rejected(self):
        for name, old, new in (("VERSION", None, "9.9.9\n"),
                               ("package.json", '"version": "', '"version": "0.'),
                               ("src/internal/release/catalog.go", 'currentVersion = "', 'currentVersion = "0.')):
            folder = self.copy()
            path = folder / name
            path.write_text(new if old is None else path.read_text().replace(old, new, 1))
            with self.assertRaises(release.ReleaseError, msg=name):
                release.check(folder)

    def test_notes_are_the_current_section(self):
        body = release.notes(ROOT)
        self.assertTrue(body.strip())
        self.assertNotIn("## [", body)


class HomebrewFormulaTest(unittest.TestCase):
    def archive(self, dist, version, os_name, metadata=None):
        name = f"sessionsift_{version}_{os_name}_amd64"
        buffer = io.BytesIO()
        with tarfile.open(fileobj=buffer, mode="w:gz") as tar:
            members = {name + "/" + item: b"x" for item in homebrew.PAYLOAD}
            members[name + "/BUILD-METADATA.json"] = json.dumps(
                metadata or {"version": version, "target": f"{os_name}/amd64"}).encode()
            for member, data in members.items():
                info = tarfile.TarInfo(member)
                info.size = len(data)
                tar.addfile(info, io.BytesIO(data))
        (dist / (name + ".tar.gz")).write_bytes(buffer.getvalue())
        return name + ".tar.gz"

    def dist(self, version="1.2.3", bad=None):
        folder = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, folder)
        (folder / "VERSION").write_text(version + "\n")
        names = [self.archive(folder, version, "darwin", bad), self.archive(folder, version, "linux")]
        (folder / "SHA256SUMS").write_text("".join(
            f"{hashlib.sha256((folder / n).read_bytes()).hexdigest()}  {n}\n" for n in names))
        return folder

    def test_formula_pins_both_targets(self):
        folder = self.dist()
        version, sums = homebrew.release_inputs(folder / "VERSION", folder)
        text = homebrew.formula(version, sums)
        for name, digest in sums.items():
            self.assertIn(f"releases/download/v1.2.3/{name}", text)
            self.assertIn(f'sha256 "{digest}"', text)

    def test_mismatched_metadata_or_checksum_fails(self):
        folder = self.dist(bad={"version": "0.0.1", "target": "darwin/amd64"})
        with self.assertRaises(ValueError):
            homebrew.release_inputs(folder / "VERSION", folder)
        folder = self.dist()
        (folder / "sessionsift_1.2.3_linux_amd64.tar.gz").write_bytes(b"tampered")
        with self.assertRaises(ValueError):
            homebrew.release_inputs(folder / "VERSION", folder)


if __name__ == "__main__":
    unittest.main()
