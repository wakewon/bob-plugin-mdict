const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, 'main.js'), 'utf8');

function load(options, respond) {
    const requests = [];
    const context = {
        $option: options || {},
        $http: {
            request(request) {
                requests.push(request);
                respond(request);
            }
        }
    };
    vm.createContext(context);
    vm.runInContext(source, context, { filename: 'main.js' });
    return { context, requests };
}

function bobQuery(text, complete) {
    return {
        text,
        detectFrom: 'en',
        detectTo: 'zh-Hans',
        cancelSignal: { test: true },
        onCompletion: complete
    };
}

function response(statusCode, data) {
    return { response: { statusCode }, data };
}

test('blank ID uses first match and explicit ID restricts lookup', () => {
    for (const [dictionaryID, expected] of [['', undefined], ['abc123', ['abc123']]]) {
        let completion;
        const loaded = load({ dictionaryID }, request => {
            assert.equal(request.url, 'http://127.0.0.1:15321/v1/lookup');
            assert.equal(request.body.limit, 1);
            assert.equal(JSON.stringify(request.body.dictionaries), JSON.stringify(expected));
            request.handler(response(200, { bob: { word: 'flimber', parts: [] } }));
        });
        loaded.context.translate(bobQuery('flimber', value => { completion = value; }));
        assert.equal(completion.result.toDict.word, 'flimber');
        assert.equal('toParagraphs' in completion.result, false);
        assert.equal('fromParagraphs' in completion.result, false);
    }
});

test('exact trimmed /list uses dictionaries endpoint and paragraphs', () => {
    let completion;
    const loaded = load({}, request => {
        assert.equal(request.method, 'GET');
        assert.equal(request.url, 'http://127.0.0.1:15321/v1/dictionaries');
        assert.equal(request.cancelSignal.test, true);
        request.handler(response(200, {
            directory: '/synthetic/dictionaries',
            dictionaries: [
                { id: 'abc123', title: 'Synthetic Learner Dictionary', health: 'ok' },
                { id: 'broken1', title: 'Broken Synthetic Dictionary', health: 'unavailable', diagnostics: ['test diagnostic'] }
            ]
        }));
    });
    loaded.context.translate(bobQuery('  /list  ', value => { completion = value; }));
    assert.equal(completion.result.toParagraphs[0], 'MDict dictionaries');
    assert.match(completion.result.toParagraphs[1], /Synthetic Learner Dictionary/);
    assert.match(completion.result.toParagraphs[1], /ID: abc123/);
    assert.match(completion.result.toParagraphs[2], /状态：不可用/);
    assert.match(completion.result.toParagraphs[2], /test diagnostic/);
    assert.equal('toDict' in completion.result, false);
});

test('ordinary list remains a dictionary lookup', () => {
    const loaded = load({}, request => {
        assert.equal(request.method, 'POST');
        assert.equal(request.body.query, 'list');
        request.handler(response(404, {}));
    });
    loaded.context.translate(bobQuery('list', () => {}));
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
    loaded.context.translate(bobQuery('/list', value => { completion = value; }));
    assert.equal(completion.result.toParagraphs[0], '未发现 MDict 词典');
    assert.match(completion.result.toParagraphs[1], /\/synthetic\/empty/);

    fail = true;
    loaded.context.translate(bobQuery('/list', value => { completion = value; }));
    assert.equal(completion.error.type, 'network');
    assert.match(completion.error.message, /无法连接/);
});

test('invalid configured dictionary is rejected during pluginValidate', () => {
    let completion;
    const loaded = load({ dictionaryID: 'expired-id' }, request => {
        if (request.url.endsWith('/v1/status')) {
            request.handler(response(200, {
                service: 'bob-mdict', apiVersion: 'v1', serviceVersion: '0.1.0', healthyDictionaryCount: 1
            }));
            return;
        }
        request.handler(response(200, { dictionaries: [{ id: 'current-id', title: 'Current', health: 'ok' }] }));
    });
    loaded.context.pluginValidate(value => { completion = value; });
    assert.equal(completion.result, false);
    assert.match(completion.error.message, /expired-id/);
    assert.match(completion.error.addition, /\/list/);
});

test('lookup maps invalid ID service errors to actionable guidance', () => {
    let completion;
    const loaded = load({ dictionaryID: 'expired-id' }, request => {
        request.handler(response(404, { error: 'dictionaryNotFound', hint: 'Query /list in Bob.' }));
    });
    loaded.context.translate(bobQuery('flimber', value => { completion = value; }));
    assert.match(completion.error.addition, /\/list/);
});
