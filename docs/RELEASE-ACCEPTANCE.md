# Сквозная приёмка кандидата P17

Это инструкция и форма доказательств для независимого проверяющего. Фактический
статус этапа и результаты команд записываются только в [PLAN.md](../PLAN.md).
Исполнитель тесты не запускал. Результаты независимых запусков, замечания
и ограничения обязательной матрицы сохранены в PLAN.
По прямому решению пользователя 2026-10-05 недоступные Linux/macOS и Windows
symlink перенесены в TODO-04 PLAN; P17 принят по проверенной Windows-области.
Следующие команды для отложенных сред — маршрут будущего TODO-04, а не
свидетельство их успешного выполнения. Полный финальный Windows-набор — P18.
Контракт сценариев — [ACCEPTANCE.md](ACCEPTANCE.md), требования — P17 в PLAN.

## Точный кандидат

Принятый упаковщик P16 подготовил `.tools/releases/0.1.0-rc.1-r02`.
Версия продукта `0.1.0-rc.1`; manifest SHA-256:
`1d2feeb2d0a45fcbdf0b9e2e214bf5ced4fb611bfc83c695e2b085680dbb0657`.
Source SHA-256 из manifest:
`de9274bbadbe8237dd4fa47bea520d802aaebc37ef1354c70706a0fb04a3c920`.

| Native платформа | Архив / SHA-256 | Бинарник SHA-256 |
|---|---|---|
| Windows/amd64 | `git-task_0.1.0-rc.1_windows_amd64.zip` / `9f9bc8c08f5f3ddb0b2ba51ffd3bf51b83f82a6b9eca74953eb0d607c7f8ed63` | `2cea361849dc53467404f136943d515cec5239934378ce0fe893020596fd7adf` |
| Linux/amd64 | `git-task_0.1.0-rc.1_linux_amd64.tar.gz` / `40232b49d0922851aae6a53d24641ecde654b1e2f4fb7c81b73d15fa2aa588c7` | `21eb7c458dacf649470acbcd2e4a523e6104779bb9049484ea8b36ed1e089e1d` |
| macOS/amd64 | `git-task_0.1.0-rc.1_darwin_amd64.tar.gz` / `2a6f9c1192950adfc3c1fa859f13b76dc173a57eacb2e44807008fa8aa6ffe73` | `f005c1bc6fe8e6127928c67f85353267986fda32afada863ecae13e25348bb34` |

P17 меняет тестовый код и драйвер, но не `cmd/git-task/main.go`, `internal`,
`go.mod` или продуктовый бинарник. Старый архив содержит снимок документации
P16: выполнять текущие сценарии из ветки P17 против извлечённого R02.
Новая упаковка не нужна. При изменении продукта необходим новый кандидат,
новые hash и повтор затронутых проверок; драйвер намеренно отвергает иной manifest.

## Адресные сценарии

[`p17-tests.json`](../scripts/p17-tests.json) закрепляет имена и пакеты.
Драйвер `check.ps1 -Stage P17` проверяет manifest, hash native-архива и
извлечённого бинарника. Распаковка идёт в новый каталог `.tools/checks/candidate-*`;
прежние кандидаты не перезаписываются. Тесты cmd получают абсолютный путь,
ожидаемые SHA-256 и версию. При несоответствии они падают, без сборки замены.
Обычный запуск без переменной кандидата сохраняет прежнюю сборку тестового CLI.

