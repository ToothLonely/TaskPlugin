# Установка без Go

Нужны Git 2.51.0 или новее и архив своей ОС/amd64. Go не нужен.
Кандидат 0.1.0-rc.1 локальный; адрес скачивания ещё не назначен.
Получите архив, manifest.json и SHA256SUMS из одного каталога кандидата.
Инструкции ниже работают в выделенном каталоге внутри проекта. Для другого
места выберите собственный отдельный каталог: системные каталоги и глобальный
PATH не нужны. Не копируйте бинарник поверх неизвестного файла.

## Windows (PowerShell)

Из корня исходного проекта, где подготовлен кандидат:

```powershell
$candidate = Join-Path $PWD '.tools/releases/0.1.0-rc.1-r02'
$archive = 'git-task_0.1.0-rc.1_windows_amd64.zip'
$manifest = Get-Content -LiteralPath (Join-Path $candidate 'manifest.json') -Raw | ConvertFrom-Json
$item = $manifest.artifacts | Where-Object platform -eq 'windows/amd64'
if ((Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $candidate $archive)).Hash.ToLowerInvariant() -ne $item.sha256) { throw 'Checksum mismatch' }
$install = Join-Path $PWD '.tools/installed/git-task-0.1.0-rc.1-r02'
if (Test-Path -LiteralPath $install) { throw 'Choose a new installation directory' }
Expand-Archive -LiteralPath (Join-Path $candidate $archive) -DestinationPath $install
if ((Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $install 'git-task.exe')).Hash.ToLowerInvariant() -ne $item.binary_sha256) { throw 'Binary checksum mismatch' }
$env:PATH = $install + [IO.Path]::PathSeparator + $env:PATH
git task version
```

Сверьте SHA256 manifest.json и архива со строками SHA256SUMS (при передаче
ожидаемую сумму лучше получить отдельно). Hash из manifest не доказывает
подлинность источника: архивы не подписаны. `version` должен показать
`git-task 0.1.0-rc.1`. Вне исходников замените `$candidate` каталогом полученных
файлов, а `$install` — новым абсолютным путём выделенной установки.
PATH меняется только у текущего процесса; закрытие терминала убирает изменение.

## Linux/macOS (sh)

Из корня проекта, либо замените candidate/install абсолютными каталогами:

```sh
candidate="$PWD/.tools/releases/0.1.0-rc.1-r02"
install="$PWD/.tools/installed/git-task-0.1.0-rc.1-r02"
cd "$candidate" || exit 1
# Linux:
sha256sum -c SHA256SUMS || exit 1
# На macOS вместо предыдущей строки: shasum -a 256 -c SHA256SUMS || exit 1
test ! -e "$install" || exit 1
mkdir -p "$install" || exit 1
# На macOS замените linux на darwin в имени архива:
tar -xzf git-task_0.1.0-rc.1_linux_amd64.tar.gz -C "$install" || exit 1
export PATH="$install:$PATH"
git task version
```

Выполняйте только строку checksum своей ОС. Unix-архив сохраняет executable
bit бинарника. Сверьте его hash (`sha256sum`/`shasum -a 256`) с binary_sha256
своей платформы в manifest. Shell и утилиты архивирования нужны для установки;
приложение использует отдельный Git. На macOS ограничение запуска полученного
извне файла зависит от системы: подписания/notarization у кандидата нет.
Кандидат дарwin/amd64 не является обещанием поддержки ARM64/Rosetta.

После установки перейдите в пользовательский репозиторий и выполните
[README](../README.md). `init` не устанавливает hooks. `hooks install`
запоминает абсолютный путь бинарника; не используйте временный каталог
или `go run` для интеграции. При нестандартном общем hooksPath подключайте
обработчик вручную согласно [HOOKS.md](HOOKS.md).

## Обновление

1. Сверьте суммы нового кандидата и распакуйте в **новый** каталог рядом
   со старым. Старую установку пока сохраните. Не заменяйте файлы `.git-task`.
2. Во всех репозиториях со старой интеграцией выполните по абсолютному пути
   старого бинарника `hooks uninstall`. Если старый бинарник недоступен,
   это можно сделать новым. Отказ при чужой правке hook требует разбора;
   не удаляйте обработчик силой.
3. Добавьте новый каталог первым в PATH текущего терминала и выполните
   `git task version`. Если вызывается другая версия, проверьте `Get-Command
   git-task` (Windows) или `command -v git-task` (Unix).
4. В каждом нужном репозитории выполните новым бинарником `hooks install`.
   Выполните `doctor` и сравните очередь/ID. Подключение общего плана
   сохраняется в plan.json; connect/init для обновления не нужны.
5. Когда старая интеграция снята везде, удалите только старый бинарник по
   инструкции ниже. Остаток документации можно оставить.

Переход схемы выполняется только отдельным `migrate` после диагностики.
Обновление бинарника само по себе не мигрирует и не публикует данные.

## Удаление

В каждом репозитории со включённой интеграцией сначала:

```text
git task hooks uninstall
```

Команда восстанавливает чужие hooks, удаляя только свою неизменённую обёртку.
Если кто-то изменил её после установки, команда откажется; сохраните файл
и разберите вручную. Uninstall не удаляет `.git-task/plan.json`, backup,
очередь, настройки remote, info/exclude, ветку git-task-plan или историю кода.
Удаление бинарника при оставленных hooks приведёт к диагностике отсутствия CLI.

После снятия hooks удалите **только известный бинарник** по точному пути:

```powershell
$binary = Join-Path $install 'git-task.exe'
if ((Get-FileHash -Algorithm SHA256 -LiteralPath $binary).Hash.ToLowerInvariant() -ne $item.binary_sha256) { throw 'File changed; inspect before removal' }
Remove-Item -LiteralPath $binary
```

На Unix сверьте hash с сохранённым manifest, затем `rm -- "$install/git-task"`.
Значение install должно оставаться точным путём выделенного каталога выбранной
версии; не используйте рекурсивное удаление репозитория. Закройте терминал,
чтобы убрать временное изменение PATH. План/очередь можно позже открыть
повторно установленным CLI. Для продолжения публикации после переустановки
используйте `team publish` с прежними ID.
