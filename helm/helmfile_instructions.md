## Почему выбрали helmfile
Использование helmfile позволяет:

- Зафиксировать имена релизов и namespace'ы. Ранее имена релизов и namespace'ов различались между окружениями и нужно было помнить, где какое.
- Зафиксировать используемые values. Не нужно думать какие values, secrets и в каком порядке использовать.
- Зафиксировать версии helm chart. Версии helm chart часто различались между окружениями из-за невнимательности\забывчивости\специально.
- Зафиксировать список helm chart, которые должны устанавливаются в конкретные окружения. Ранее нужно было помнить, в какие окружения какие helm chart ставить нужно, а в какие - нет;
- Упростить установку релизов в окружения. Ранее каждый чарт ставился руками, сейчас большую часть работы выполняет helmfile. 

## Разработка
**Начало**  
Для начала поставьте все что нужно для работы helmfile. Зафиксированные версии можно посмотреть в Convention:

- helm
- helm diff plugin
- helmfile
### Структура
**helmfile** использует следующие файлы/папки:

- **helmfile.yaml** - главный файл. Это по факту gotmpl файл, в нем содержится информация, как создать результирующий конфигурационный файл helmfile взависимости от настроек.
- **environments.yaml** - файл с описанием окружение для деплоя. Под окружением можно понимать одно окружение бэкэнда, которых может быть много на одном k8s кластере. Так и окружение как весь кластер k8s, в которые ставятся релизы в единственном экземпляре.
- **releases** - папка с описанием всех релизов. Содержит helmfile и папки с values файлами.
- **common.yaml** - общие настройки для использования всем subhelmfile'ами
- **FOLDER_NAME** - слой релизов, обьединенным общими правилами (например, shared - общие релизы, dedicated - только для dedicated кластера, meta - только для мета кластера)
    - **helmfile.yaml** - описание релизов для этого слоя
    - **RELEASE_NAME** - содержит values для конкретного релиза.
        - values.yaml -
        - values..yaml.tmpl
        - values-tier-dev.yaml 
        - values-dev-ci.yaml 

### Правило формирования values
Values указываем в следующем порядке:

1. values.yaml - общие значения для всех окружений.
2. values.yaml.gotmpl - общие значения для всех окружений, которые можно кастомизировать через StateValues (переменные окружения, которые указываются в environmets.yaml).
3. values-tier-{TIER}.yaml - values для окружений разного тира. В целом, можно делать по аналогии срезы values - tier, publicity, cloud и тп.
4. values-{ENV_NAME}.yaml - настройки для конкретного окружения

### Добавление нового релиза
Для этого требуется следующее:

Понять, к какому слою принадлежит новый релиз
В этом слое в файле helmfile.yaml добавить описание релиза. Пример:
Добавление нового релиза
```yaml
- name: mockserver
  chart: strikerz/mockserver
  namespace: {{ .Values | get "namespace" .Environment.Name }}
  version: 5.14.0
  installed: {{ .Values | get "mockserver.enabled" false | toYaml }}  # вот эта магия означает, что если флаг установлен релиз будет установлен, иначе - удален\не
  values:
    - "{{`{{ .Release.Name }}`}}/values.yaml"
    - "{{`{{ .Release.Name }}`}}/values-{{ .Environment.Name }}.yaml"  # будет искать values файл с таким шаблоном имени, например game-backend/amazon-metadata-ec2/values-local.yaml
```

### Удаление релиза
Есть 2 способа удалить релиз:

1. Просто удалить описание. Это чревато тем, что при синхронизации релиз не удалиться, а просто перестанет управляться через helmfile.
2. Сделать миграцию. Указать installed: false, что приведет к тому, что helmfile при следующей синхронизации удалит релиз.
По прошествиии времени, когда удаление расскатано на все окружения можно будет удалить и описание.

### Удаление релиза
```yaml
- name: mockserver
  chart: strikerz/mockserver
  namespace: {{ .Values | get "namespace" .Environment.Name }}
  version: 5.14.0
  installed: false
```

### Переименование релиза
Есть 2 способа переименовать релиз:

1. Просто переименовать. Но, как и в случае удаления, релиз со старым названием никуда не исчезнет, нужно будет удалить его в ручную.
2. Сделать миграцию:
    - Удалить старый (см. Удаление релиза п2).
    - Добавить релиз с новым именем.

### Использование
Все команды должны выполняться в папке, где лежит одновременно helmfile.yaml и environment.yaml. Шаблон команды:
```bash
helmfile [-e ENV_NAME] [-l LABEL=VALUE] command
```
Где:

**ENV_NAME** - это имя окружения. Имена всех окружений посмотреть в файле environments.yaml  
**LABEL=VALUE** - у каждого релиза есть свой набор лейблов, с помощью которых можно делать фильтрацию. Из часто используемых - имя релиза. Например: `name=prometheus-stack`
### Примеры команд:

