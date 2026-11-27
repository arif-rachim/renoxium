// Every builder tool's structured result matches the output schema it
// declares, checked by the MCP TypeScript SDK the way Claude.ai's
// connector checks it: listTools first (which loads the schemas), then
// each call. A mismatch fails the call with "Structured content does not
// match the tool's output schema".
import { test, expect } from '@playwright/test';
import { person, signUp, email, unique, connectAgent } from '../lib.mjs';
import { RENOXIUM } from '../env.mjs';
import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { StreamableHTTPClientTransport } from '@modelcontextprotocol/sdk/client/streamableHttp.js';

const PNG = 'iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAEElEQVR4nGNocFAAIgYIBQAaDgOBnA45xwAAAABJRU5ErkJggg==';

test('structured results match the declared output schemas', async ({ browser }) => {
  test.setTimeout(180_000);
  const { page } = await person(browser);
  await signUp(page, email('schemas'));
  const { access_token: token } = await connectAgent(page);
  const client = new Client({ name: 'renoxium-e2e-schemas', version: '1.0.0' });
  await client.connect(new StreamableHTTPClientTransport(new URL(RENOXIUM + '/mcp'), { requestInit: { headers: { Authorization: 'Bearer ' + token } } }));
  const { tools } = await client.listTools(); // loads every output schema into the client's validator
  const declared = new Set(tools.filter((t) => t.outputSchema).map((t) => t.name));
  const called = new Set();
  const failures = [];
  const call = async (name, args) => {
    called.add(name);
    try {
      const r = await client.callTool({ name, arguments: args });
      if (r.isError) return { error: r.content.map((c) => c.text).join(' ') };
      return r.structuredContent;
    } catch (e) {
      failures.push(`${name}: ${e.message}`);
      return {};
    }
  };

  // A free account has 3 apps: a shop, a booking site with every test tool, and a from-scratch app.
  for (const ex of ['store-uae', 'booking-uae']) {
    const app = unique('sc');
    const created = await call('create_app', { app, from_example: ex, business: 'Test Business', check: true });
    expect(created.error, ex).toBeUndefined();
    await call('get_app', { app });
    await call('get_app_outline', { app });
    await call('get_app_parts', { app, paths: ['routes', 'tables'] });
    await call('view_page', { app, path: '/' });
    await call('view_live_page', { app, path: '/' });
    await call('screenshot_preview', { app, path: '/' });
    await call('try_scheduled_jobs', { app });
    await call('get_logs', { app });
    if (ex === 'booking-uae') {
      await call('change_preview_row', { app, action: 'update', table: 'services', id: 1, values: { name: 'Changed' } });
      await call('change_preview_row', { app, action: 'delete', table: 'staff', id: 1 });
      await call('publish', { app });
      await call('publish', { app, discard_preview_changes: true });
      const v = await call('create_test_visitor', { app, name: 'Test Visitor' });
      await call('submit_form', { app, path: '/', form: {}, follow_redirects: true });
      await call('upload_as_visitor', { app, path: '/', field: 'photo', file_name: 'a.png', data_base64: PNG, as_visitor: v.email });
      const file = await call('upload_file', { app, data_base64: PNG });
      await call('clear_test_rows', { app });
      // Every optional part of the outline at once: a file used by a preview row, unsaved preview rows.
      await call('change_preview_row', { app, action: 'update', table: 'services', id: 2, values: { image: file.url } });
      await call('get_app', { app });
      const outline = await call('get_app_outline', { app });
      expect(outline.files?.[0]?.used_in?.length, 'outline lists the file use').toBeGreaterThan(0);
      expect(outline.unsaved_preview_changes?.length).toBeGreaterThan(0);
      await call('edit_app', { app, base_version: outline.version, edits: [{ target: 'templates.css_extra', old_text: '', new_text: '<style>:root{--x:1}</style>' }] });
      await call('patch_app', { app, base_version: outline.version + 1, templates: { css_extra: '<style>:root{--x:2}</style>' } });
      await call('rename_app', { app, new_name: app + '-x' });
      await call('delete_app', { app });
    }
  }
  await call('list_apps', {});
  await call('get_example', {});
  await call('get_example', { name: 'tasks' });
  await call('get_example', { reference_only: true });
  const apply = unique('sc');
  await call('create_app', { app: apply });
  await call('apply_change', { app: apply, definition: { routes: [{ method: 'GET', path: '/', template: 'home' }], templates: { home: '<p>hi</p>' } } });

  expect(failures).toEqual([]);
  // Every tool with an output schema was called at least once.
  expect([...declared].filter((n) => !called.has(n))).toEqual([]);
  await client.close();
});
