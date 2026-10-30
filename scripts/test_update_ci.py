import importlib.util
import pathlib
import subprocess
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('update_ci', pathlib.Path(__file__).with_name('update-ci.py'))
update_ci = importlib.util.module_from_spec(spec)
spec.loader.exec_module(update_ci)


class PermissionFixtureTests(unittest.TestCase):
    def test_rejects_writable_directory(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = pathlib.Path(temporary)
            with self.assertRaisesRegex(RuntimeError, 'still allows subdirectory creation'):
                update_ci.verify_directory_creation_denied(directory)
            self.assertFalse((directory / 'permission-probe').exists())

    def test_accepts_only_permission_failure(self):
        with patch.object(pathlib.Path, 'mkdir', side_effect=PermissionError):
            update_ci.verify_directory_creation_denied(pathlib.Path('denied'))
        with patch.object(pathlib.Path, 'mkdir', side_effect=FileNotFoundError):
            with self.assertRaises(FileNotFoundError):
                update_ci.verify_directory_creation_denied(pathlib.Path('missing'))

    def test_child_failure_is_visible_in_error_and_log(self):
        output = 'write succeeded in denied directory\n'
        with tempfile.TemporaryDirectory() as temporary:
            with patch.object(update_ci, 'evidence', pathlib.Path(temporary)):
                with patch.object(update_ci.subprocess, 'run', return_value=subprocess.CompletedProcess(['child'], 1, output)):
                    with self.assertRaises(RuntimeError) as raised:
                        update_ci.run(['child'])
                self.assertIn('exit 1', str(raised.exception))
                self.assertIn(output, str(raised.exception))
                self.assertIn(output, (pathlib.Path(temporary) / 'commands.log').read_text())


if __name__ == '__main__':
    unittest.main()
