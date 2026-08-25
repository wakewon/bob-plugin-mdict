const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, 'main.js'), 'utf8')
    .replace('__BOB_MDICT_PLUGIN_VERSION__', '0.2.0-test')
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
            assert.equal(JSON.stringify(request.body.dictionaries), JSON.stringify(expected));
            request.handler(response(200, { bob: { word: 'flimber', parts: [] } }));
        });
        loaded.context.translate(bobQuery({ text: 'flimber', originalText: 'flimber' }, value => { completion = value; }));
        assert.equal(completion.result.toDict.word, 'flimber');
        assert.equal('toParagraphs' in completion.result, false);
        assert.equal('fromParagraphs' in completion.result, false);
    }
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
                service: 'bob-mdict', apiVersion: 'v2', serviceVersion: '0.2.0', buildCommit: 'service1', healthyDictionaryCount: 1
            }));
            return;
        }
        request.handler(response(200, { dictionaries: [{ id: 'current-id', title: 'Current', health: 'ok' }] }));
    });
    loaded.context.pluginValidate(value => { completion = value; });
    assert.equal(completion.result, false);
    assert.match(completion.error.message, /expired-id/);
    assert.match(completion.error.addition, /\/list/);
    assert.match(loaded.logs[0], /MDict plugin 0\.2\.0-test \(test123\)/);
    assert.match(loaded.logs[0], /bob-mdict 0\.2\.0 \(service1\), API v2/);
});

test('lookup maps invalid ID service errors to actionable guidance', () => {
    let completion;
    const loaded = load({ dictionaryID: 'expired-id' }, request => {
        request.handler(response(404, { error: 'dictionaryNotFound', hint: 'Query /list in Bob.' }));
    });
    loaded.context.translate(bobQuery({ text: 'flimber', originalText: 'flimber' }, value => { completion = value; }));
    assert.match(completion.error.addition, /\/list/);
});
