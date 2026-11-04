/* i18n.js — runtime UI language layer for the dashboard (EN base).
   Each page ships a language <select> (id=lang-select); switching persists
   to localStorage and reloads. EN is the untouched upstream source — zh/ru
   are applied client-side from the packs below, so the HTML/JS stay
   merge-friendly with upstream. A MutationObserver covers dynamically
   rendered nodes; window.confirm is wrapped so native dialogs translate. */
(function () {
  'use strict';

  var KEY = 'dash_lang';

  /* ── language packs: exact matches (trimmed) + regex rules for templated strings ── */
  var PACKS = {
    zh: {
      dict: {
        // shared / nav / header
        'Account Pool': '账号池',
        'Proxies': '代理线路',
        'Settings': '设置',
        'Log out': '退出登录',
        // index page
        'Multi-account rotation · automatic failover on quota · device-warmed identities': '多账号轮换 · 额度耗尽自动切换 · 设备身份预热',
        'Import': '导入',
        'Export': '导出',
        'Claim all': '全部领取',
        'Claim all: preview billing and claim every plan the origin offers, per account': '全部领取:预览账单并逐账号领取官方提供的所有套餐',
        'Refresh quotas': '刷新额度',
        'Delete expired': '删除过期账号',
        'Add': '添加',
        'Job': '任务',
        'starting…': '启动中…',
        'Account overview': '账号总览',
        'Total accounts': '账号总数',
        'Healthy': '健康账号',
        'Quota exhausted': '额度耗尽',
        'Total quota (remaining)': '总剩余额度',
        'Account details': '账号明细',
        'loading…': '加载中…',
        'Add account': '添加账号',
        'Sign in via OAuth or paste credentials, one per line.': '通过 OAuth 登录,或粘贴凭证(每行一条)。',
        'OAuth login': 'OAuth 登录',
        'Paste credentials': '粘贴凭证',
        'Z.AI only allows the official ZCode callback URL, so you need to copy the browser URL once after the login completes; no username or password is required.': 'Z.AI 只允许官方 ZCode 回调地址,因此登录完成后需要把浏览器地址栏的 URL 复制回来一次;全程无需输入账号密码。',
        'Start login': '开始登录',
        '1. Open the authorization page and complete the Z.AI login:': '1. 打开授权页面并完成 Z.AI 登录:',
        'Copy': '复制',
        'Open': '打开',
        '● Waiting for authorization…': '● 等待授权中…',
        '— the server polls automatically; on success the JWT lands in the pool and gets warmed.': '—— 服务器自动轮询;成功后 JWT 会加入账号池并自动预热。',
        'Tokens': '凭证',
        'One per line: JWT, or email:pass:token — existing JWTs are skipped': '每行一条:JWT,或 邮箱:密码:token —— 已存在的 JWT 会自动跳过',
        'Egress line': '出口线路',
        'Pool — random from all lines': '池 —— 所有线路随机',
        'Bulk from file is served by env (ZCODE_KEYS=tokens.txt) at startup — here paste is for quick additions.': '从文件批量导入由环境变量(ZCODE_KEYS=tokens.txt)在启动时完成 —— 此处粘贴用于快速追加。',
        'Cancel': '取消',
        'Edit account': '编辑账号',
        'Change the name, credentials, egress line or disable specific models.': '修改名称、凭证、出口线路,或禁用特定模型。',
        'Name': '名称',
        'Leave empty to keep unchanged': '留空则保持不变',
        'Disabled models': '禁用的模型',
        'Once checked, this account is excluded from dispatching for that model.': '勾选后,该账号将不再参与此模型的调度。',
        'Save': '保存',
        'Oldest first / newest first': '旧→新 / 新→旧',
        'Credential / account': '凭证 / 账号',
        'Status': '状态',
        '3 strikes (final 3012) → 30m cooldown; a clean 200 resets': '3 次违规(最终 3012)→ 冷却 30 分钟;一次干净的 200 请求会清零',
        'Strikes': '违规',
        'Quota': '额度',
        'Actions': '操作',
        'No accounts in this view': '当前视图没有账号',
        'No plan data': '无套餐数据',
        'no plan': '无套餐',
        'Active for chat': '当前对话账号',
        'Make active for chat': '设为当前对话账号',
        'Refresh quota': '刷新额度',
        'Claim every offered plan': '领取所有可用套餐',
        'Edit: name, token, egress line, disabled models': '编辑:名称、凭证、出口线路、禁用模型',
        'Delete account': '删除账号',
        'Enable / disable': '启用 / 停用',
        'Active': '活跃',
        'Standby': '待命',
        'Exhausted': '额度耗尽',
        'Cooling': '冷却中',
        'Revoked': '已吊销',
        'Expired': '已过期',
        'Disabled': '已停用',
        'All': '全部',
        'Claim': '领取',
        'Check': '检测',
        'done': '完成',
        'Claim finished': '领取完成',
        'No accounts in file': '文件中没有账号',
        'OAuth account added': 'OAuth 账号已添加',
        'Authorization link generated. Copy it into a browser and complete the Z.AI login…': '授权链接已生成,请复制到浏览器并完成 Z.AI 登录…',
        'Flow expired — press Start login again.': '流程已过期 —— 请重新点击「开始登录」。',
        'Copied': '已复制',
        'Direct (no proxy)': '直连(不走代理)',
        'Account saved': '账号已保存',
        'Delete this account?': '确定删除该账号?',
        // proxies page
        'Proxy Settings': '代理设置',
        'Centralized management of HTTP / SOCKS5 egress lines · bind per account or rotate in the pool': 'HTTP / SOCKS5 出口线路集中管理 · 可按账号绑定或进池轮换',
        'Test current line': '测试当前线路',
        'Import lines': '导入线路',
        'Add proxy': '添加代理',
        'Current server egress': '服务器当前出口',
        'Not tested yet': '尚未测试',
        'Public IP via ip.sb → ipwho.is → ipapi.is fallback chain': '公网 IP:ip.sb → ipwho.is → ipapi.is 依次回退',
        'Proxy lines': '代理线路',
        'Clear all': '全部清空',
        'Create here, then select per account in Account Pool (Egress line)': '在此创建,再到「账号池」按账号选择(出口线路)',
        'No proxy lines yet': '还没有代理线路',
        'Connection format': '连接格式',
        'Proxies with authentication are supported': '支持带用户名密码认证的代理',
        'Lines are just egress addresses. What to use — a specific line or Pool (random from all) — is chosen per account in Account Pool → Edit → Egress line. Two failed probes auto-pause a line.': '线路只是出口地址。用哪条 —— 指定线路还是「池(全部随机)」—— 在「账号池 → 编辑 → 出口线路」按账号选择。连续 2 次探测失败会自动暂停该线路。',
        'HTTP, HTTPS, SOCKS4/5 — optionally with user:pass auth': 'HTTP、HTTPS、SOCKS4/5 —— 可选 user:pass 认证',
        'user:pass@host:port or http://host:port': 'user:pass@host:port 或 http://host:port',
        'main-dc (optional)': 'main-dc(可选)',
        'Bind to account': '绑定到账号',
        'Pool (all accounts)': '池(所有账号)',
        'Line vs Pool is chosen per account in Account Pool → Edit → Egress line.': '用线路还是池,在「账号池 → 编辑 → 出口线路」按账号选择。',
        'Import proxy lines': '导入代理线路',
        'One line per row — the same format as proxy.txt': '每行一条 —— 与 proxy.txt 格式相同',
        'Duplicates are skipped. New lines start unbound (pool).': '自动跳过重复项。新线路默认不绑定(进池)。',
        'line': '线路',
        'paused': '已暂停',
        'Probe: public IP + latency': '探测:公网 IP + 延迟',
        'Delete line': '删除线路',
        'Proxy added': '代理已添加',
        'All lines deleted': '已清空全部线路',
        // settings page
        'Runtime configuration — applied without a restart': '运行时配置 —— 保存即生效,无需重启',
        'Authentication': '认证',
        'Admin password': '管理密码',
        'leave empty to keep unchanged': '留空则保持不变',
        'Used to log in to this admin panel. Applied immediately.': '用于登录本管理后台,保存后立即生效。',
        'Gateway API key (optional)': '网关 API Key(可选)',
        'leave empty to use the admin password': '留空则使用管理密码',
        'When set, calls to /v1/messages must carry Authorization: Bearer <key> or x-api-key. Currently:': '设置后,调用 /v1/messages 必须携带 Authorization: Bearer <key> 或 x-api-key。当前:',
        'Model & quotas': '模型与额度',
        'Quota refresh interval (minutes)': '额度刷新间隔(分钟)',
        '0 disables the periodic monitor (button-only mode).': '0 表示关闭周期刷新(仅手动按钮)。',
        'Captcha pre-mint pool': '验证码预生成池',
        'Pool min / max': '池最小 / 最大',
        'Background solver keeps MIN ready tokens (≤ MAX, serial minting). Applies after restart.': '后台打码器保持 MIN 个可用验证参数(≤ MAX,串行生成)。重启后生效。',
        'Token TTL': 'Token 有效期',
        'Upstream params live ~2 minutes; expired tokens are evicted.': '上游参数有效期约 2 分钟,过期参数会被清除。',
        'Chat dump': '对话转储',
        'Write client + origin bodies to disk': '将客户端与上游的请求体写入磁盘',
        'File:': '文件:',
        '. Off by default after restart unless you turn it on here. Needs': '。重启后默认关闭,除非在此打开。需要配置',
        '(CHAT_DUMP empty)': '(CHAT_DUMP 未配置)',
        'Plan claim': '套餐领取',
        'Auto-claim offered plans': '自动领取可用套餐',
        'Daily warm only. Claim all/one on the accounts page always runs. Turn this off while chatting — warm still fires login events on the same MIX lines.': '仅作用于每日预热。账号页的「全部领取 / 单个领取」不受影响。对话进行中建议关闭 —— 预热仍会在同一条 MIX 线路上触发登录事件。',
        'Usage notes': '使用说明',
        '• Async (ticket-SSE):': '• 异步(ticket-SSE):',
        'Both chat endpoints accept Bearer auth with the admin password.': '两个对话端点均接受 Bearer 认证,密钥为管理密码。',
        'Enter something to save': '请先填写内容再保存',
        'Authentication saved': '认证设置已保存',
        'Saved': '已保存',
        'Saved — pool knobs apply after restart': '已保存 —— 池参数重启后生效',
        // login page
        'Admin Panel': '管理后台',
        'Enter the admin password to continue': '输入管理密码以继续',
        'Invalid password': '密码错误',
        'Continue': '继续',
        // titles
        'Zcode2Api · Account Pool': 'Zcode2Api · 账号池',
        'Zcode2Api · Proxies': 'Zcode2Api · 代理线路',
        'Zcode2Api · Settings': 'Zcode2Api · 设置',
        'Zcode2Api · Login': 'Zcode2Api · 登录',
        // app.js shared
        'job…': '任务…'
      },
      rules: [
        [/^Updated at (.+)$/, '更新于 $1'],
        [/^until (.+)$/, '截止 $1'],
        [/^Show (\d+) more \((\d+) \/ (\d+)\)$/, '显示更多 $1 条($2 / $3)'],
        [/^Show all (\d+)$/, '显示全部 $1 条'],
        [/^Show first (\d+)$/, '只显示前 $1 条'],
        [/^(\d+) lines?$/, '$1 条线路'],
        [/^Deleted (\d+) expired$/, '已删除 $1 个过期账号'],
        [/^Imported (\d+), skipped (\d+)$/, '已导入 $1 个,跳过 $2 个'],
        [/^Imported (\d+) line\(s\)$/, '已导入 $1 条线路'],
        [/^Added (\d+) account\(s\)$/, '已添加 $1 个账号'],
        [/^Delete (\d+) expired accounts\? This cannot be undone\.$/, '确定删除 $1 个已过期账号?此操作不可撤销。'],
        [/^Delete all (\d+) proxy lines\?$/, '确定删除全部 $1 条代理线路?'],
        [/^Account #(\d+) is now active$/, '账号 #$1 已设为当前对话账号'],
        [/^account #(\d+)$/, '账号 #$1'],
        [/^#(\d+) in flight…$/, '#$1 处理中…'],
        [/^Claim failed: ?(.*)$/, '领取失败: $1'],
        [/^Failed to load accounts: ?(.*)$/, '账号加载失败: $1'],
        [/^Save failed: ?(.*)$/, '保存失败: $1'],
        [/^Import failed: ?(.*)$/, '导入失败: $1'],
        [/^Probe failed: ?(.*)$/, '探测失败: $1'],
        [/^Failed: ?(.*)$/, '失败: $1'],
        [/^Account added: ?(.*)$/, '账号已添加: $1'],
        [/^Server returned non-JSON from (.+)$/, '服务器返回了非 JSON 响应: $1']
      ]
    },
    ru: {
      dict: {
        // shared / nav / header
        'Account Pool': 'Пул аккаунтов',
        'Proxies': 'Прокси',
        'Settings': 'Настройки',
        'Log out': 'Выйти',
        // index page
        'Multi-account rotation · automatic failover on quota · device-warmed identities': 'Ротация аккаунтов · автопереключение при исчерпании квоты · прогрев устройств',
        'Import': 'Импорт',
        'Export': 'Экспорт',
        'Claim all': 'Забрать все',
        'Claim all: preview billing and claim every plan the origin offers, per account': 'Забрать все: предпросмотр биллинга и получение всех предложенных тарифов для каждого аккаунта',
        'Refresh quotas': 'Обновить квоты',
        'Delete expired': 'Удалить истёкшие',
        'Add': 'Добавить',
        'Job': 'Задача',
        'starting…': 'запуск…',
        'Account overview': 'Обзор аккаунтов',
        'Total accounts': 'Всего аккаунтов',
        'Healthy': 'Здоровые',
        'Quota exhausted': 'Квота исчерпана',
        'Total quota (remaining)': 'Общий остаток квоты',
        'Account details': 'Детали аккаунтов',
        'loading…': 'загрузка…',
        'Add account': 'Добавить аккаунт',
        'Sign in via OAuth or paste credentials, one per line.': 'Вход через OAuth или вставьте учётные данные, по одной в строке.',
        'OAuth login': 'Вход через OAuth',
        'Paste credentials': 'Вставить учётные данные',
        'Z.AI only allows the official ZCode callback URL, so you need to copy the browser URL once after the login completes; no username or password is required.': 'Z.AI разрешает только официальный callback-URL ZCode, поэтому после входа нужно один раз скопировать URL из адресной строки браузера; логин и пароль не требуются.',
        'Start login': 'Начать вход',
        '1. Open the authorization page and complete the Z.AI login:': '1. Откройте страницу авторизации и завершите вход в Z.AI:',
        'Copy': 'Копировать',
        'Open': 'Открыть',
        '● Waiting for authorization…': '● Ожидание авторизации…',
        '— the server polls automatically; on success the JWT lands in the pool and gets warmed.': '— сервер опрашивает автоматически; при успехе JWT попадает в пул и прогревается.',
        'Tokens': 'Токены',
        'One per line: JWT, or email:pass:token — existing JWTs are skipped': 'По одной в строке: JWT или email:пароль:токен — существующие JWT пропускаются',
        'Egress line': 'Исходящий канал',
        'Pool — random from all lines': 'Пул — случайно из всех линий',
        'Bulk from file is served by env (ZCODE_KEYS=tokens.txt) at startup — here paste is for quick additions.': 'Массовый импорт из файла задаётся переменной окружения (ZCODE_KEYS=tokens.txt) при запуске — вставка здесь для быстрого добавления.',
        'Cancel': 'Отмена',
        'Edit account': 'Изменить аккаунт',
        'Change the name, credentials, egress line or disable specific models.': 'Измените имя, учётные данные, исходящий канал или отключите конкретные модели.',
        'Name': 'Имя',
        'Leave empty to keep unchanged': 'Оставьте пустым, чтобы не менять',
        'Disabled models': 'Отключённые модели',
        'Once checked, this account is excluded from dispatching for that model.': 'После отметки аккаунт исключается из диспетчеризации для этой модели.',
        'Save': 'Сохранить',
        'Oldest first / newest first': 'Сначала старые / сначала новые',
        'Credential / account': 'Учётные данные / аккаунт',
        'Status': 'Статус',
        '3 strikes (final 3012) → 30m cooldown; a clean 200 resets': '3 нарушения (финальный 3012) → охлаждение 30 минут; чистый 200 сбрасывает',
        'Strikes': 'Нарушения',
        'Quota': 'Квота',
        'Actions': 'Действия',
        'No accounts in this view': 'В этом виде нет аккаунтов',
        'No plan data': 'Нет данных о тарифах',
        'no plan': 'без тарифа',
        'Active for chat': 'Активен для чата',
        'Make active for chat': 'Сделать активным для чата',
        'Refresh quota': 'Обновить квоту',
        'Claim every offered plan': 'Забрать все предложенные тарифы',
        'Edit: name, token, egress line, disabled models': 'Изменить: имя, токен, канал, отключённые модели',
        'Delete account': 'Удалить аккаунт',
        'Enable / disable': 'Включить / отключить',
        'Active': 'Активен',
        'Standby': 'Резерв',
        'Exhausted': 'Исчерпана',
        'Cooling': 'Охлаждение',
        'Revoked': 'Отозван',
        'Expired': 'Истёк',
        'Disabled': 'Отключён',
        'All': 'Все',
        'Claim': 'Забор',
        'Check': 'Проверка',
        'done': 'готово',
        'Claim finished': 'Забор завершён',
        'No accounts in file': 'В файле нет аккаунтов',
        'OAuth account added': 'OAuth-аккаунт добавлен',
        'Authorization link generated. Copy it into a browser and complete the Z.AI login…': 'Ссылка авторизации создана. Откройте её в браузере и завершите вход в Z.AI…',
        'Flow expired — press Start login again.': 'Сессия истекла — нажмите «Начать вход» снова.',
        'Copied': 'Скопировано',
        'Direct (no proxy)': 'Напрямую (без прокси)',
        'Account saved': 'Аккаунт сохранён',
        'Delete this account?': 'Удалить этот аккаунт?',
        // proxies page
        'Proxy Settings': 'Настройки прокси',
        'Centralized management of HTTP / SOCKS5 egress lines · bind per account or rotate in the pool': 'Централизованное управление исходящими линиями HTTP/SOCKS5 · привязка к аккаунту или ротация в пуле',
        'Test current line': 'Проверить текущую линию',
        'Import lines': 'Импорт линий',
        'Add proxy': 'Добавить прокси',
        'Current server egress': 'Текущий исходящий IP сервера',
        'Not tested yet': 'Ещё не проверялось',
        'Public IP via ip.sb → ipwho.is → ipapi.is fallback chain': 'Публичный IP: ip.sb → ipwho.is → ipapi.is (по очереди)',
        'Proxy lines': 'Линии прокси',
        'Clear all': 'Очистить все',
        'Create here, then select per account in Account Pool (Egress line)': 'Создайте здесь, затем выберите для аккаунта в «Пул аккаунтов» (Исходящий канал)',
        'No proxy lines yet': 'Линий прокси пока нет',
        'Connection format': 'Формат подключения',
        'Proxies with authentication are supported': 'Поддерживаются прокси с аутентификацией',
        'Lines are just egress addresses. What to use — a specific line or Pool (random from all) — is chosen per account in Account Pool → Edit → Egress line. Two failed probes auto-pause a line.': 'Линии — просто адреса выхода. Что использовать — конкретную линию или «Пул (случайно из всех)» — выбирается для аккаунта в «Пул аккаунтов → Изменить → Исходящий канал». Две неудачные проверки ставят линию на паузу.',
        'HTTP, HTTPS, SOCKS4/5 — optionally with user:pass auth': 'HTTP, HTTPS, SOCKS4/5 — опционально с аутентификацией user:pass',
        'user:pass@host:port or http://host:port': 'user:pass@host:port или http://host:port',
        'main-dc (optional)': 'main-dc (необязательно)',
        'Bind to account': 'Привязать к аккаунту',
        'Pool (all accounts)': 'Пул (все аккаунты)',
        'Line vs Pool is chosen per account in Account Pool → Edit → Egress line.': 'Линия или пул выбирается для аккаунта в «Пул аккаунтов → Изменить → Исходящий канал».',
        'Import proxy lines': 'Импорт линий прокси',
        'One line per row — the same format as proxy.txt': 'Одна линия в строке — формат как в proxy.txt',
        'Duplicates are skipped. New lines start unbound (pool).': 'Дубликаты пропускаются. Новые линии не привязаны (пул).',
        'line': 'линия',
        'paused': 'пауза',
        'Probe: public IP + latency': 'Проверка: публичный IP + задержка',
        'Delete line': 'Удалить линию',
        'Proxy added': 'Прокси добавлен',
        'All lines deleted': 'Все линии удалены',
        // settings page
        'Runtime configuration — applied without a restart': 'Конфигурация на лету — применяется без перезапуска',
        'Authentication': 'Аутентификация',
        'Admin password': 'Пароль администратора',
        'leave empty to keep unchanged': 'оставьте пустым, чтобы не менять',
        'Used to log in to this admin panel. Applied immediately.': 'Используется для входа в эту панель. Применяется сразу.',
        'Gateway API key (optional)': 'Ключ шлюза (необязательно)',
        'leave empty to use the admin password': 'оставьте пустым, чтобы использовать пароль администратора',
        'When set, calls to /v1/messages must carry Authorization: Bearer <key> or x-api-key. Currently:': 'Если задан, запросы к /v1/messages должны содержать Authorization: Bearer <key> или x-api-key. Сейчас:',
        'Model & quotas': 'Модели и квоты',
        'Quota refresh interval (minutes)': 'Интервал обновления квот (мин)',
        '0 disables the periodic monitor (button-only mode).': '0 отключает периодический мониторинг (только кнопкой).',
        'Captcha pre-mint pool': 'Пул предгенерации капчи',
        'Pool min / max': 'Минимум / максимум пула',
        'Background solver keeps MIN ready tokens (≤ MAX, serial minting). Applies after restart.': 'Фоновый солвер держит MIN готовых токенов (≤ MAX, последовательная генерация). Вступает в силу после перезапуска.',
        'Token TTL': 'TTL токена',
        'Upstream params live ~2 minutes; expired tokens are evicted.': 'Параметры живут ~2 минуты; истёкшие токены удаляются.',
        'Chat dump': 'Дамп чата',
        'Write client + origin bodies to disk': 'Писать тела запросов клиента и апстрима на диск',
        'File:': 'Файл:',
        '. Off by default after restart unless you turn it on here. Needs': '. По умолчанию выключен после перезапуска, если не включить здесь. Нужен',
        '(CHAT_DUMP empty)': '(CHAT_DUMP не задан)',
        'Plan claim': 'Получение тарифов',
        'Auto-claim offered plans': 'Автоматически забирать доступные тарифы',
        'Daily warm only. Claim all/one on the accounts page always runs. Turn this off while chatting — warm still fires login events on the same MIX lines.': 'Только для ежедневного прогрева. «Забрать все/один» на странице аккаунтов работает всегда. Отключите во время чата — прогрев всё равно шлёт события входа на те же MIX-линии.',
        'Usage notes': 'Примечания',
        '• Async (ticket-SSE):': '• Асинхронный (ticket-SSE):',
        'Both chat endpoints accept Bearer auth with the admin password.': 'Оба чат-эндпоинта принимают Bearer-аутентификацию с паролем администратора.',
        'Enter something to save': 'Введите значение для сохранения',
        'Authentication saved': 'Аутентификация сохранена',
        'Saved': 'Сохранено',
        'Saved — pool knobs apply after restart': 'Сохранено — параметры пула вступят в силу после перезапуска',
        // login page
        'Admin Panel': 'Панель управления',
        'Enter the admin password to continue': 'Введите пароль администратора',
        'Invalid password': 'Неверный пароль',
        'Continue': 'Продолжить',
        // titles
        'Zcode2Api · Account Pool': 'Zcode2Api · Пул аккаунтов',
        'Zcode2Api · Proxies': 'Zcode2Api · Прокси',
        'Zcode2Api · Settings': 'Zcode2Api · Настройки',
        'Zcode2Api · Login': 'Zcode2Api · Вход',
        // app.js shared
        'job…': 'задача…'
      },
      rules: [
        [/^Updated at (.+)$/, 'Обновлено в $1'],
        [/^until (.+)$/, 'до $1'],
        [/^Show (\d+) more \((\d+) \/ (\d+)\)$/, 'Показать ещё $1 ($2 / $3)'],
        [/^Show all (\d+)$/, 'Показать все: $1'],
        [/^Show first (\d+)$/, 'Показать первые: $1'],
        [/^(\d+) lines?$/, '$1 лин.'],
        [/^Deleted (\d+) expired$/, 'Удалено истёкших: $1'],
        [/^Imported (\d+), skipped (\d+)$/, 'Импортировано: $1, пропущено: $2'],
        [/^Imported (\d+) line\(s\)$/, 'Импортировано линий: $1'],
        [/^Added (\d+) account\(s\)$/, 'Добавлено аккаунтов: $1'],
        [/^Delete (\d+) expired accounts\? This cannot be undone\.$/, 'Удалить истёкших аккаунтов: $1? Действие необратимо.'],
        [/^Delete all (\d+) proxy lines\?$/, 'Удалить все линии прокси ($1)?'],
        [/^Account #(\d+) is now active$/, 'Аккаунт #$1 теперь активен'],
        [/^account #(\d+)$/, 'аккаунт #$1'],
        [/^#(\d+) in flight…$/, '#$1 выполняется…'],
        [/^Claim failed: ?(.*)$/, 'Ошибка забора: $1'],
        [/^Failed to load accounts: ?(.*)$/, 'Не удалось загрузить аккаунты: $1'],
        [/^Save failed: ?(.*)$/, 'Ошибка сохранения: $1'],
        [/^Import failed: ?(.*)$/, 'Ошибка импорта: $1'],
        [/^Probe failed: ?(.*)$/, 'Ошибка проверки: $1'],
        [/^Failed: ?(.*)$/, 'Ошибка: $1'],
        [/^Account added: ?(.*)$/, 'Аккаунт добавлен: $1'],
        [/^Server returned non-JSON from (.+)$/, 'Сервер вернул не-JSON: $1']
      ]
    }
  };

  function detect() {
    try {
      var s = localStorage.getItem(KEY);
      if (s === 'en' || s === 'zh' || s === 'ru') return s;
    } catch (e) {}
    var nav = (navigator.language || 'en').toLowerCase();
    if (nav.indexOf('zh') === 0) return 'zh';
    if (nav.indexOf('ru') === 0) return 'ru';
    return 'en';
  }

  var lang = detect();

  function tr(s) {
    if (typeof s !== 'string' || !s) return s;
    var pack = PACKS[lang];
    if (!pack) return s;
    var key = s.trim();
    if (!key) return s;
    var hit = Object.prototype.hasOwnProperty.call(pack.dict, key) ? pack.dict[key] : undefined;
    if (hit === undefined) {
      for (var i = 0; i < pack.rules.length; i++) {
        if (pack.rules[i][0].test(key)) { hit = key.replace(pack.rules[i][0], pack.rules[i][1]); break; }
      }
    }
    if (hit === undefined) return s;
    var lead = s.match(/^\s*/)[0] || '';
    var tail = s.match(/\s*$/)[0] || '';
    return lead + hit + tail;
  }

  function translateAttrs(el) {
    var attrs = ['title', 'placeholder', 'aria-label'];
    for (var i = 0; i < attrs.length; i++) {
      var v = el.getAttribute(attrs[i]);
      if (v) {
        var t = tr(v);
        if (t !== v) el.setAttribute(attrs[i], t);
      }
    }
  }

  function translateText(node) {
    var v = node.nodeValue;
    if (!v || !v.trim()) return;
    var t = tr(v);
    if (t !== v) node.nodeValue = t;
  }

  function translateTree(root) {
    if (!root || root.nodeType === undefined) return;
    if (root.nodeType === 3) { translateText(root); return; }
    if (root.nodeType !== 1) return;
    translateAttrs(root);
    var walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT | NodeFilter.SHOW_ELEMENT, null, false);
    for (var n = walker.nextNode(); n; n = walker.nextNode()) {
      if (n.nodeType === 3) translateText(n);
      else translateAttrs(n);
    }
  }

  var mo = null;
  function startObserver() {
    if (mo || typeof MutationObserver === 'undefined') return;
    mo = new MutationObserver(function (muts) {
      for (var i = 0; i < muts.length; i++) {
        var m = muts[i];
        if (m.type === 'characterData') { translateText(m.target); continue; }
        for (var j = 0; j < m.addedNodes.length; j++) {
          var n = m.addedNodes[j];
          if (n.nodeType === 3) translateText(n);
          else if (n.nodeType === 1) translateTree(n);
        }
      }
    });
    mo.observe(document.body, { childList: true, characterData: true, subtree: true });
  }

  function syncSelect() {
    var sel = document.getElementById('lang-select');
    if (sel) sel.value = lang;
  }

  function applyLang() {
    document.documentElement.lang = lang === 'zh' ? 'zh-CN' : (lang === 'ru' ? 'ru' : 'en');
    syncSelect();
    if (lang === 'en') return; // EN = untouched upstream source
    document.title = tr(document.title);
    translateTree(document.body);
    startObserver();
    try {
      var _confirm = window.confirm;
      if (_confirm) {
        window.confirm = function (msg) { return _confirm.call(window, tr(String(msg))); };
      }
    } catch (e) {}
  }

  window.setLang = function (v) {
    if (v !== 'en' && v !== 'zh' && v !== 'ru') v = 'en';
    if (v === lang) { syncSelect(); return; }
    try { localStorage.setItem(KEY, v); } catch (e) {}
    location.reload(); // reload → clean re-translate from the EN base
  };

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', applyLang);
  else applyLang();
})();
