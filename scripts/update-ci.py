import hashlib
import json
import os
import pathlib
import platform
import shutil
import subprocess
import sys
import tarfile
import zipfile
import tempfile

def msi_registration():
    script = "$ErrorActionPreference='Stop'; $i=New-Object -ComObject WindowsInstaller.Installer; $p=@($i.RelatedProducts('{C0C7D813-8DDD-4F2B-951A-E30DC27F643C}')); ConvertTo-Json -Compress -InputObject $p"
    return json.loads(run(['powershell.exe', '-NoProfile', '-NonInteractive', '-Command', script]))

def permission_check():
    executable = evidence / 'permission.test.exe'
    run(['go', 'test', '-c', '-o', executable, './internal/update'])
    with tempfile.TemporaryDirectory(prefix='git-task-permission-', dir='/tmp' if os.name != 'nt' else None) as temp:
        temp = pathlib.Path(temp)
        temp.chmod(0o755)
        child = temp / 'permission.test.exe'
        shutil.copyfile(executable, child)
        child.chmod(0o755)
        denied = temp / 'denied'
        denied.mkdir()
        target = str(denied / 'git-task')
        if platform.system() == 'Windows':
            sid = run(['whoami', '/user', '/fo', 'csv', '/nh']).strip().split(',')[-1].strip('"')
            run(['icacls', denied, '/deny', '*' + sid + ':(OI)(CI)(W)'])
            try:
                os.environ['UPDATE_DENIED_PATH'] = target
                run([child, '-test.run=^TestUpdatePermissionChild$', '-test.v'])
            finally:
                run(['icacls', denied, '/remove:d', '*' + sid])
                os.environ.pop('UPDATE_DENIED_PATH', None)
        else:
            denied.chmod(0o555)
            try:
                args = [child, '-test.run=^TestUpdatePermissionChild$', '-test.v']
                if os.geteuid() == 0:
                    args = (['runuser', '-u', 'nobody', '--'] if platform.system() == 'Linux' else ['sudo', '-u', 'nobody']) + ['env', 'UPDATE_DENIED_PATH=' + target] + args
                else:
                    os.environ['UPDATE_DENIED_PATH'] = target
                run(args)
            finally:
                denied.chmod(0o755)
                os.environ.pop('UPDATE_DENIED_PATH', None)
    executable.unlink()

root = pathlib.Path(__file__).resolve().parent.parent
evidence = root / '.tools/update-evidence'