Построить результирующий конфигурационный файл для local окружения
```bash
helmfile -e local build
```
Устанановить все релизы для local окружения
```bash
helmfile -e local sync
```
Переустановить релиз редиса для local окружения
```bash
helmfile -e local -l name=redis sync
```
**helmfile list**  
Выводит список релизов. Позволяет понять, какие релизы будут установлены, их имена, неймспейсы, лейблы и версии чартов.
```bash
helmfile -e local list
``` 
**helmfile sync**  
Устанавливает релизы в текущий выбранный kubernetes cluster. Убедитесь, что окружение helmfile и kubernetes cluster совпадают.
```bash
helmfile -e local sync
```
Можно установить конкретный релиз:
```bash
helmfile -e local -l name=meta-core sync
```
**helmfile diff**  
Отображает разницу между манифестом текущих установленных релизов и манифестов, которые будут установлены.
```bash
helmfile -e local diff
```
Можно посмотреть для конкретного релиза
```bash
helmfile -e local -l name=meta-core diff
```
**helmfile apply**  
Выполняет обновления релиза только в том случае, если есть изменения.
```bash
helmfile -e local apply
```
Можно так же применить изменения для конкретного релиза.
```bash
helmfile -e local -l name=meta-core apply
```
**WARNING!** Если релиз еще не был установлен, команда может упасть. Это связано с особенность работы helm плагина diff.


**helmfile destroy**
Удаляет релизы. Выполняется `helm uninstall`.
```bash
helmfile -e local destroy
```
Конкретный релиз meta-core
```bash
helmfile -e local -l name=meta-core destroy
```
**helmfile status**  
Выводит статус текущих релизов
```bash
helmfile -e local status
```
**helmfile template**  
Шаблонизирует релиз, выдавая манифест для kubernetes. Очень удобно проверять, все ли корректно values применяются и в целом дебажить чарты.
```bash
helmfile -e local template
```
Пример для конкретного чарта meta-core
```bash
helmfile -e local -l name=meta-core template
```

**helmfile write-values**  
Записывает один общий values для релизов. Таким образом удобно посмотреть как смержились values и какие значения в результате будут переданы helm'у.
```bash
helmfile -e local write-values
```
Пример для  конкретного чарта meta-core
```bash
helmfile -e local -l name=meta-core write-values
```
**helmfile build**
Билдит результирующий helmfile. Наш helmfile содержит шаблоны и прочую магию. Иногда очень удобно посмотреть, как же выглядит в результате конфиг.
```bash
helmfile -e local build
```
**helmfile lint**  
Запускает линтинг для чартов. Проверка, что чарты корректно валидируются helm'ом. Иногда это помогает выявить проблемы  с совместимостью kubernetes.
```bash
helmfile -e local lint
```

## FAQ
### Как установить отдельный релиз?  
Выполнить команду `sync` c лейблом имени конкретного релиза
```bash
helmfile -e local -l name=redis sync
```
### Как удалить релиз?
Выполнить команду `destroy` c лейблом имени конкретного релиза
```bash
helmfile -e local -l name=redis destroy
```
### Как переустановить релиз?
Нужно удалить релиз `destroy` и потом его установить `sync`
```bash
helmfile -e local -l name=redis destroy
helmfile -e local -l name=redis sync
```
### Как перезапустить релиз?
Выполнить команду `sync`, так как она безусловно выполняет `upgrade` релизу
```bash
helmfile -e local sync
helmfile -e local -l name=redis sync
```
### Как посмотреть, какие изменения будут применены?
Используйте команду `diff`, работает и для конкретно релиза. Если релиз еще не установлен, то могут возникнуть проблемыс с CRD
```bash
helmfile -e local diff
helmfile -e local -l name=redis diff
```
### Как посмотреть, что будет установлено?
Используйте команду `list`
```bash
helmfile -e local list
```
Чтобы посмотреть какие изменения будут применены (и какие релизы будут задеты)
```bash
helmfile -e local diff
```
## Troubleshooting
**No state file found**  
`helmfile` не находит конфигурационных файлов. Если у вас такая ошибка, значит вы запускаете команду не в папке environment.
```bash
no state file found. It must be named helmfile.d/*.{yaml,yml} or helmfile.yaml, otherwise specified with the --file flag
UPGRADE FAILED: another operation (install/upgrade/rollback) is in progress
```
Нужно откатить текущий релиз и попробовать снова:
```bash
helm ls -A -a # найдите зависший релиз
helm rollback boh-k8s 3 # откатите на текущую версию
```
## Документация
https://helmfile.readthedocs.io/en/latest/#configuration
https://helmfile.readthedocs.io/en/latest/writing-helmfile/