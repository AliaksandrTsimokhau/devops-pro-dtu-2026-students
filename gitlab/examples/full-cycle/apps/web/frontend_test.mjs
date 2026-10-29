import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import vm from 'node:vm';

const source = await readFile(new URL('./web/app.js', import.meta.url), 'utf8');

async function page(fetch) {
  const elements = Object.fromEntries(['status', 'refresh', 'error', 'pipeline', 'version', 'environment', 'release', 'hostname', 'checked'].map(id => [id, {
    textContent: '', hidden: true, disabled: false,
    addEventListener(event, handler) { this[event] = handler; },
  }]));
  await vm.runInNewContext(source, {
    document: { querySelector: selector => elements[selector.slice(1)], getElementById: id => elements[id] },
    fetch, AbortSignal, URL, Date,
  });
  return elements;
}

const release = { version: 'abc123', environment: 'student', release: '0.1.2', hostname: 'web-1', pipelineUrl: 'https://gitlab.example.com/project/-/pipelines/1' };

test('shows the deployed release and a usable pipeline link', async () => {
  const elements = await page(async () => ({ ok: true, json: async () => release }));
  assert.equal(elements.status.textContent, 'Online');
  assert.equal(elements.version.textContent, 'abc123');
  assert.equal(elements.refresh.disabled, false);
  assert.equal(elements.pipeline.hidden, false);
  assert.equal(elements.pipeline.href, release.pipelineUrl);
});

test('a network failure preserves the last release and lets the user retry', async () => {
  let fail = false;
  const elements = await page(async () => {
    if (fail) throw new Error('Network unavailable');
    return { ok: true, json: async () => release };
  });
  fail = true;
  await elements.refresh.click();
  assert.equal(elements.status.textContent, 'Unavailable');
  assert.equal(elements.version.textContent, 'abc123');
  assert.match(elements.error.textContent, /Network unavailable/);
  assert.equal(elements.refresh.disabled, false);
});

test('rejects a broken API contract and hides unsafe pipeline links', async () => {
  const broken = await page(async () => ({ ok: true, json: async () => ({}) }));
  assert.equal(broken.status.textContent, 'Unavailable');
  const unsafe = await page(async () => ({ ok: true, json: async () => ({ ...release, pipelineUrl: 'javascript:alert(1)' }) }));
  assert.equal(unsafe.pipeline.hidden, true);
});
