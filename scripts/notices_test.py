#!/usr/bin/env python3
"""Tests for notices.py with synthetic packages. Run: python3 -m unittest scripts/notices_test.py"""
import io
import json
from pathlib import Path
import shutil
import sys
import tempfile
import unittest
from unittest import mock
import zipfile

sys.path.insert(0, str(Path(__file__).resolve().parent))
import notices  # noqa: E402


class NoticeTest(unittest.TestCase):
    def tree(self):
        root = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, root)
        (root / "src/web/components").mkdir(parents=True)
        (root / "src/web/main.tsx").write_text('import {createRoot} from "react-dom/client";\nimport {App} from "./components/App";\n')
        (root / "src/web/hosted.tsx").write_text('import "./styles.css";\nexport {x} from "node:path";\n')
        (root / "src/web/styles.css").write_text("body{}\n")
        (root / "src/web/components/App.tsx").write_text('import Markdown from "react-markdown";\n')
        packages = {
            "": {"name": "fixture", "version": "1.0.0"},
            "node_modules/react-dom": {"version": "1.0.0", "dependencies": {"scheduler": "^1"}},
            "node_modules/react-markdown": {"version": "2.0.0", "dependencies": {"scheduler": "^2"},
                                            "optionalDependencies": {"absent": "*"}},
            "node_modules/scheduler": {"version": "1.0.0"},
            "node_modules/react-markdown/node_modules/scheduler": {"version": "2.0.0"},
            "node_modules/vite": {"version": "9.0.0", "dev": True},
        }
        (root / "package-lock.json").write_text(json.dumps({"packages": packages}))
        (root / "package.json").write_text(json.dumps({"dependencies": {"react-dom": "1.0.0", "react-markdown": "2.0.0", "vite": "9.0.0"}}))
        for key, row in packages.items():
            if not key:
                continue
            folder = root / key
            (folder / "lib").mkdir(parents=True, exist_ok=True)
            name = key.split("node_modules/")[-1]
            (folder / "package.json").write_text(json.dumps({"name": name, "version": row["version"]}))
            (folder / "LICENSE").write_text(f"license of {name}@{row['version']}\n")
            (folder / "lib/notice.txt").write_text("nested notice\n")
            (folder / "lib/index.js").write_text("code\n")
        return root

    def test_frontend_closure_follows_imports_and_nested_versions(self):
        root = self.tree()
        self.assertEqual(notices.frontend_imports(root), ["react-dom", "react-markdown"])
        keys = [key for key, _ in notices.frontend_closure(root)]
        self.assertEqual(keys, ["node_modules/react-dom", "node_modules/react-markdown",
                                "node_modules/react-markdown/node_modules/scheduler", "node_modules/scheduler"])

    def test_npm_notices_select_license_files_in_walk_order(self):
        root = self.tree()
        row = {"version": "2.0.0"}
        parts = notices.npm_notices(root, "node_modules/react-markdown", row)
        self.assertEqual([title for title, _ in parts], ["NPM react-markdown@2.0.0 / LICENSE", "NPM react-markdown@2.0.0 / lib/notice.txt"])
        self.assertEqual(parts[0][1], b"license of react-markdown@2.0.0\n")

    def test_drift_and_undeclared_imports_are_rejected(self):
        root = self.tree()
        (root / "node_modules/react-dom/package.json").write_text(json.dumps({"name": "react-dom", "version": "9.9.9"}))
        with self.assertRaises(notices.NoticeError):
            notices.npm_notices(root, "node_modules/react-dom", {"version": "1.0.0"})
        (root / "src/web/main.tsx").write_text('import x from "left-pad";\n')
        with self.assertRaises(notices.NoticeError):
            notices.frontend_closure(root)
        (root / "src/web/main.tsx").write_text('import x from "./missing";\n')
        with self.assertRaises(notices.NoticeError):
            notices.frontend_imports(root)

    def test_go_notices_read_module_zip_in_archive_order(self):
        folder = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, folder)
        archive = folder / "v1.0.0.zip"
        with zipfile.ZipFile(archive, "w") as zipped:
            zipped.writestr("example.com/mod@v1.0.0/LICENSE", "MIT\n")
            zipped.writestr("example.com/mod@v1.0.0/code.go", "package mod\n")
            zipped.writestr("example.com/mod@v1.0.0/sub/COPYING", "GPL-compatible\n")
        info = json.dumps({"Path": "example.com/mod", "Version": "v1.0.0", "Zip": str(archive)})
        with mock.patch.object(notices, "go", return_value=info):
            parts = notices.go_notices(folder, "example.com/mod", "v1.0.0")
        self.assertEqual(parts, [("GO example.com/mod@v1.0.0 / LICENSE", b"MIT\n"),
                                 ("GO example.com/mod@v1.0.0 / sub/COPYING", b"GPL-compatible\n")])
        with zipfile.ZipFile(archive, "a") as zipped:
            zipped.writestr("other.com/escape@v1/LICENSE", "x")
        with mock.patch.object(notices, "go", return_value=info), self.assertRaises(notices.NoticeError):
            notices.go_notices(folder, "example.com/mod", "v1.0.0")

    def test_check_reports_stale_notice(self):
        with mock.patch.object(notices, "render", return_value=b"fresh\n"), \
                mock.patch.object(notices, "ROOT", Path(tempfile.mkdtemp())) as root, \
                mock.patch("sys.stderr", new_callable=io.StringIO) as error, mock.patch("sys.stdout", new_callable=io.StringIO):
            self.addCleanup(shutil.rmtree, root)
            (root / "NOTICE").write_bytes(b"stale\n")
            self.assertEqual(notices.main(["--check"]), 1)
            self.assertIn("out of date", error.getvalue())
            self.assertEqual(notices.main(["--write"]), 0)
            self.assertEqual(notices.main(["--check"]), 0)


if __name__ == "__main__":
    unittest.main()
