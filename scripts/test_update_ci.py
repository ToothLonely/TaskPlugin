import importlib.util
import os
import pathlib
import subprocess
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('update_ci', pathlib.Path(__file__).with_name('update-ci.py'))
update_ci = importlib.util.module_from_spec(spec)
spec.loader.exec_module(update_ci)


class PermissionFixtureTests(unittest.TestCase):
    def test_windows_restricts_child_and_restores_acl(self):
        for child_fails in (False, True):
            with self.subTest(child_fails=child_fails), tempfile.TemporaryDirectory() as temporary:
                evidence = pathlib.Path(temporary)
                calls = []

                def run(args):
                    args = [str(arg) for arg in args]
                    calls.append(args)
                    if args[0] == 'go':
                        (evidence / 'permission.test.exe').write_bytes(b'test fixture')
                    elif args[:2] == ['whoami', '/user']:
                        return '"runner","S-1-5-21-123-456-789-500"\n'
                    elif args[0].endswith('permission.test.exe'):
                        self.assertEqual(args[1], '-test.run=^TestUpdateWindowsPermissionProcess$')
                        self.assertTrue(os.environ['UPDATE_DENIED_PATH'].endswith('git-task'))
                        if child_fails:
                            raise RuntimeError('injected child error')
                    return ''

                with patch.dict(os.environ), patch.object(update_ci, 'evidence', evidence), patch.object(update_ci.platform, 'system', return_value='Windows'), patch.object(update_ci, 'run', side_effect=run):
                    os.environ.pop('UPDATE_DENIED_PATH', None)
                    if child_fails:
                        with self.assertRaisesRegex(RuntimeError, 'injected child error'):
                            update_ci.permission_check()
                    else:
                        update_ci.permission_check()
                    self.assertNotIn('UPDATE_DENIED_PATH', os.environ)
                self.assertTrue(any(args[0] == 'icacls' and '/remove:d' in args for args in calls))

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
