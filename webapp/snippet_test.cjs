const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { test } = require('node:test');
const vm = require('node:vm');

test('README browser API tracks events and records replay chunks', () => {
    const requests = [];
    const scripts = [];
    const intervals = [];
    let emit;
    const sandbox = {
        document: {
            currentScript: { src: 'http://localhost/analytics/e.js' },
            cookie: 'poa_visitor=123',
            createElement: () => ({}),
            head: { appendChild(script) { scripts.push(script); script.onload(); } },
        },
        location: { pathname: '/hello' },
        navigator: {},
        fetch(url, options) {
            requests.push({ url, body: JSON.parse(options.body) });
            return Promise.resolve();
        },
        rrweb: { record(options) { emit = options.emit; } },
        setInterval(callback) { intervals.push(callback); },
        addEventListener() {},
    };
    sandbox.window = sandbox;
    vm.createContext(sandbox);
    const source = readFileSync(__dirname + '/static/e.js', 'utf8');
    vm.runInContext(source, sandbox);

    sandbox.plainoldanalytics.track('signup', { plan: 'pro' });
    assert.deepEqual(requests[0], {
        url: 'http://localhost/analytics/e',
        body: { name: 'signup', props: { plan: 'pro' }, visitor: '123', path: '/hello' },
    });

    sandbox.plainoldanalytics.init_session_recording();
    assert.equal(scripts[0].src, 'http://localhost/analytics/static/vendor/rrweb.min.js');
    emit({ type: 2, timestamp: 1234 });
    intervals[0]();
    assert.deepEqual(requests[1], {
        url: 'http://localhost/analytics/replay',
        body: { visitor: '123', events: [{ type: 2, timestamp: 1234 }] },
    });

    assert.equal(sandbox.poa, sandbox.plainoldanalytics);
    assert.equal(sandbox.poa.init, sandbox.plainoldanalytics.init_session_recording);
    const track = sandbox.plainoldanalytics.track;
    vm.runInContext(source, sandbox);
    assert.equal(sandbox.plainoldanalytics.track, track);
});