| ACCEPTANCE / риск | Доказательство |
|---|---|
| A011–A043, A056–A070, локальный цикл | `TestBinaryCommands`: настоящий `git task`, Markdown/JSON import/export, pause/resume, complete/archive, точные селекторы и `--again`; `TestRealHooks`: merge/FF и чужие hooks |
| A078–A080, авторы, все-done, поздняя доставка | `TestAcceptanceOfflineApproachesAndLateDelivery/alice-first` и `/bob-first`: два порядка публикации кандидатом в независимых fixture; Alice/Bob/Charlie начинаются offline с отдельными ID; один done оставляет active, второй paused затем done закрывает задачу, поздний третий возвращает active с уведомлением; повтор publish не добавляет commit; сверка трёх clone и remote |
| A081, гонка после fetch | `TestAcceptancePublicationBarriers/retry`: перед первым реальным push Bob публикует новое действие; stale push отказывается; кандидат получает свежую базу и успешно повторяет; обе копии/remote сохраняют действия |
| A081, ограничение и последующая доставка | `…/exhaustion`: Bob обновляет remote перед каждым из трёх push; ненулевой результат, прежняя очередь/ID сохранены; обычный следующий publish доставляет её без повторного add/start |
| A082/A083, потерянный ответ и повторы | `…/lost-ack`: настоящий Git успешно публикует, прокси возвращает отказ; очередь остаётся; следующий publish подтверждает прежние action ID/digest ровно один раз |
| Отмена на границе публикации | `TestAcceptanceCanceledPublicationKeepsQueue`: после fetch, перед push прокси соединяется с loopback-барьером; проверяющий тест завершает CLI через отмену context; очередь и remote прежние, следующий publish доставляет действие |
| A077, A073, миграция/backup/repair | новый `TestAcceptanceMigrationAllStatuses`: один старый план со всеми todo/active/paused/done/archived, включая архивированный открытый подход и manual/imported историю; ID/status/order/события/неизвестный author/backup/исходные байты/повтор migrate/refs/HEAD/index. Прежний `TestAcceptanceMigrationAndBackupRepair` проверяет doctor/restore/no-op; storage interruption отдельно проверяет прерывания |
| A085/A087, реальные fetch/pull/hooks | `TestTeamPlanReceivedByRealFetchAndPullHooks`: plan-only, FF, merge, rebase, конфликт; `TestMissedReferenceHookRecoveredLocallyWithoutNetwork`, `TestReferenceConflictKeepsLocalPlanAndForeignHook` |
| A086/A088, отказ до мутаций и hook после эффекта | `TestTeamRejectedRequestsPreserveExistingQueue`, `TestRejectedTeamStartPreservesQueueAndRemote`, `TestTeamStartHookFailurePublishesConfirmedMutation` |
| A088, JSON истории общего плана | новый `TestAcceptanceTeamJSONTransfer`: исторический done и два active под Alice/Bob, настоящее локальное наблюдение; экспорт/импорт в пустой отдельный repo сохраняет authors/IDs/статусы/завершения/ветки/время/receipts; повторный экспорт совпадает побайтно. Team/local assignments/observations/warnings очищены по контракту, remote/refs/hooks не создаются |
| A088, uninstall с очередью | новый `TestAcceptanceUninstallPreservesTeamQueue`: offline-start и offline-add дают непустую очередь; doctor/uninstall сохраняют все байты плана, очередь/ID/локальную связь и refs/HEAD/index, чужой hook восстановлен и реально работает; последующий publish доставляет прежние action ID/digest ровно один раз обеим копиям |
| A083/A084/A086, устаревшее завершение/конфликты/битый remote/отказ сервера | выбранные app `TestForeignPublishedCompletionProtectedOnEveryReceivePath`, `TestTeamCorruptRemoteDoesNotReplaceLocalQueue`, `TestTeamOfflineAndConflictingVersionsStayReadable`, `TestTeamCancellationAndServerDenialKeepQueue`: защита опубликованной истории на каждом receive, сохранность очереди/версий, отказ локального изолированного сервера |
| A089 и отрицательный squash | расширенный `TestServerPostReceiveAutomaticallyPublishesCompletion`: отдельный bare server/worker и настоящий post-receive, сначала один done, затем все done, повтор события без нового metadata commit, `--again` и отрицательный server-side squash. Source/squash имеют разные явные сообщения и одинаковые фиксированные даты; до push проверяются разные ID и отсутствие ancestry с отдельной обработкой ошибки Git. App `TestServerCompletionRacesDeveloperPublication` проверяет гонку server/developer; `TestRealHooks` и выбранная `app: TestSyncRejectsStaleWorkAndUnsupportedIntegration` дополняют отрицательные графы |
| A006/A008/A026/A072, прерывания и межпроцессная запись | выбранные `app: TestDoctorReviewActualStartCrashRecovery`, `storage: TestMigrationInterruptionPreservesSourceAndDiagnosesRetry`, `TestTwoProcessesDetectStaleSnapshot`, `TestProcessCrashPreservesPlanAndBackup` |
| A074/A075, платформенные границы | выбранные storage symlink/junction/read-only; три release-теста путей; два doctor-теста чтения журнала и выбранный race |

