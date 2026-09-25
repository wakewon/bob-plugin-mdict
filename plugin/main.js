/**
 * MDict for Bob — 本地 MDict 词典插件
 *
 * 这个插件刻意保持很薄：它不解析 MDX/MDD，也不解析词典 HTML。
 * 所有繁重工作都在本机的 bob-mdict 服务里完成，插件只负责
 * 取词、发请求、把结构化结果映射成 Bob 的 toDict，以及错误处理。
 *
 * @author wakewon
 * @homepage https://github.com/wakewon/bob-plugin-mdict
 * @license GPL-3.0-or-later
 */

// 插件与本地服务是两个独立升级的组件，靠 apiVersion 约定兼容性。
var REQUIRED_API_VERSION = 'v2';

// build-plugin.sh replaces these two markers only in the packaged main.js.
// They are diagnostic identity, not compatibility gates.
var PLUGIN_VERSION = '__BOB_MDICT_PLUGIN_VERSION__';
var PLUGIN_BUILD_COMMIT = '__BOB_MDICT_PLUGIN_COMMIT__';

var DEFAULT_SERVICE_URL = 'http://127.0.0.1:15321';

var TROUBLESHOOTING_LINK = 'https://github.com/wakewon/bob-plugin-mdict#troubleshooting';

// 词典可以是任何语言，这里列出 Bob 常用语言以便用户自由指定翻译方向。
var SUPPORT_LANGUAGES = [
    'auto', 'en', 'zh-Hans', 'zh-Hant', 'ja', 'ko', 'fr', 'de', 'es',
    'it', 'ru', 'pt', 'nl', 'ar', 'th', 'vi'
];

function supportLanguages() {
    return SUPPORT_LANGUAGES;
}

// Bob 的查词弹窗需要很快返回；本地服务是毫秒级的，超时只用于兜底。
function pluginTimeoutInterval() {
    return 30;
}

function getOption(key, fallback) {
    var value = $option[key];
    if (value === undefined || value === null || value === '') {
        return fallback;
    }
    return value;
}

// normalizeBaseURL 去掉末尾斜杠，避免拼出 //v2/lookup 这种地址。
function normalizeBaseURL(raw) {
    var url = String(raw || '').trim();
    if (url === '') {
        url = DEFAULT_SERVICE_URL;
    }
    if (url.indexOf('://') < 0) {
        url = 'http://' + url;
    }
    while (url.length > 1 && url.charAt(url.length - 1) === '/') {
        url = url.substring(0, url.length - 1);
    }
    return url;
}

function configuredDictionaryID() {
    return String(getOption('dictionaryID', '') || '').trim();
}

function parsePositiveInt(raw, fallback) {
    var parsed = parseInt(String(raw), 10);
    if (isNaN(parsed) || parsed <= 0) {
        return fallback;
    }
    return parsed;
}

var SUPERSCRIPT_DIGITS = {
    '⁰': '0', '¹': '1', '²': '2', '³': '3', '⁴': '4',
    '⁵': '5', '⁶': '6', '⁷': '7', '⁸': '8', '⁹': '9'
};

// parseRecordSelector recognizes only the reserved trailing navigation forms.
// The returned base is intentionally not lowercased or normalized: the server
// remains the sole owner of exact-key Unicode/case semantics.
function parseRecordSelector(raw) {
    var text = String(raw || '').trim();
    if (text === '') {
        return null;
    }

    var end = text.length;
    var start = end;
    var digits = '';
    while (start > 0 && SUPERSCRIPT_DIGITS[text.charAt(start - 1)] !== undefined) {
        start--;
        digits = SUPERSCRIPT_DIGITS[text.charAt(start)] + digits;
    }
    if (digits !== '') {
        return validRecordSelector(text.substring(0, start), digits);
    }

    var match = text.match(/^(.*)\^\{([0-9]+)\}$/);
    if (!match) {
        match = text.match(/^(.*)\^([0-9]+)$/);
    }
    if (!match) {
        return null;
    }
    return validRecordSelector(match[1], match[2]);
}

function validRecordSelector(base, digits) {
    base = String(base || '').trim();
    var ordinal = Number(digits);
    if (base === '' || !isFinite(ordinal) || Math.floor(ordinal) !== ordinal ||
        ordinal < 1 || ordinal > 9007199254740991) {
        return null;
    }
    return { base: base, recordOrdinal: ordinal };
}

function configuredMultiRecordMode() {
    return getOption('multiRecordMode', 'separate') === 'combined' ? 'combined' : 'separate';
}

function configuredPresentationMode() {
    var mode = getOption('presentationMode', 'dict');
    if (mode === 'plain' || mode === 'markdown') {
        return mode;
    }
    return 'dict';
}