def run(args):
    result = subprocess.run([str(x) for x in args], cwd=root, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    with (evidence / 'commands.log').open('a', encoding='utf-8') as log:
        log.write(json.dumps([str(x) for x in args]) + '\n' + result.stdout + '\nexit=' + str(result.returncode) + '\n')
    if result.returncode:
        raise RuntimeError('Command failed: ' + str(args))
    return result.stdout

def main():
    if os.environ.get('GITHUB_ACTIONS') != 'true':
        raise RuntimeError('Disposable GitHub runner required')
    evidence.mkdir(parents=True, exist_ok=True)
    channel = sys.argv[1]
    protected = root / '.tools/update-protected'
    protected.mkdir()
    for name in ('project/code.txt', 'project/.git-task/plan.json', 'project/.git/config', 'project/.git/hooks/post-commit', 'global.gitconfig'):
        path = protected / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text('preserve ' + name)
    (protected / 'global.gitconfig').write_text('[user]\n name = Update CI\n email = update@example.invalid\n')
    os.environ['GIT_CONFIG_GLOBAL'] = str(protected / 'global.gitconfig')
    os.environ['GIT_CONFIG_NOSYSTEM'] = '1'
    snapshot = lambda: {str(p.relative_to(protected)): hashlib.sha256(p.read_bytes()).hexdigest() for p in protected.rglob('*') if p.is_file()}
    before = snapshot()
    (evidence / 'protected-before.json').write_text(json.dumps(before))
    if channel == 'msi':
        registration = msi_registration()
        user_path_script = "ConvertTo-Json -Compress -InputObject ([Environment]::GetEnvironmentVariable('Path','User'))"
        user_path_before = run(['powershell.exe', '-NoProfile', '-Command', user_path_script]).strip()
        (evidence / 'msi-before.json').write_text(json.dumps(registration))
        if registration:
            raise RuntimeError('Runner already contains Git Task MSI')
    permission_check()
    old, new = root / '.tools/update-old', root / '.tools/update-new'
    system = {'Windows': 'windows', 'Linux': 'linux', 'Darwin': 'darwin'}[platform.system()]
    arch = 'arm64' if platform.machine().lower() in ('arm64', 'aarch64') else 'amd64'
    for bundle in (old, new):
        for line in (bundle / 'SHA256SUMS').read_text().splitlines():
            expected, name = line.split('  ', 1)
            if pathlib.Path(name).name != name or hashlib.sha256((bundle / name).read_bytes()).hexdigest() != expected:
                raise RuntimeError('Invalid bundle checksum: ' + name)
    (evidence / 'environment.json').write_text(json.dumps({'platform': platform.platform(), 'arch': arch, 'channel': channel, 'N': '90.0.0', 'next': '90.0.1'}))
    def package(version, directory):
        suffix = {'msi': 'windows.msi', 'deb': 'linux.deb', 'rpm': 'linux.rpm', 'pkg': 'darwin_arm64.pkg'}[channel]
        return directory / ('git-task_' + version + '_' + suffix)
    if channel == 'portable':
        target = root / '.tools/update-install'
        target.mkdir()
        extension = '.zip' if system == 'windows' else '.tar.gz'
        archive = old / ('git-task_90.0.0_' + system + '_' + arch + extension)
        if extension == '.zip':
            with zipfile.ZipFile(archive) as stream:
                stream.extractall(target)
        else:
            with tarfile.open(archive) as stream:
                stream.extractall(target, filter='data')
        binary = target / ('git-task.exe' if system == 'windows' else 'git-task')
        uninstall = [str(pathlib.Path(sys.executable).resolve()), str(pathlib.Path(__file__).resolve()), 'remove-portable', str(binary)]
    else:
        initial = package('90.0.0', old)
        if channel == 'msi':
            tool = pathlib.Path(os.environ['SystemRoot']) / 'System32/msiexec.exe'
            run([tool, '/i', initial, '/qn', '/norestart'])
            binary = pathlib.Path(os.environ['LOCALAPPDATA']) / 'Programs/GitTask/bin/git-task.exe'
            uninstall = [str(tool), '/x', str(package('90.0.1', new)), '/qn', '/norestart']
        elif channel == 'pkg':
            run(['/usr/sbin/installer', '-pkg', initial, '-target', '/'])
            binary = pathlib.Path('/usr/local/bin/git-task')
            uninstall = [str(pathlib.Path(sys.executable).resolve()), str(pathlib.Path(__file__).resolve()), 'remove-pkg', str(binary)]
        else:
            tool = shutil.which('dpkg' if channel == 'deb' else 'rpm')
            run([tool, '--install' if channel == 'deb' else '-i', initial])
            binary = pathlib.Path('/usr/bin/git-task')
            uninstall = [tool, '--remove' if channel == 'deb' else '--erase', 'git-task']
    os.environ['PATH'] = str(binary.parent) + os.pathsep + os.environ['PATH']
    os.environ.update(GIT_TASK_UPDATE_DISPOSABLE_VM='1', GIT_TASK_UPDATE_EXECUTABLE=str(binary), GIT_TASK_UPDATE_PREVIOUS='90.0.0', GIT_TASK_UPDATE_BUNDLE=str(new), GIT_TASK_UPDATE_UNINSTALL_PROGRAM=uninstall[0], GIT_TASK_UPDATE_UNINSTALL_ARGS=json.dumps(uninstall[1:]))
    run([binary, 'version'])
    output = run(['go', 'test', '-json', '-count=1', '-run', '^TestUpdateCommandInDisposableVM$', './internal/cli'])
    events = [json.loads(line) for line in output.splitlines() if line.startswith('{')]
    if any(e.get('Action') == 'skip' for e in events) or not any(e.get('Action') == 'pass' and e.get('Test') == 'TestUpdateCommandInDisposableVM' for e in events):
        raise RuntimeError('Required test did not PASS')
    faults = run(['go', 'test', '-json', '-count=1', '-run', 'TestLatestReleaseValidation|TestDownloadRejectsCorruption|TestVersionOrder|TestExtractBinary|TestSelectInstallationAsset|TestInvalidChecksums|TestRecovery|TestPortableTransaction|TestHTTPFailures|TestReleaseTrustBoundary|TestUpdateLock|TestUpdateProcessLock|TestUpdateCrashBoundaries|TestUpdateRunningExecutable|TestUpdateForeign|TestUpdateInstaller', './internal/update'])
    if any(json.loads(line).get('Action') == 'skip' for line in faults.splitlines() if line.startswith('{')):
        raise RuntimeError('Fault coverage skipped')
    run(['go', 'test', '-count=1', '-run', 'TestUpdateSourceFailures|TestUpdateArgumentsBeforeEffects|TestInformationCommands|TestInvalidArguments|TestCatalog|TestArchiveContentsAndModes|TestReleaseInputsExcludeLocalAgentFiles', './internal/update', './internal/cli', './internal/diagnostic', './scripts/release'])
    after = snapshot()
    (evidence / 'protected-after.json').write_text(json.dumps(after))
    if after != before:
        raise RuntimeError('Protected projects/plans/hooks/config changed')
    if channel == 'msi':
        registration = msi_registration()
        (evidence / 'msi-after.json').write_text(json.dumps(registration))
        if registration:
            raise RuntimeError('Windows Installer registration remains after uninstall')
        user_path_after = run(['powershell.exe', '-NoProfile', '-Command', user_path_script]).strip()
        (evidence / 'msi-path.json').write_text(json.dumps({'before': user_path_before, 'after': user_path_after}))
        if user_path_before != user_path_after:
            raise RuntimeError('MSI uninstall did not restore user PATH')
    if binary.exists():
        raise RuntimeError('Executable remains after uninstall')
    if channel in ('deb', 'rpm', 'pkg'):
        query = {'deb': ['dpkg-query', '-W', '-f=${Status}', 'git-task'], 'rpm': ['rpm', '-q', 'git-task'], 'pkg': ['/usr/sbin/pkgutil', '--pkg-info', 'io.github.toothlonely.git-task']}[channel]
        result = subprocess.run(query, capture_output=True, text=True)
        (evidence / 'uninstall-registration.json').write_text(json.dumps({'command': query, 'exit': result.returncode, 'stdout': result.stdout, 'stderr': result.stderr}))
        if result.returncode == 0 and (channel != 'deb' or result.stdout == 'install ok installed'):
            raise RuntimeError('Package remains registered after uninstall')

if __name__ == '__main__':
    if len(sys.argv) > 2 and sys.argv[1] in ('remove-portable', 'remove-pkg'):
        if os.environ.get('GITHUB_ACTIONS') != 'true':
            raise RuntimeError('Disposable runner required')
        binary = pathlib.Path(sys.argv[2]).resolve()
        if sys.argv[1] == 'remove-pkg':
            if binary != pathlib.Path('/usr/local/bin/git-task'):
                raise RuntimeError('Unexpected package target')
            binary.unlink()
            pathlib.Path('/etc/paths.d/git-task').unlink()
            for name in ('README.md', 'GO-LICENSE', 'GO-PATENTS'):
                pathlib.Path('/usr/local/share/doc/git-task', name).unlink()
            pathlib.Path('/usr/local/share/doc/git-task').rmdir()
            subprocess.run(['/usr/sbin/pkgutil', '--forget', 'io.github.toothlonely.git-task'], check=True)
        else:
            if binary.parent != root / '.tools/update-install':
                raise RuntimeError('Unexpected portable target')
            binary.unlink()
            (binary.parent / 'git-task-portable.json').unlink()
    else:
        main()
