// Prints a short pass/fail list from the JSON report.
import fs from 'node:fs';
const r = JSON.parse(fs.readFileSync(new URL('./playwright-report/results.json', import.meta.url)));
const walk = (s, out = []) => { (s.specs || []).forEach((sp) => out.push(sp)); (s.suites || []).forEach((c) => walk(c, out)); return out; };
let pass = 0, fail = 0;
for (const sp of r.suites.flatMap((s) => walk(s))) {
  const res = sp.tests[0]?.results.at(-1);
  const ok = res?.status === 'passed';
  ok ? pass++ : fail++;
  console.log(`${ok ? 'PASS' : (res?.status || 'skip').toUpperCase()}  ${sp.file}:${sp.line}  ${sp.title}  (${((res?.duration || 0) / 1000).toFixed(1)}s)`);
  if (!ok && res?.error) console.log('      ' + String(res.error.message).replace(/\u001b\[[0-9;]*m/g, '').split('\n').slice(0, 6).join('\n      '));
}
console.log(`\n${pass} passed, ${fail} not passed`);