/**
 * describeTransportError 把连接失败翻译成用户能照着做的提示。
 * “服务没装”和“服务没跑”对用户来说是完全不同的两件事。
 */
function describeTransportError(serviceURL, message) {
    return {
        type: 'network',
        message: '无法连接本地 MDict 服务',
        addition: '插件需要本机的 bob-mdict 服务才能查词。\n\n' +
            '请确认：\n' +
            '1. 已经安装服务：brew install wakewon/tap/bob-mdict\n' +
            '2. 服务正在运行：brew services start bob-mdict\n' +
            '3. 地址正确：当前配置为 ' + serviceURL + '\n\n' +
            '可以在终端运行 bob-mdict --check 自查。' +
            (message ? '\n\n底层错误：' + message : ''),
        troubleshootingLink: TROUBLESHOOTING_LINK
    };
}

// readBody 兼容 Bob 把 JSON 解析成对象、以及原样返回字符串两种情况。
function readBody(resp) {
    var data = resp && resp.data;
    if (data === undefined || data === null) {
        return null;
    }
    if (typeof data === 'string') {
        try {
            return JSON.parse(data);
        } catch (e) {
            return null;
        }
    }
    return data;
}

function statusCodeOf(resp) {
    if (resp && resp.response && typeof resp.response.statusCode === 'number') {
        return resp.response.statusCode;
    }
    return 0;
}

/**
 * serviceErrorFor 把服务返回的错误码映射成带处置建议的 Bob 错误。
 */
function serviceErrorFor(statusCode, body, serviceURL) {
    var code = body && body.error ? body.error : '';
    var hint = body && body.hint ? body.hint : '';

    if (code === 'noDictionaries') {
        return {
            type: 'notFound',
            message: '还没有安装任何词典',
            addition: (hint || '把包含 .mdx/.mdd 文件的词典文件夹复制到词典目录中。') +
                '\n\n默认词典目录：\n~/Library/Application Support/bob-mdict/dictionaries/\n\n' +
                '复制完成后运行 bob-mdict --rescan，或重启服务。',
            troubleshootingLink: TROUBLESHOOTING_LINK
        };
    }
    if (code === 'dictionaryNotFound' || code === 'dictionaryUnavailable') {
        return {
            type: 'notFound',
            message: code === 'dictionaryUnavailable' ? '指定的 MDict 词典当前不可用' : '未找到指定的 MDict 词典',
            addition: '请在 MDict 中查询 /list 查看当前词典及 ID。' +
                (hint ? '\n\n服务提示：' + hint : ''),
            troubleshootingLink: TROUBLESHOOTING_LINK
        };
    }
    if (code === 'recordNotFound') {
        return {
            type: 'notFound',
            message: body && body.message ? body.message : '没有这个词条记录',
            addition: hint
        };
    }
    if (statusCode === 404) {
        return { type: 'notFound', message: '词典中没有收录这个词' };
    }
    if (statusCode === 400) {
        return { type: 'param', message: body && body.message ? body.message : '请求无效' };
    }
    return {
        type: 'api',
        message: body && body.message ? body.message : ('本地服务返回了 HTTP ' + statusCode),
        addition: hint,
        troubleshootingLink: TROUBLESHOOTING_LINK
    };
}

/**
 * buildRequestBody 组装查询请求。
 *
 * 服务直接返回对应的呈现结果；插件不解释 EntrySet，也不解析 Markdown。
 */
function buildRequestBody(text, recordOrdinal) {
    var presentation = configuredPresentationMode();
    var body = {
        query: text,
        multiRecordMode: configuredMultiRecordMode(),
        format: presentation === 'dict' ? 'bob' : presentation,
        mode: 'exact',
        maxExamples: parsePositiveInt(getOption('maxExamples', '3'), 3),
        includeExamples: getOption('showExamples', 'enable') === 'enable',
        includeExtras: getOption('showExtras', 'enable') === 'enable',
        includeGrammar: getOption('showGrammar', 'enable') === 'enable',
        limit: 1
    };
    var dictionaryID = configuredDictionaryID();
    if (dictionaryID !== '') {
        body.dictionaries = [dictionaryID];
    }
    if (recordOrdinal > 0) {
        body.recordOrdinal = recordOrdinal;
    }
    return body;
}

/**
 * presentedText 把一份已经排好版的文档交给 Bob。
 *
 * Bob 1.21.0 起，译文以 content = { format, text } 声明如何理解 text，且优先于
 * toParagraphs：format 为 markdown 时按 Markdown 渲染，为 plain 时原样显示。
 * 更早的 Bob 会忽略 content，只读 toParagraphs，所以两者同时给出：旧版本看到的
 * 仍是同一份文本（单个元素，按纯文本显示），新版本得到明确的渲染格式。
 * 不设 format 为 lines：那是逐行对应原文的机器翻译格式，词典词条不是。
 */
