const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, 'main.js'), 'utf8')
    .replace('__BOB_MDICT_PLUGIN_VERSION__', '1.1.0-test')
    .replace('__BOB_MDICT_PLUGIN_COMMIT__', 'test123');

function load(options, respond) {
    const requests = [];
    const logs = [];
    const context = {
        $option: options || {},
        $log: { info(message) { logs.push(message); } },
        $http: {
            request(request) {
                requests.push(request);
                respond(request);
            }
        }
    };
    vm.createContext(context);
    vm.runInContext(source, context, { filename: 'main.js' });
    return { context, requests, logs };
}

function bobQuery(input, complete) {
    const query = {
        text: input.text,
        detectFrom: input.detectFrom || 'en',
        detectTo: input.detectTo || 'zh-Hans',
        cancelSignal: { test: true },
        onCompletion: complete
    };
    if (Object.hasOwn(input, 'originalText')) {
        query.originalText = input.originalText;
    }
    return query;
}

function response(statusCode, data) {
    return { response: { statusCode }, data };
}

test('blank ID uses first match and explicit ID restricts lookup', () => {
    for (const [dictionaryID, expected] of [['', undefined], ['abc123', ['abc123']]]) {
        let completion;
        const loaded = load({ dictionaryID }, request => {
            assert.equal(request.url, 'http://127.0.0.1:15321/v2/lookup');
            assert.equal(request.body.limit, 1);
            assert.equal(request.body.includeGrammar, true);
            assert.equal(JSON.stringify(request.body.dictionaries), JSON.stringify(expected));
            request.handler(response(200, { bob: { word: 'flimber', parts: [] } }));
        });
        loaded.context.translate(bobQuery({ text: 'flimber', originalText: 'flimber' }, value => { completion = value; }));
        assert.equal(completion.result.toDict.word, 'flimber');
        assert.equal('toParagraphs' in completion.result, false);
        assert.equal('fromParagraphs' in completion.result, false);
    }
});

// Bob types toParagraphs as an array of strings. The whole Markdown document
// travels as exactly one element so its formatting stays one unit; splitting it
// into per-paragraph strings would break fenced blocks, lists and tables apart.
test('Markdown presentation returns the whole document as one toParagraphs element', () => {
    let completion;
    const markdown = '# flimber\n\n## noun\n\n- **1** synthetic definition\n\n---\n\n## verb\n';
    const loaded = load({
        presentationMode: 'markdown', showExamples: 'disable', showExtras: 'disable', showGrammar: 'disable', maxExamples: '2'
    }, request => {
        assert.equal(request.body.format, 'markdown');
        assert.equal(request.body.includeExamples, false);
        assert.equal(request.body.includeExtras, false);
        assert.equal(request.body.includeGrammar, false);
        assert.equal(request.body.maxExamples, 2);
        request.handler(response(200, { markdown, matches: [{}] }));
    });
    loaded.context.translate(bobQuery({ text: 'flimber', originalText: 'flimber' }, value => { completion = value; }));
    assert.ok(Array.isArray(completion.result.toParagraphs));
    assert.equal(completion.result.toParagraphs.length, 1);
    assert.equal(completion.result.toParagraphs[0], markdown);
    assert.equal('toDict' in completion.result, false);
});

test('Plain presentation returns the whole document as one toParagraphs element', () => {
    let completion;
    const plain = 'flimber\n\nnoun\n1. synthetic definition\n';
    const loaded = load({ presentationMode: 'plain' }, request => {
        assert.equal(request.body.format, 'plain');
        request.handler(response(200, { effectiveFormat: 'plain', plain, matches: [{}] }));
    });
    loaded.context.translate(bobQuery({ text: 'flimber', originalText: 'flimber' }, value => { completion = value; }));
    assert.equal(Array.isArray(completion.result.toParagraphs), true);
    assert.equal(completion.result.toParagraphs.length, 1);
    assert.equal(completion.result.toParagraphs[0], plain);
    assert.equal('toDict' in completion.result, false);
});

test('Dictionary card honours the service effective Plain fallback', () => {
    let completion;
    const plain = '好\n\n第一段。\n\n第二段。\n';
    const loaded = load({}, request => {
        assert.equal(request.body.format, 'bob');
        request.handler(response(200, { effectiveFormat: 'plain', plain, matches: [{}] }));
    });
    loaded.context.translate(bobQuery({ text: '好', originalText: '好' }, value => { completion = value; }));
    assert.equal(Array.isArray(completion.result.toParagraphs), true);
    assert.equal(completion.result.toParagraphs.length, 1);
    assert.equal(completion.result.toParagraphs[0], plain);
    assert.equal('toDict' in completion.result, false);
});

test('Markdown presentation forwards the configured multi-record mode', () => {
    for (const [configured, expected] of [[undefined, 'separate'], ['separate', 'separate'], ['combined', 'combined']]) {
        let completion;
        const loaded = load({ presentationMode: 'markdown', multiRecordMode: configured }, request => {
            assert.equal(request.body.format, 'markdown');
            assert.equal(request.body.multiRecordMode, expected);
            request.handler(response(200, { markdown: '# wound\n', matches: [{}] }));
        });
        loaded.context.translate(bobQuery({ text: 'wound', originalText: 'wound' }, value => { completion = value; }));
        assert.equal(completion.result.toParagraphs.length, 1);
    }
});