Прокси [`scripts/acceptance-git`](../scripts/acceptance-git/main.go) — только
тестовый инструмент. Он собирается как временный `git` и присутствует в PATH
лишь дочернего CLI Alice. Остальные операции выполняет настоящий Git. В режиме
гонки Bob вызывает тот же кандидат с обычным PATH до делегирования push.
В режиме lost-ack прокси искажает только код возврата уже выполненного push.
В режиме cancel до реального push выполняется TCP handshake на `127.0.0.1`:
нет ожидания по sleep, обращений к внешней сети или случайного момента отмены.
Это проверяет прерывание процесса CLI, а не переносимость Ctrl+C терминала.
Контексты/тайм-ауты ограничивают время; тест сверяет локальные данные и remote.

Все repo/temp находятся внутри `GOTMPDIR` драйвера; собственные identity,
global/system config, hooks и только локальный file remote. Credentials,
пользовательский Git config, внешние push и установка реального server-hook
не нужны. Гонки дополнительно сравнивают staged/unstaged код, HEAD и индекс.
App/storage проверки используют свои точки отказа и не заявляются исполнением
R02; они дополняют бинарные сценарии crash/миграции, недоступные через обычный CLI.

## Команды независимого проверяющего

Windows, отдельный процесс из корня workspace:

~~~powershell
powershell -NoProfile -File scripts/check.ps1 -Stage P17 -Go .tools/go/bin/go.exe
~~~

После первого review драйвер P17 запускает каждый выбранный тест отдельной
командой с `-parallel=1` и собственным `-timeout=15m`. Один тайм-аут не расходует
общий бюджет всех cmd-тестов; провал останавливает маршрут и сохраняется как
провал. Не запускать два harness/toolchain одновременно: предыдущие
перекрывающиеся Windows-запуски не завершили cmd-группу. Лимит Publish 30s
и assertions не ослаблены. Timeout отдельного теста не означает PASS.

Адресный маршрут P17-R01 (уже подтверждён проверяющим; повтор нужен при его изменении):

~~~powershell
$p17Go = '.tools/go/bin/go.exe'
$p17Names = @('TestAcceptanceMigrationAllStatuses', 'TestAcceptanceTeamJSONTransfer', 'TestAcceptanceUninstallPreservesTeamQueue', 'TestAcceptanceOfflineApproachesAndLateDelivery')
foreach ($p17Name in $p17Names) {
    powershell -NoProfile -File scripts/check.ps1 -Stage P17 -Go $p17Go -P17Test $p17Name
    if ($LASTEXITCODE -ne 0) { throw "P17 review failed: $p17Name" }
}
~~~

Затем повторить цикл с `$p17Go = '.tools/review-go-min/go/bin/go.exe'`.
Прежде убедиться, что это именно Go 1.26.0; драйвер пишет фактическую версию.
Он проверяет тот же R02 и использует изолированное окружение при любом фильтре.
Для одного порядка: `-P17Test TestAcceptanceOfflineApproachesAndLateDelivery
-P17Subtest bob-first` (оба значения в одной командной строке). Имена тестов
должны присутствовать в manifest; subtest должен реально появиться в логе.

Адресная проверка P17-R02 — только изменённый серверный тест, последовательно
на двух harness против неизменённого R02:

~~~powershell
powershell -NoProfile -File scripts/check.ps1 -Stage P17 -Go .tools/go/bin/go.exe -P17Test TestServerPostReceiveAutomaticallyPublishesCompletion
powershell -NoProfile -File scripts/check.ps1 -Stage P17 -Go .tools/review-go-min/go/bin/go.exe -P17Test TestServerPostReceiveAutomaticallyPublishesCompletion
~~~

В логе нужны разные source/target IDs и успешное прохождение проверки
не-достижимости **до** push, затем active/отсутствие Completion после реального
server-hook. Если подтверждённый отрицательный граф всё же даёт done, сохранить
диагностику и вернуть продуктовый дефект исполнителю: потребуются исправление
продукта и новый кандидат. Первоначальный провал в PLAN не стирается успешным
повтором. Даты намеренно одинаковые, sleep не используется.

Маршрут Windows cmd, оставшийся после второго review: `-P17Test TestBinaryCommands` и
`TestRealHooks` на Go 1.26.0; на обеих версиях отдельно
`TestTeamPlanReceivedByRealFetchAndPullHooks`,
`TestMissedReferenceHookRecoveredLocallyWithoutNetwork`,
`TestReferenceConflictKeepsLocalPlanAndForeignHook`,
`TestTeamRejectedRequestsPreserveExistingQueue`,
`TestRejectedTeamStartPreservesQueueAndRemote`,
`TestTeamStartHookFailurePublishesConfirmedMutation`.
Перед следующим запуском сверить результаты в PLAN и выбрать только ещё
не подтверждённые или затронутые изменениями имена из этого маршрута.
Полный `TestRealHooks` и `TestBinaryCommands` на Go 1.26.8 подтверждены во втором
review; успешные неизменённые R01/app/storage/release
проверки повторять при новых ошибках/изменении среды, а не автоматически из-за
R01. На Windows runner с symlink-привилегией отдельно выполнить
`-P17Test TestSymlinkStoragePreservesDestination` и
`-P17Test TestReleaseOutputDirectory -P17Subtest symlink`, потребовав PASS.
Junction не заменяет эти случаи. Каждая команда имеет собственные vet/build.