function presentedText(format, text) {
    return {
        content: { format: format, text: text },
        toParagraphs: [text]
    };
}

function dictionaryListParagraphs(body) {
    var dictionaries = body && body.dictionaries ? body.dictionaries : [];
    if (dictionaries.length === 0) {
        return [
            '未发现 MDict 词典',
            '词典目录：' + ((body && body.directory) || '~/Library/Application Support/bob-mdict/dictionaries/')
        ];
    }
    var paragraphs = ['MDict dictionaries'];
    for (var i = 0; i < dictionaries.length; i++) {
        var dictionary = dictionaries[i] || {};
        var lines = [(i + 1) + '. ' + (dictionary.title || '未命名词典'), 'ID: ' + (dictionary.id || '—')];
        if (dictionary.health && dictionary.health !== 'ok') {
            lines.push('状态：不可用');
            if (dictionary.diagnostics && dictionary.diagnostics.length > 0) {
                lines.push('诊断：' + dictionary.diagnostics.join('；'));
            }
        }
        paragraphs.push(lines.join('\n'));
    }
    return paragraphs;
}

function listDictionaries(query, serviceURL) {
    $http.request({
        method: 'GET',
        url: serviceURL + '/v2/dictionaries',
        timeout: 15,
        cancelSignal: query.cancelSignal,
        handler: function (resp) {
            if (resp.error) {
                query.onCompletion({ error: describeTransportError(serviceURL, resp.error.message) });
                return;
            }
            var statusCode = statusCodeOf(resp);
            var body = readBody(resp);
            if (statusCode !== 200 || !body) {
                query.onCompletion({ error: serviceErrorFor(statusCode, body, serviceURL) });
                return;
            }
            // 1.21+ 会把多元素的 toParagraphs 当作逐行对应原文的 lines 格式，而 /list
            // 没有对应的原文，所以 content 用一份段落间空一行的纯文本；旧版本仍读
            // 原来的多段落 toParagraphs，显示不变。
            var paragraphs = dictionaryListParagraphs(body);
            var listing = presentedText('plain', paragraphs.join('\n\n'));
            listing.toParagraphs = paragraphs;
            listing.from = query.detectFrom;
            listing.to = query.detectTo;
            query.onCompletion({ result: listing });
        }
    });
}

function translate(query, completion) {
    var serviceURL = normalizeBaseURL(getOption('serviceURL', DEFAULT_SERVICE_URL));
    // Bob preprocesses query.text. Since Bob 1.20.0, originalText preserves
    // exactly what the user entered and is therefore the only safe place to
    // recognize a control command without hijacking the real word "list".
    var originalText = String(query.originalText || '').trim();
    var lookupText = String(query.text || '').trim();

    if (originalText === '/list') {
        listDictionaries(query, serviceURL);
        return;
    }

    var selector = parseRecordSelector(originalText);
    var recordOrdinal = 0;
    if (selector) {
        lookupText = selector.base;
        recordOrdinal = selector.recordOrdinal;
    }

    if (lookupText === '') {
        query.onCompletion({ error: { type: 'param', message: '没有可查询的内容' } });
        return;
    }

    $http.request({
        method: 'POST',
        url: serviceURL + '/v2/lookup',
        header: { 'Content-Type': 'application/json' },
        body: buildRequestBody(lookupText, recordOrdinal),
        timeout: 15,
        cancelSignal: query.cancelSignal,
        handler: function (resp) {
            if (resp.error) {
                query.onCompletion({ error: describeTransportError(serviceURL, resp.error.message) });
                return;
            }

            var statusCode = statusCodeOf(resp);
            var body = readBody(resp);

            if (statusCode !== 200) {
                query.onCompletion({ error: serviceErrorFor(statusCode, body, serviceURL) });
                return;
            }
            var presentation = configuredPresentationMode();
            var effective = body && body.effectiveFormat ? body.effectiveFormat :
                (presentation === 'dict' ? 'bob' : presentation);
            var hasPresentation = body && (
                (effective === 'bob' && body.bob && body.bob.word) ||
                (effective === 'plain' && body.plain) ||
                (effective === 'markdown' && body.markdown)
            );
            if (!hasPresentation) {
                query.onCompletion({ error: { type: 'notFound', message: '词典中没有收录这个词' } });
                return;
            }

            var result;
            if (effective === 'plain') {
                result = presentedText('plain', body.plain);
            } else if (effective === 'markdown') {
                // 服务端已经由同一份结构化词条排好版；插件原样转交，不解释 Markdown。
                result = presentedText('markdown', body.markdown);
            } else {
                result = { toDict: body.bob };
            }
            result.from = query.detectFrom;
            result.to = query.detectTo;
            query.onCompletion({
                result: result
            });
        }
    });
}