// A record selector must reach the service identically in both presentations,
// so Markdown separate mode can answer with the selected record.
test('Markdown presentation forwards a reserved record selector', () => {
    for (const originalText of ['wound\u00b2', 'wound^2', 'wound^{2}']) {
        let completion;
        const loaded = load({ presentationMode: 'markdown' }, request => {
            assert.equal(request.body.query, 'wound');
            assert.equal(request.body.recordOrdinal, 2);
            request.handler(response(200, { markdown: '# wound\n', matches: [{}] }));
        });
        loaded.context.translate(bobQuery({ text: 'wound', originalText }, value => { completion = value; }));
        assert.equal(completion.result.toParagraphs[0], '# wound\n');
    }
});

test('dictionary card remains the default presentation', () => {
    let completion;
    const loaded = load({}, request => {
        assert.equal(request.body.format, 'bob');
        request.handler(response(200, { bob: { word: 'flimber', parts: [] } }));
    });
    loaded.context.translate(bobQuery({ text: 'flimber', originalText: 'flimber' }, value => { completion = value; }));
    assert.equal(completion.result.toDict.word, 'flimber');
    assert.equal('toParagraphs' in completion.result, false);
});

test('Bob-preprocessed /list uses originalText and never performs a lookup', () => {
    for (const originalText of ['/list', '  /list  ']) {
        let completion;
        const loaded = load({}, request => {
            assert.equal(request.method, 'GET');
            assert.equal(request.url, 'http://127.0.0.1:15321/v2/dictionaries');
			assert.equal(request.url.endsWith('/v2/lookup'), false);
            assert.equal(request.cancelSignal.test, true);
            request.handler(response(200, {
                directory: '/synthetic/dictionaries',
                dictionaries: [
                    { id: 'abc123', title: 'Synthetic Learner Dictionary', health: 'ok' },
                    { id: 'broken1', title: 'Broken Synthetic Dictionary', health: 'unavailable', diagnostics: ['test diagnostic'] }
                ]
            }));
        });
        loaded.context.translate(bobQuery({ text: 'list', originalText }, value => { completion = value; }));
        assert.equal(loaded.requests.length, 1);
        assert.equal(completion.result.toParagraphs[0], 'MDict dictionaries');
        assert.match(completion.result.toParagraphs[1], /Synthetic Learner Dictionary/);
        assert.match(completion.result.toParagraphs[1], /ID: abc123/);
        assert.match(completion.result.toParagraphs[2], /状态：不可用/);
        assert.match(completion.result.toParagraphs[2], /test diagnostic/);
        assert.equal('toDict' in completion.result, false);
    }
});

test('ordinary list and missing originalText remain normal lookups', () => {
    let calls = 0;
    const loaded = load({}, request => {
        calls++;
        assert.equal(request.method, 'POST');
        assert.equal(request.body.query, 'list');
        request.handler(response(404, {}));
    });
    loaded.context.translate(bobQuery({ text: 'list', originalText: 'list' }, () => {}));
    loaded.context.translate(bobQuery({ text: 'list' }, () => {}));
    assert.equal(calls, 2);
});

test('normal words use preprocessed text and are unaffected', () => {
    const loaded = load({}, request => {
        assert.equal(request.method, 'POST');
        assert.equal(request.body.query, 'good');
        request.handler(response(200, { bob: { word: 'good', parts: [] } }));
    });
    loaded.context.translate(bobQuery({ text: 'good', originalText: 'good' }, () => {}));
});

test('lookup preserves headword case from Bob through the HTTP request', () => {
    const loaded = load({}, request => {
        assert.equal(request.method, 'POST');
        assert.equal(request.body.query, 'Polish');
        request.handler(response(404, {}));
    });
    loaded.context.translate(bobQuery({ text: 'Polish', originalText: 'Polish' }, () => {}));
    assert.equal(loaded.requests.length, 1);
});

test('Chinese queries are passed through instead of being rejected by language', () => {
    let completion;
    const loaded = load({}, request => {
        assert.equal(request.method, 'POST');
        assert.equal(request.body.query, '放弃');
        request.handler(response(404, {}));
    });
    loaded.context.translate(bobQuery({
        text: '放弃', originalText: '放弃', detectFrom: 'zh-Hans', detectTo: 'en'
    }, value => { completion = value; }));
    assert.equal(loaded.requests.length, 1);
    assert.equal(completion.error.type, 'notFound');
});

test('/list reports an empty registry and transport failure clearly', () => {
    let completion;
    let fail = false;
    const loaded = load({}, request => {
        if (fail) {
            request.handler({ error: { message: 'refused' } });
        } else {
            request.handler(response(200, { directory: '/synthetic/empty', dictionaries: [] }));
        }
    });
    loaded.context.translate(bobQuery({ text: 'list', originalText: '/list' }, value => { completion = value; }));
    assert.equal(completion.result.toParagraphs[0], '未发现 MDict 词典');
    assert.match(completion.result.toParagraphs[1], /\/synthetic\/empty/);

    fail = true;
    loaded.context.translate(bobQuery({ text: 'list', originalText: '/list' }, value => { completion = value; }));
    assert.equal(completion.error.type, 'network');
    assert.match(completion.error.message, /无法连接/);
});

