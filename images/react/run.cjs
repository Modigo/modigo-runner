/*
 * Runs a React level: compiles the learner's App.jsx with esbuild, renders its
 * default export with the test's props (stdin, as JSON) and prints the HTML.
 * Usage: node /opt/react/run.cjs App.jsx < props.json
 */
'use strict';
const fs = require('fs');
const path = require('path');
const esbuild = require('esbuild');
const React = require('react');
const { renderToStaticMarkup } = require('react-dom/server');

const fail = (message) => {
    process.stderr.write(message.trim() + '\n');
    process.exit(1);
};

const file = path.resolve(process.argv[2] || 'App.jsx');

let code;
try {
    const result = esbuild.buildSync({
        entryPoints: [file],
        bundle: true,
        write: false,
        platform: 'node',
        format: 'cjs',
        jsx: 'automatic',
        loader: { '.js': 'jsx', '.jsx': 'jsx' },
        external: ['react', 'react-dom', 'react/*', 'react-dom/*'],
        logLevel: 'silent',
    });
    code = result.outputFiles[0].text;
} catch (e) {
    const errors = (e.errors || []).map((err) => {
        const at = err.location ? `${path.basename(err.location.file)}:${err.location.line}:${err.location.column}: ` : '';
        return at + err.text;
    });
    fail(errors.length ? errors.join('\n') : String(e.message || e));
}

const mod = { exports: {} };
try {
    new Function('require', 'module', 'exports', code)(require, mod, mod.exports);
} catch (e) {
    fail(e && e.stack ? e.stack : String(e));
}

const exp = mod.exports;
const App = (exp && (exp.default || exp.App)) || (typeof exp === 'function' ? exp : null);
if (typeof App !== 'function' && !(App && typeof App === 'object' && App.$$typeof)) {
    fail('App.jsx must export a component: export default function App(props) { ... }');
}

const raw = fs.readFileSync(0, 'utf8').trim();
let props = {};
if (raw) {
    try {
        props = JSON.parse(raw);
    } catch {
        fail('The test input is not valid JSON props.');
    }
}

try {
    process.stdout.write(renderToStaticMarkup(React.createElement(App, props)) + '\n');
} catch (e) {
    fail(e && e.stack ? e.stack : String(e));
}