/**
 * pluginValidate 区分三种失败：服务不可用、服务没有词典、以及服务与插件版本不兼容。
 * 三者的处置方式完全不同，所以不能笼统地报“配置错误”。
 */
function pluginValidate(completion) {
    var serviceURL = normalizeBaseURL(getOption('serviceURL', DEFAULT_SERVICE_URL));

    $http.request({
        method: 'GET',
        url: serviceURL + '/v2/status',
        timeout: 10,
        handler: function (resp) {
            if (resp.error) {
                completion({ result: false, error: describeTransportError(serviceURL, resp.error.message) });
                return;
            }

            var statusCode = statusCodeOf(resp);
            var body = readBody(resp);

            if (statusCode !== 200 || !body) {
                completion({
                    result: false,
                    error: {
                        type: 'api',
                        message: '本地服务响应异常（HTTP ' + statusCode + '）',
                        addition: '这个地址可能被其它程序占用了。请确认 ' + serviceURL + ' 指向 bob-mdict。',
                        troubleshootingLink: TROUBLESHOOTING_LINK
                    }
                });
                return;
            }

            if (body.service !== 'bob-mdict') {
                completion({
                    result: false,
                    error: {
                        type: 'api',
                        message: '该地址上运行的不是 bob-mdict',
                        addition: serviceURL + ' 被另一个程序占用了。请换一个端口重启 bob-mdict，或修改插件里的服务地址。',
                        troubleshootingLink: TROUBLESHOOTING_LINK
                    }
                });
                return;
            }

            if (typeof $log !== 'undefined' && $log && typeof $log.info === 'function') {
                $log.info('MDict plugin ' + PLUGIN_VERSION + ' (' + PLUGIN_BUILD_COMMIT + '), ' +
                    'bob-mdict ' + (body.serviceVersion || 'unknown') + ' (' +
                    (body.buildCommit || 'unknown') + '), API ' + (body.apiVersion || 'unknown'));
            }

            if (body.apiVersion !== REQUIRED_API_VERSION) {
                completion({
                    result: false,
                    error: {
                        type: 'api',
                        message: '插件与本地服务版本不兼容',
                        addition: '插件需要 API ' + REQUIRED_API_VERSION + '，' +
                            '而本地服务（bob-mdict ' + (body.serviceVersion || '未知版本') + '）提供的是 ' +
                            (body.apiVersion || '未知') + '。\n\n' +
                            '请把两者升级到同一代版本：\n' +
                            '  brew upgrade bob-mdict\n' +
                            '并在 Bob 中更新本插件。',
                        troubleshootingLink: TROUBLESHOOTING_LINK
                    }
                });
                return;
            }

            if (!body.healthyDictionaryCount) {
                completion({
                    result: false,
                    error: {
                        type: 'notFound',
                        message: '本地服务已运行，但还没有可用词典',
                        addition: '把包含 .mdx（以及配套 .mdd）的词典文件夹复制到：\n\n' +
                            (body.dictionaryDirectory || '~/Library/Application Support/bob-mdict/dictionaries/') +
                            '\n\n例如：\n  dictionaries/牛津高阶/牛津高阶.mdx\n  dictionaries/牛津高阶/牛津高阶.mdd\n\n' +
                            '然后运行 bob-mdict --rescan，或重启服务。\n\n' +
                            '本插件不附带任何词典数据，词典需要你自己准备。',
                        troubleshootingLink: TROUBLESHOOTING_LINK
                    }
                });
                return;
            }

            validateConfiguredDictionary(completion, serviceURL);
        }
    });
}

function validateConfiguredDictionary(completion, serviceURL) {
    var dictionaryID = configuredDictionaryID();
    if (dictionaryID === '') {
        completion({ result: true });
        return;
    }
    $http.request({
        method: 'GET',
        url: serviceURL + '/v2/dictionaries',
        timeout: 10,
        handler: function (resp) {
            if (resp.error) {
                completion({ result: false, error: describeTransportError(serviceURL, resp.error.message) });
                return;
            }
            var body = readBody(resp);
            var dictionaries = body && body.dictionaries ? body.dictionaries : [];
            for (var i = 0; i < dictionaries.length; i++) {
                if (dictionaries[i].id === dictionaryID) {
                    if (dictionaries[i].health && dictionaries[i].health !== 'ok') {
                        completion({ result: false, error: serviceErrorFor(503, { error: 'dictionaryUnavailable' }, serviceURL) });
                        return;
                    }
                    completion({ result: true });
                    return;
                }
            }
            completion({
                result: false,
                error: {
                    type: 'notFound',
                    message: '未找到 ID 为 ' + dictionaryID + ' 的词典',
                    addition: '请在 MDict 中查询 /list 获取当前词典 ID。',
                    troubleshootingLink: TROUBLESHOOTING_LINK
                }
            });
        }
    });
}