test('invalid configured dictionary is rejected during pluginValidate', () => {
    let completion;
    const loaded = load({ dictionaryID: 'expired-id' }, request => {
        if (request.url.endsWith('/v2/status')) {
            request.handler(response(200, {
                service: 'bob-mdict', apiVersion: 'v2', serviceVersion: '1.0.0', buildCommit: 'service1', healthyDictionaryCount: 1
            }));
            return;
        }
        request.handler(response(200, { dictionaries: [{ id: 'current-id', title: 'Current', health: 'ok' }] }));
    });
    loaded.context.pluginValidate(value => { completion = value; });
    assert.equal(completion.result, false);
    assert.match(completion.error.message, /expired-id/);
    assert.match(completion.error.addition, /\/list/);
    assert.match(loaded.logs[0], /MDict plugin 1\.1\.0-test \(test123\)/);
    assert.match(loaded.logs[0], /bob-mdict 1\.0\.0 \(service1\), API v2/);
});

test('lookup maps invalid ID service errors to actionable guidance', () => {
    let completion;
    const loaded = load({ dictionaryID: 'expired-id' }, request => {
        request.handler(response(404, { error: 'dictionaryNotFound', hint: 'Query /list in Bob.' }));
    });
    loaded.context.translate(bobQuery({ text: 'flimber', originalText: 'flimber' }, value => { completion = value; }));
    assert.match(completion.error.addition, /\/list/);
});

test('record selectors use originalText and send a canonical v2 request', () => {
    const cases = [
        ['foo¹', 'foo', 1],
        ['foo²', 'foo', 2],
        ['foo¹²', 'foo', 12],
        ['foo^1', 'foo', 1],
        ['foo^2', 'foo', 2],
        ['foo^12', 'foo', 12],
        ['foo^{1}', 'foo', 1],
        ['foo^{2}', 'foo', 2],
        ['foo^{12}', 'foo', 12],
        ['China²', 'China', 2],
        ['China^2', 'China', 2],
        ['China^{2}', 'China', 2],
        ['china²', 'china', 2],
        ['Cafe\u0301²', 'Cafe\u0301', 2]
    ];
    for (const [originalText, base, ordinal] of cases) {
        const loaded = load({}, request => {
            assert.equal(request.body.query, base);
            assert.equal(request.body.recordOrdinal, ordinal);
            assert.equal(request.body.multiRecordMode, 'separate');
            request.handler(response(200, { bob: { word: base, parts: [] } }));
        });
        loaded.context.translate(bobQuery({
            text: originalText === 'foo²' ? 'foo2' : originalText,
            originalText
        }, () => {}));
        assert.equal(loaded.requests.length, 1);
    }
});

test('malformed selectors and ordinary digit or caret headwords stay ordinary', () => {
    const values = ['foo2', 'C++', 'x^y', 'H2O', 'foo^', 'foo^{}', 'foo^{x}', 'foo⁰', 'foo^0'];
    for (const value of values) {
        const loaded = load({}, request => {
            assert.equal(request.body.query, value);
            assert.equal('recordOrdinal' in request.body, false);
            request.handler(response(404, {}));
        });
        loaded.context.translate(bobQuery({ text: value, originalText: value }, () => {}));
        assert.equal(loaded.requests.length, 1);
    }

    const missingOriginal = load({}, request => {
        assert.equal(request.body.query, 'foo²');
        assert.equal('recordOrdinal' in request.body, false);
        request.handler(response(404, {}));
    });
    missingOriginal.context.translate(bobQuery({ text: 'foo²' }, () => {}));
});

test('combined preference is explicit while separate remains the default', () => {
    for (const [options, want] of [[{}, 'separate'], [{ multiRecordMode: 'combined' }, 'combined']]) {
        const loaded = load(options, request => {
            assert.equal(request.body.multiRecordMode, want);
            request.handler(response(200, { bob: { word: 'foo', parts: [] } }));
        });
        loaded.context.translate(bobQuery({ text: 'foo', originalText: 'foo' }, () => {}));
    }
});

test('recordNotFound is not confused with a missing selector-shaped headword', () => {
    let completion;
    const loaded = load({}, request => {
        assert.equal(request.body.query, 'foo');
        assert.equal(request.body.recordOrdinal, 3);
        request.handler(response(404, {
            error: 'recordNotFound',
            message: '“foo” 只有 2 个可用词条记录。',
            hint: '请选择第 1 到第 2 条记录。'
        }));
    });
    loaded.context.translate(bobQuery({ text: 'foo3', originalText: 'foo³' }, value => { completion = value; }));
    assert.equal(completion.error.type, 'notFound');
    assert.match(completion.error.message, /只有 2 个/);
    assert.match(completion.error.addition, /第 1 到第 2 条/);
});
