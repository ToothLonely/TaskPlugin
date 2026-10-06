# Проверки P15 и финальный маршрут P18

Точное дерево, согласованная Windows-матрица P18, последовательные команды
на обеих Go и дополнительные финальные маршруты — [HANDOFF.md](HANDOFF.md).

Отдельный маршрут P17 для собранного R02: [RELEASE-ACCEPTANCE.md](RELEASE-ACCEPTANCE.md),
`scripts/check.ps1 -Stage P17` и `scripts/p17-tests.json`. Он использует текущие
тесты и проверенный по hash архивный бинарник; P15/контейнерный драйвер не
подменяют native матрицу P17.

Статус и фактические результаты — только в [PLAN.md](../PLAN.md). Этот документ
описывает подготовленные команды, а не подтверждает их успешное выполнение.
Тесты, race, fuzz, security и CI запускает только независимый проверяющий.

## Адресный набор

[`scripts/p15-tests.json`](../scripts/p15-tests.json) содержит точные имена тестов
для пяти пакетов. [`scripts/check.ps1`](../scripts/check.ps1) собирает из них
закреплённые по началу и концу регулярные выражения `-run`, использует `-count=1`
и останавливается при ненулевом коде. Набор сводит P13/P14 с реальными
сквозными сценариями; весь пакет или весь проект в P15 не запускается.
Добавление новых этапов P16/P17 требует своего выбора затронутых тестов.

| Риск / сценарии ACCEPTANCE | Выбранное доказательство |
|---|---|
| A076, локальный цикл CLI, импорт/экспорт, pause/resume, --again | `cmd/git-task: TestBinaryCommands` включает `testBinaryLifecycle`; `TestRealHooks`; `app: TestSyncMergeFastForwardAndAgain` |
| A077, схема 1 → 2, сохранение ID/истории и отказ | `storage: TestExplicitMigrationKeepsSourceAndBackup`, `TestMigrationRefusesPendingOperationAndForeignBackup`, новый `TestMigrationInterruptionPreservesSourceAndDiagnosesRetry` на candidate/backup/installed; два `task: TestMigration…` |
| A078–A080, два автора, все-done и поздний offline-start | `app: TestTeamTwoRealClonesOfflineStartsAndAllDone`; `task: TestIndependentOfflineStartsAndDeliveryRepeats`, `TestCompletionAndLateOfflineStart` |
| A081, несколько stale push с авторами и сервером | новый `app: TestPublicationRepeatedRacesWithAuthorsAndServer`: Alice, Bob, Charlie, два барьера до push, затем успех на третьей попытке; сервер подтверждает только Alice, Bob остаётся active; повторы события без нового metadata commit |
| A081, лимит и последующая доставка | `app: TestTeamRepeatedRacesAreBoundedAndQueueSurvives`, `TestPublicationDeadlineFailureKeepsActionIDsForNextPublish` |
| A082, потерянное подтверждение | `app: TestTeamPublicationRaceRetryAndLostAcknowledgement`, `TestRecordedActionWithUnclosedJournalCannotPublish` |
| Отмена после fetch, непосредственно перед push | новый `app: TestPublicationCancellationAtPushBarrierRetainsIdentity`: настоящий отменённый context, неизменные очередь/remote, повтор с прежними action/attempt ID, одна квитанция |
| A083, старый active после done, защита опубликованной истории | `task: TestStaleActiveCannotUndoCompletedAttempt`, `TestSharedAdvanceProtectsAllConfirmedHistory`; `app: TestFreshRemoteCannotErasePublishedCompletion`, `TestForeignPublishedCompletionProtectedOnEveryReceivePath` |
| A084/A086, offline-add, конфликты, повреждение, отказ авторизации | `app: TestTeamOfflineAndConflictingVersionsStayReadable`, `TestTeamCorruptRemoteDoesNotReplaceLocalQueue`, `TestTeamCancellationAndServerDenialKeepQueue`; `task: TestSameIDAndManualFieldConflictsAreRejected`, `TestNewTaskIDsAreIndependentAndLegacyIDsSurvive` |
| A085/A087, fetch/pull, FF/merge/rebase/no-op/конфликт, пропущенный hook, без сети локально | три `cmd/git-task: TestTeamPlanReceivedByRealFetchAndPullHooks`, `TestMissedReferenceHookRecoveredLocallyWithoutNetwork`, `TestReferenceConflictKeepsLocalPlanAndForeignHook`; `app: TestForeignApproachIsNotLocalBranchAssignment` |
| A088, экспорт/импорт и uninstall, отказ до мутаций | `TestBinaryCommands`, `TestRealHooks`; три `cmd/git-task: TestTeamRejectedRequestsPreserveExistingQueue`, `TestRejectedTeamStartPreservesQueueAndRemote`, `TestTeamStartHookFailurePublishesConfirmedMutation` |
| A089, настоящий post-receive и серверная гонка | `cmd/git-task: TestServerPostReceiveAutomaticallyPublishesCompletion`; `app: TestServerCompletionRacesDeveloperPublication`, новый сценарий с тремя авторами |
| A044–A055, отрицательные squash/cherry-pick, старый tip, конфликт | `app: TestSyncRejectsStaleWorkAndUnsupportedIntegration`, `TestSyncConflictAndResolvedMerge`; отрицательный squash также внутри серверного теста бинарника |
| A006/A008/A072/A074, межпроцессный lock, crash и recovery | `storage: TestTwoProcessesDetectStaleSnapshot`, `TestProcessCrashPreservesPlanAndBackup`, `TestDoctorRecoveryGateBlocksWriter`; `app: TestDoctorReviewActualStartCrashRecovery`, `TestDoctorInterruptedStartRepairAndNoOp` |
| P14-R01, чтение параллельно закрытию журнала | `storage: TestDoctorReviewConcurrentJournalClosureDoesNotPanic`, `TestDoctorJournalSnapshotChangedDuringInspection` обычным и race-запуском |
| A075, пути с пробелами/Unicode, регистр/symlink/junction | временные repo существующих тестов; четыре теста путей storage, два Windows-specific теста в manifest |