Linux/macOS, native amd64 host с PowerShell 7, Go и Git в PATH:

~~~powershell
pwsh -NoProfile -File scripts/check.ps1 -Stage P17 -Go go -CandidateDirectory .tools/releases/0.1.0-rc.1-r02
pwsh -NoProfile -File scripts/check.ps1 -Stage P17 -Mode Race -Go go -CandidateDirectory .tools/releases/0.1.0-rc.1-r02
~~~

Race требует настроенных `CGO_ENABLED=1` и C-компилятора. Он запускает только
три выбранных storage-теста. Сквозной TCP handshake использует отдельные процессы;
race detector не доказывает межпроцессную атомарность. Fuzz в P17 не назначен:
парсеры продукта не менялись. Финальные security/fuzz/полный набор — маршрут P18.

На native runner передать текущую ветку P17 и неизменённые R02 manifest/архивы
в каталог внутри его workspace. Обычный checkout их не содержит: `.tools`
исключён из Git. CI P15 не переключён автоматически на P17; его успешный job
не является доказательством этого кандидата. Контейнерный `check-linux.ps1`
также остаётся P15/P18 и не выполняет бинарную приёмку P17.

## Матрица доказательств для заполнения в PLAN

Фактические результаты и оставшиеся проверки каждой строки — только в PLAN.

| Native среда | Go harness | Git | Необходимый запуск |
|---|---|---|---|
| Windows/amd64 | 1.26.0 | 2.51.0.windows.1 | P17 Checks; symlink без SKIP |
| Windows/amd64 | 1.26.8 | 2.51.0.windows.1 | P17 Checks; symlink без SKIP |
| Linux/amd64 | 1.26.0 | 2.51.0 | P17 Checks + Race |
| Linux/amd64 | 1.26.8 | 2.51.0 | P17 Checks + Race |
| macOS/amd64 | 1.26.0 | 2.51.0 | P17 Checks + Race |
| macOS/amd64 | 1.26.8 | 2.51.0 | P17 Checks + Race |

Go версии здесь относятся к harness; R02 собран Go 1.26.8. Ни кросс-сборка,
ни Linux под Windows не подтверждают native macOS. Windows symlink SKIP
прошлого P16 остаётся непроверенным случаем: нужен runner с правом создания
symlink. Не менять настройки ОС автоматически. Платформенные тесты могут
сознательно SKIP на другой ОС; фиксировать каждое имя и причину. Пропуск
обязательного случая обычно блокирует приёмку; для перечисленных отложенных
сред действует явное решение пользователя о переносе в TODO-04. SKIP остаётся
непроверенным случаем и не заменяется PASS.

Для каждой строки в журнале PLAN сохранить ОС/архитектуру/файловую систему,
Go/Git/CC, HEAD и dirty diff, manifest/hash, полную команду/exit code,
логи test/subtest, SKIP и результат ручной оценки каждого критерия P17.
После замечаний повторять затронутые сценарии актуального кандидата.
Полный `go test -count=1 ./...` до P18 не запускать.

## Границы первой версии

P17 подтверждает локальный file transport и изолированный server-worker.
Hosted Git provider без установленного триггера не обещает автоматическую
публикацию server-side merge; fetch/pull получает служебную ветку только когда
она уже опубликована. Status/sync/hooks сами в сеть не идут. Queue требует
следующего явного publish/настроенной пользовательской мутации после отказа.
Потерянное подтверждение сверяется по известным данным без сильного readback/ACL.

TODO общего плана из PLAN остаётся отложенным: squash/cherry-pick автоматическое
done, resolver ручных конфликтов, сильное серверное подтверждение, ACL и
обязательная онлайн-регистрация. Тест сохранности при отказе не означает
реализацию этих возможностей. ARM64, иные файловые системы и абсолютная защита
от внешнего редактора/power loss не добавлены этой матрицей.
