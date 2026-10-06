# Установка, обновление и удаление

Нужен Git 2.51.0 или новее. Архивы выпуска содержат готовый бинарник; Go для
его запуска не нужен. Для Windows требуется Git for Windows с `sh`, `mktemp`,
`cat` и `rm`, для Linux/macOS — эти утилиты и `tar`. Для скачивания Unix-установщиком
нужен `curl`; для SHA-256 используется `sha256sum` или `shasum`.

## Установка скриптом

1. На [странице Releases](https://github.com/ToothLonely/TaskPlugin/releases)
   выберите версию и скачайте `install.ps1` (Windows) или `install.sh` (Linux/macOS).
2. В терминале перейдите в обычный Git-репозиторий своего проекта.
3. Выполните скачанный файл по его пути:

```powershell
& "$env:USERPROFILE/Downloads/install.ps1"
git task version
```

```sh
sh "$HOME/Downloads/install.sh"
git task version
```

Запускайте блок своей ОС. Скрипт из Release уже содержит его точную версию;
скрипт из исходников требует `-Version 0.1.0` или `--version 0.1.0`.
До публикации первого выпуска скачивание архивов недоступно.

Скрипт выбирает Windows/amd64, Linux/amd64, Darwin/amd64 или Darwin/arm64,
скачивает архив и `SHA256SUMS` из выбранного тега `v<версия>`, сверяет сумму
и проверяет версию извлечённого бинарника. Повреждённый архив отклоняется до
регистрации alias. Установка находится в `.tools/git-task/<версия>/<ОС>-<архитектура>`
текущего проекта. Git config этого проекта содержит `alias.task` и
`git-task.install-path`. В `info/exclude` добавляется только каталог установки.
Глобальный PATH, Git-настройки и исходники проекта не меняются.

Не используйте sudo/администратора для установки. Существующий каталог,
tracked-путь, symlink/junction или чужой `task` alias дают отказ.
SHA-256 проверяет целостность передачи; архивы не подписаны.

После установки создайте план и подключите hooks:

```text
git task init --target main
git task hooks install
```

Для `master` укажите `--target master`. Если проект уже имеет план,
повторная инициализация для обновления не нужна. Задачи добавляются через
`add` или импорт собственного `plan.md`: [README](../README.md#импорт-и-экспорт).

## Установка из скачанного архива без сети

Из одного выпуска скачайте архив вашей ОС/архитектуры, `SHA256SUMS` и установщик.
Сохраните их в одном каталоге. Из своего Git-проекта передайте его путь:

```powershell
& "$env:USERPROFILE/Downloads/git-task-release/install.ps1" -FromDirectory "$env:USERPROFILE/Downloads/git-task-release"
```

```sh
sh "$HOME/Downloads/git-task-release/install.sh" --from-directory "$HOME/Downloads/git-task-release"
```

При локальной проверке собранного выпуска можно вызвать установщик прямо из
каталога, созданного упаковщиком. Проверки суммы и версии сохраняются.

## Сборка из исходников

С Go 1.26.0+ клонируйте исходники в отдельный каталог и выполните:

```powershell
go build -o bin/git-task.exe ./cmd/git-task
$gitTaskBin = Join-Path $PWD 'bin'
$env:PATH = $gitTaskBin + [IO.Path]::PathSeparator + $env:PATH
```

```sh
go build -o bin/git-task ./cmd/git-task
export PATH="$PWD/bin:$PATH"
```

PATH этих примеров действует в текущем терминале. Такая установка не создаёт
локальный alias и отличается от установки готового архива; постоянный путь
к бинарнику нужен в обоих случаях.

## Обновление

В каждом проекте, установленном скриптом:

```text
git task hooks uninstall
git config --local --unset alias.task
git config --local --unset git-task.install-path
```

Затем выполните установщик новой версии и `git task hooks install`.
План, очередь, Git-ветки и прежний бинарник сохраняются. Для снятия hooks
нужен старый бинарник; при чужой правке обёртки остановитесь и разберите ошибку.
После установки проверьте `git task version`, `git task doctor` и `git task status`.
Миграция старой схемы выполняется только отдельным `git task migrate`.

## Удаление

Сначала выполните те же три команды снятия hooks и alias. Потом удалите только
каталог выбранной установки `.tools/git-task/<версия>/<платформа>` после проверки
его содержимого. Это не требует удаления всего проекта или `.git-task`.
План, backup, очередь, служебная ветка и чужие hooks сохраняются.

Если CLI устанавливался через PATH, сначала снимите hooks в каждом подключённом
проекте, затем уберите бинарник и его путь из своих настроек оболочки.