Это трассировка адресной приёмки P15. Она не заменяет весь набор A001–A089
или финальный прогон P18. Проверяющий проверяет полноту, фактические subtests,
сообщения SKIP и вправе дополнить необходимый набор. Некоторые проверки путей
сознательно применимы только на определённой ОС; Windows symlink может быть
SKIP без соответствующей привилегии. Race не доказывает межпроцессный lock.

## Команды проверяющего

Из корня проекта в отдельном процессе PowerShell (скрипт меняет окружение
своего процесса; исходный терминал сохраняет настройки):

~~~powershell
powershell -NoProfile -File scripts/check.ps1 -Go .tools/go/bin/go.exe
powershell -NoProfile -File scripts/check.ps1 -Mode Fuzz -Go .tools/go/bin/go.exe -FuzzTime 30s
~~~

Для Linux/macOS использовать `pwsh -NoProfile -File scripts/check.ps1` с Go/Git
в PATH. Скрипт ничего не устанавливает. Race требует доступного C-компилятора
и `CGO_ENABLED=1`, выбранных проверяющим; затем отдельный процесс:

~~~powershell
pwsh -NoProfile -File scripts/check.ps1 -Mode Race
~~~

Fuzz отдельно запускает существующий `FuzzPlanJSON` и новые
`FuzzPlanJSONAtomic` (ошибка не изменяет receiver), `FuzzQueuedAction`
(ошибка не меняет исходный план, успешное действие повторяется идемпотентно),
`FuzzHookInput` (сложные stdin и аргументы hooks). Обычный адресный запуск
исполняет также seed corpus; генерация выполняется только маршрутом Fuzz.

Для security нужен уже подготовленный локально `govulncheck`:

~~~powershell
pwsh -NoProfile -File scripts/check.ps1 -Mode Security -Go .tools/go/bin/go.exe -Govulncheck .tools/security/bin/govulncheck.exe
~~~

Это Windows-пример с проектным Go; на Linux/macOS указать путь к выбранному
`go` и `govulncheck` без `.exe` либо оставить `-Go go`, если нужный Go уже в PATH.
Драйвер разрешает `-Go` в абсолютный путь и добавляет его каталог первым в PATH
своего процесса. Заголовок, команды Go и дочерний `govulncheck` используют один
toolchain, даже если в исходном PATH Go отсутствует или находится другая версия.
Пользовательский PATH и глобальные настройки не меняются; запускайте драйвер
в отдельном процессе, как показано выше.

Security обращается только к публичной базе Go, это отдельная сетевая проверка
без Git transport. Для полностью offline-среды передать `-VulnerabilityDatabase`
с `file://` URL заранее подготовленного снимка базы. Записать дату/источник
снимка; устаревшую базу не считать актуальной. Инструмент и база не скачиваются
локальным драйвером. CI отдельно готовит `golang.org/x/vuln/cmd/govulncheck@v1.8.0`
в `.tools/security`, затем запускает сканирование; зависимости продукта не меняются.

Полный набор разрешён только в P18:

~~~powershell
pwsh -NoProfile -File scripts/check.ps1 -Stage P18
~~~

Этот маршрут выполняет `go vet ./...`, `go test -count=1 -timeout=60m ./...`,
`go build ./...`. Race/fuzz/security выбираются дополнительно по финальным
критериям; запуск `-Stage P18` сам по себе не доказывает все среды. Лимит
каждого полного пакета увеличен по разрешению пользователя; продуктовые
сроки операций и тестовые утверждения не изменены. Container-драйвер P18
использует тот же 60m. Для явного P18 dispatch общий integration job получает
120 минут с запасом на подготовку/vet/build и дополнительные маршруты;
обычный P15 job сохраняет 45 минут. Изменение workflow не доказывает его запуск.

## Матрица и изоляция

[`checks.yml`](../.github/workflows/checks.yml) готовит восемь native-сред:

| ОС / архитектура | Go | Git | Маршруты |
|---|---|---|---|
| Windows/amd64 (`windows-latest`) | 1.26.0, 1.26.8 | 2.51.0.windows.1 (MinGit) | адресный набор, vet/build |
| Linux/amd64 (`ubuntu-latest`) | 1.26.0, 1.26.8 | 2.51.0 из исходников | адресный набор, vet/build, race; fuzz на 1.26.8 |
| macOS/amd64 (`macos-15-intel`) | 1.26.0, 1.26.8 | 2.51.0 из исходников | адресный набор, vet/build, race |
| macOS/arm64 (`macos-15`) | 1.26.0, 1.26.8 | 2.51.0 из исходников | адресный набор, vet/build, race; результат после запуска CI |

Git подготовлен внутри workspace с проверкой SHA-256. Unix-вариант намеренно
собран без HTTP/OpenSSL: тестам требуется только локальный file transport.
Это не проверка реального HTTPS/SSH-хостинга. Minimum Go 1.26.0 и Git 2.51.0
проверяются исполнением, не кросс-компиляцией. Windows race пока отдельный
локальный маршрут с настроенным компилятором. macOS ARM64 добавлен по запросу
пользователя от 2026-10-06; добавление job не является результатом его исполнения.
Другие ARM-платформы и файловые системы этой матрицей не покрываются.
По решению пользователя 2026-10-05 недоступные
native Linux/macOS и Windows symlink перенесены из P17 в TODO-04 PLAN;
для P18 согласована доступная Windows-матрица с обязательным полным набором.
Отложенные среды и SKIP не считаются пройденными.

CI имеет только `contents: read`, checkout не сохраняет credentials. Подготовка
toolchain и security DB требует сети; сами Git-сценарии используют изолированные
локальные bare remote, собственные identity/config/hooks. Драйвер убирает
унаследованные `GIT_*`, задаёт пустые global/system config и template, разрешает
только file transport, запрещает prompts, отключает загрузку Go-модулей при
проверках. Кеши/temp находятся в `.tools/checks`. Пользовательские Git-настройки
не меняются. В workflow нет push продукта или установки в реальный Git-server.

Для тестов сохранять stdout/stderr драйвера: версии Go/Git, GOOS/GOARCH/CC,
commit SHA, `git status --short` (отличает грязного кандидата), точные команды,
test/subtest имена и пропуски. Доказательство CI относится к SHA запуска;
после исправлений нужны новые результаты. Hosted runner labels и базовые ОС
изменяемые, конфигурация не обещает побайтовую воспроизводимость окружения.

Linux из Windows: `scripts/check-linux.ps1 -UseExistingImage` использует тот же
manifest и выполняет адресные ordinary/race тесты без сети внутри Linux tmpfs.
Новый образ требует python3 для чтения manifest; старый образ нужно пересобрать.
Создание образа меняет Docker storage вне workspace и требует отдельной
авторизации по AGENTS.md. `-Stage P18` включает финальный полный набор;
в P15 его не использовать. Container-драйвер не выполняет fuzz/security.

Первоисточники конфигурации: [setup-go](https://github.com/actions/setup-go),
[checkout](https://github.com/actions/checkout),
[SHA-256 MinGit 2.51.0](https://github.com/git-for-windows/git/releases/tag/v2.51.0.windows.1),
[govulncheck и параметр -db](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck).

## Установщики и регрессия быстрых Git-коммитов

P15 manifest включает TestArchiveInstaller из scripts/release: сборка нативного
CLI, установка из локального архива в изолированный Git-проект с пробелами,
Unicode и апострофом в пути, запуск через git task и проверка info/exclude.
Отрицательные сценарии проверяют сохранность Git config/файлов при неверной
контрольной сумме, чужом alias, занятом или tracked-каталоге. На Windows
запускается install.ps1, на Linux/macOS — install.sh. Rendering и ARM environment
проверяются отдельно. Сеть и настоящие пользовательские проекты не нужны.

TestSyncRejectsStaleWorkAndUnsupportedIntegration для squash/cherry-pick
использует фиксированные даты и разные сообщения коммитов; проверяет, что OID
переписанного коммита отличается, а задача остаётся active. Эти проверки не
зависят от того, успели ли Git-процессы завершиться в одну секунду. Изменения
нужно подтвердить новым CI-запуском на SHA исправления; прежние результаты
PR/master не являются проверкой нового кандидата.
