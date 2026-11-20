// DeepORG ASCII scenes.
//
// Every scene is a pure function of (grid, seconds, data). The same code draws
// the animated frames in the browser and the still frames Hugo inlines before
// JavaScript runs (scripts/render-ascii-stills.mjs), so the two cannot drift.
//
// Scenes use 7-bit ASCII only. Box-drawing and block glyphs fall back to a
// different font on some systems and break the column grid.
(() => {
  const GHOST = 0; // the drifting background field
  const SOFT = 1; // rules, idle edges, captions
  const ACCENT = 2; // what is happening now
  const WARM = 3; // refusals, failures, things held
  const BRIGHT = 4; // the words to read first
  const TEXT = 5; // everything else (inherits the <pre> colour)

  const clamp = (v, lo = 0, hi = 1) => Math.max(lo, Math.min(hi, v));
  const seg = (t, a, b) => clamp((t - a) / (b - a));
  const pad = (n, w) => String(n).padStart(w, '0');

  // Copy arrives from page data written for prose; fold it to plain ASCII.
  const ascii = (s) => String(s ?? '')
    .replace(/[—–]/g, ' - ')
    .replace(/·/g, '/')
    .replace(/[‘’]/g, "'")
    .replace(/[“”]/g, '"')
    .replace(/→/g, '->')
    .replace(/[^\x20-\x7e]/g, '')
    .replace(/\s+/g, ' ')
    .trim();

  function wrap(text, width) {
    const lines = [];
    let line = '';
    for (const word of text.split(' ')) {
      if (line && (line + ' ' + word).length > width) {
        lines.push(line);
        line = word;
      } else {
        line = line ? line + ' ' + word : word;
      }
    }
    if (line) lines.push(line);
    return lines;
  }

  class Grid {
    constructor(cols, rows) {
      this.cols = cols;
      this.rows = rows;
      this.ch = new Array(cols * rows).fill(' ');
      this.tone = new Uint8Array(cols * rows).fill(TEXT);
      this.used = new Uint8Array(cols * rows);
    }

    set(x, y, c, tone = TEXT) {
      if (x < 0 || y < 0 || x >= this.cols || y >= this.rows) return;
      const i = y * this.cols + x;
      this.ch[i] = c;
      this.tone[i] = tone;
      this.used[i] = 1;
    }

    text(x, y, s, tone = TEXT) {
      for (let i = 0; i < s.length; i++) this.set(x + i, y, s[i], tone);
    }

    hline(x0, x1, y, c = '-', tone = SOFT) {
      for (let x = x0; x <= x1; x++) this.set(x, y, c, tone);
    }

    vline(x, y0, y1, c = '|', tone = SOFT) {
      for (let y = y0; y <= y1; y++) this.set(x, y, c, tone);
    }

    box(x, y, w, h, tone = SOFT) {
      this.hline(x, x + w - 1, y, '-', tone);
      this.hline(x, x + w - 1, y + h - 1, '-', tone);
      this.vline(x, y, y + h - 1, '|', tone);
      this.vline(x + w - 1, y, y + h - 1, '|', tone);
      for (const [cx, cy] of [[x, y], [x + w - 1, y], [x, y + h - 1], [x + w - 1, y + h - 1]]) {
        this.set(cx, cy, '+', tone);
      }
    }

    // Keep the background field out of a region without drawing in it.
    reserve(x, y, w, h) {
      for (let yy = y; yy < y + h; yy++) {
        for (let xx = x; xx < x + w; xx++) {
          if (xx >= 0 && yy >= 0 && xx < this.cols && yy < this.rows) this.used[yy * this.cols + xx] = 1;
        }
      }
    }

    ghost() {
      this.tone.fill(GHOST);
    }

    toText() {
      const lines = [];
      for (let y = 0; y < this.rows; y++) {
        lines.push(this.ch.slice(y * this.cols, (y + 1) * this.cols).join('').replace(/\s+$/, ''));
      }
      return lines.join('\n');
    }

    toHTML() {
      let out = '';
      for (let y = 0; y < this.rows; y++) {
        let run = '';
        let tone = TEXT;
        const flush = () => {
          if (!run) return;
          const safe = run.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
          out += tone === TEXT || !run.trim() ? safe : `<span class="t${tone}">${safe}</span>`;
          run = '';
        };
        for (let x = 0; x < this.cols; x++) {
          const i = y * this.cols + x;
          const next = this.ch[i] === ' ' ? tone : this.tone[i];
          if (next !== tone) {
            flush();
            tone = next;
          }
          run += this.ch[i];
        }
        flush();
        if (y < this.rows - 1) out += '\n';
      }
      return out;
    }
  }

  // A slow plasma in the cells nothing else claimed.
  const FIELD = ' ..::-=+';
  function field(g, t, amount = 1) {
    for (let y = 0; y < g.rows; y++) {
      for (let x = 0; x < g.cols; x++) {
        const i = y * g.cols + x;
        let near = false;
        for (let d = -2; d <= 2 && !near; d++) {
          if (x + d >= 0 && x + d < g.cols && g.used[i + d]) near = true;
        }
        if (near) continue;
        const v = Math.sin(x * 0.17 + t * 0.35) +
          Math.sin(y * 0.41 - t * 0.27) +
          Math.sin((x + y * 2) * 0.11 + t * 0.19) +
          Math.sin(Math.hypot(x - g.cols * 0.72, (y - g.rows * 0.38) * 2.2) * 0.21 - t * 0.5);
        const k = Math.floor(clamp(((v + 4) / 8 - 0.5) * 2.6 * amount) * (FIELD.length - 1));
        if (k > 0) {
          g.ch[i] = FIELD[k];
          g.tone[i] = GHOST;
        }
      }
    }
  }

  // Type lines one after another. Returns the time the last one finishes.
  function script(g, lines, t, { cps = 70, gap = 0.22 } = {}) {
    let at = 0;
    for (const line of lines) {
      const start = line.at ?? at;
      const dur = line.text.length / (line.cps ?? cps);
      const p = seg(t, start, start + dur);
      if (p > 0) {
        g.text(line.x, line.y, line.text.slice(0, Math.ceil(line.text.length * p)), line.tone ?? TEXT);
      }
      at = start + dur + (line.gap ?? gap);
    }
    return at;
  }

  const scenes = {};

  // --- org: one question, several systems, one answer (homepage hero) -------

  const ORG_SOURCES = ['postgres', 'snowflake', 'mysql', 'crm api', 'ticket api', 'files', 'code'];
  const ORG_FALLBACK = [{
    user: 'Which account is most at risk of churn - and why?',
    sources: [
      { name: 'Postgres', detail: 'accounts + invoices' },
      { name: 'Snowflake', detail: 'product usage' },
      { name: 'CRM API', detail: 'opportunities' },
    ],
    done: 'Evidence checked / 3 systems / 3 operations',
    answer: 'Meridian Robotics - renewal in 9 days, usage down 38%, two failed payments, and an unresolved escalation.',
  }];
  const orgKey = (name) => {
    const key = ascii(name).toLowerCase();
    return key === 'filesystem' ? 'files' : key;
  };

  scenes.org = {
    cols: 62,
    rows: 18,
    poster: 10,
    draw(g, time, data) {
      const items = Array.isArray(data) && data.length ? data : ORG_FALLBACK;
      const T = 12;
      const item = items[Math.floor(time / T) % items.length];
      const t = time % T;

      // The question.
      const question = ascii(item.user);
      const typed = question.slice(0, Math.floor(question.length * seg(t, 0.3, 1.9)));
      g.reserve(0, 1, g.cols, 1);
      g.text(2, 1, 'ask >', ACCENT);
      g.text(8, 1, typed, BRIGHT);
      if (t < 2.4 && Math.floor(t * 3) % 2 === 0) g.set(8 + typed.length, 1, '_', ACCENT);

      // The systems it could reach, and the ones this question needs.
      const active = (item.sources || []).map((s) => ({ key: orgKey(s.name), detail: ascii(s.detail) }));
      const nodeX = 14;
      const trunkX = 42;
      const top = 3;
      let arrived = 0;
      g.reserve(0, top, nodeX + 1, ORG_SOURCES.length);
      ORG_SOURCES.forEach((name, i) => {
        const y = top + i;
        const order = active.findIndex((a) => a.key === name);
        const startAt = 2.2 + order * 0.75;
        const on = order >= 0 ? seg(t, startAt, startAt + 0.6) : 0;
        g.text(2, y, name, on > 0 ? BRIGHT : SOFT);
        g.set(nodeX, y, 'o', on > 0 ? ACCENT : SOFT);
        g.hline(nodeX + 1, trunkX - 1, y, '-', on > 0 ? ACCENT : SOFT);
        if (on > 0) {
          const detail = active[order].detail;
          g.text(nodeX + 4, y, ' ' + detail.slice(0, Math.ceil(detail.length * on)) + ' ', TEXT);
          const run = seg(t, startAt + 0.6, startAt + 1.2);
          if (run > 0 && run < 1) g.set(Math.round(nodeX + 1 + run * (trunkX - nodeX - 2)), y, '*', WARM);
          if (run >= 1) arrived += 1;
        }
        g.set(trunkX, y, '+', on >= 1 ? ACCENT : SOFT);
      });

      // DeepORG in the middle.
      const coreAt = 2.2 + Math.max(active.length - 1, 0) * 0.75 + 1.2;
      const coreOn = t >= coreAt;
      const mid = top + 3;
      g.text(trunkX + 1, mid, '-->', coreOn ? ACCENT : SOFT);
      g.box(46, mid - 1, 14, 3, coreOn ? ACCENT : SOFT);
      g.text(49, mid, 'DeepORG', coreOn ? BRIGHT : TEXT);
      const linkRun = seg(t, coreAt - 0.3, coreAt);
      if (arrived > 0 && linkRun > 0 && linkRun < 1) g.set(trunkX + 1 + Math.round(linkRun * 2), mid, '*', WARM);

      // What it did before it answered.
      g.reserve(trunkX + 2, mid + 2, g.cols - trunkX - 2, 3);
      const steps = ['rules ok', `${active.length} queries`, 'evidence'];
      steps.forEach((label, i) => {
        const at = coreAt + 0.4 + i * 0.45;
        if (t < at) return;
        g.set(48, mid + 2 + i, '|', ACCENT);
        g.text(50, mid + 2 + i, label, TEXT);
      });
      const answerAt = coreAt + 0.4 + steps.length * 0.45 + 0.2;
      g.set(48, mid + 1, '+', coreOn ? ACCENT : SOFT);

      // The answer, and what it rests on.
      const ruleY = mid + 5;
      g.reserve(0, ruleY, g.cols, 6);
      g.hline(2, 59, ruleY, '-', SOFT);
      g.text(4, ruleY, ' answer ', t >= answerAt ? ACCENT : SOFT);
      if (t >= answerAt) g.set(48, ruleY, '+', ACCENT);
      const lines = wrap(ascii(item.answer), 58);
      const total = lines.join(' ').length;
      let budget = Math.floor(total * seg(t, answerAt + 0.2, answerAt + 2.2));
      lines.forEach((line, i) => {
        const shown = line.slice(0, Math.max(0, budget));
        budget -= line.length + 1;
        if (shown) g.text(2, ruleY + 1 + i, shown, BRIGHT);
      });
      if (t >= answerAt + 2.5) {
        g.text(2, ruleY + 5, '[ok]', ACCENT);
        g.text(7, ruleY + 5, ascii(item.done).toLowerCase(), TEXT);
      }

      field(g, time);
      if (t > T - 0.45) g.ghost();
    },
  };

  // --- orgwide: the same story read left to right, for a full-width stage ---
  //
  // Systems on the left, DeepORG in the middle, the answer on the right. Wide
  // screens show this one; narrow screens keep the stacked `org` above.

  scenes.orgwide = {
    cols: 124,
    rows: 16,
    poster: 10,
    draw(g, time, data) {
      const items = Array.isArray(data) && data.length ? data : ORG_FALLBACK;
      const T = 12;
      const item = items[Math.floor(time / T) % items.length];
      const t = time % T;

      const askY = 2;
      const top = 4; // first system row
      const mid = top + 3; // the row everything flows along
      const nodeX = 12;
      const trunkX = 46;
      const boxX = 59;
      const boxW = 18;
      const outX = boxX + boxW;
      const ansX = 90;
      const ansW = g.cols - ansX;

      // The question.
      const question = ascii(item.user);
      const typed = question.slice(0, Math.floor(question.length * seg(t, 0.3, 1.9)));
      g.reserve(0, askY, g.cols, 2);
      g.text(0, askY, 'ask >', ACCENT);
      g.text(6, askY, typed, BRIGHT);
      if (t < 2.4 && Math.floor(t * 3) % 2 === 0) g.set(6 + typed.length, askY, '_', ACCENT);

      // The systems it could reach, and the ones this question needs.
      const active = (item.sources || []).map((s) => ({ key: orgKey(s.name), detail: ascii(s.detail) }));
      let arrived = 0;
      g.reserve(0, top, g.cols, ORG_SOURCES.length);
      ORG_SOURCES.forEach((name, i) => {
        const y = top + i;
        const order = active.findIndex((a) => a.key === name);
        const startAt = 2.2 + order * 0.75;
        const on = order >= 0 ? seg(t, startAt, startAt + 0.6) : 0;
        g.text(0, y, name, on > 0 ? BRIGHT : SOFT);
        g.set(nodeX, y, 'o', on > 0 ? ACCENT : SOFT);
        g.hline(nodeX + 1, trunkX - 1, y, '-', on > 0 ? ACCENT : SOFT);
        if (on > 0) {
          const detail = active[order].detail;
          g.text(nodeX + 4, y, ' ' + detail.slice(0, Math.ceil(detail.length * on)) + ' ', TEXT);
          const run = seg(t, startAt + 0.6, startAt + 1.2);
          if (run > 0 && run < 1) g.set(Math.round(nodeX + 1 + run * (trunkX - nodeX - 2)), y, '*', WARM);
          if (run >= 1) arrived += 1;
        }
        g.set(trunkX, y, '+', on >= 1 ? ACCENT : SOFT);
      });

      // DeepORG in the middle.
      const coreAt = 2.2 + Math.max(active.length - 1, 0) * 0.75 + 1.2;
      const coreOn = t >= coreAt;
      const coreTone = coreOn ? ACCENT : SOFT;
      g.hline(trunkX + 1, boxX - 3, mid, '-', coreTone);
      g.set(boxX - 2, mid, '>', coreTone);
      g.reserve(boxX, mid - 2, boxW, 5);
      g.box(boxX, mid - 2, boxW, 5, coreTone);
      g.text(boxX + Math.floor((boxW - 7) / 2), mid, 'DeepORG', coreOn ? BRIGHT : TEXT);
      const linkRun = seg(t, coreAt - 0.35, coreAt);
      if (arrived > 0 && linkRun > 0 && linkRun < 1) {
        g.set(trunkX + 1 + Math.round(linkRun * (boxX - trunkX - 4)), mid, '*', WARM);
      }

      // What it did before it answered.
      const stepX = boxX + 3;
      g.reserve(boxX, mid + 3, boxW + 2, 3);
      const steps = ['rules ok', `${active.length} queries`, 'evidence'];
      steps.forEach((label, i) => {
        const at = coreAt + 0.4 + i * 0.45;
        if (t < at) return;
        g.set(stepX, mid + 3 + i, '|', ACCENT);
        g.text(stepX + 2, mid + 3 + i, label, TEXT);
      });
      g.set(stepX, mid + 2, '+', coreTone);
      const answerAt = coreAt + 0.4 + steps.length * 0.45 + 0.2;

      // The answer, and what it rests on.
      const answerOn = t >= answerAt;
      const outTone = answerOn ? ACCENT : SOFT;
      g.hline(outX + 1, ansX - 4, mid, '-', outTone);
      g.set(ansX - 3, mid, '>', outTone);
      const outRun = seg(t, answerAt - 0.35, answerAt);
      if (outRun > 0 && outRun < 1) g.set(outX + 1 + Math.round(outRun * (ansX - outX - 5)), mid, '*', WARM);

      g.reserve(outX, top, g.cols - outX, 9);
      g.hline(ansX, g.cols - 1, top, '-', SOFT);
      g.text(ansX + 2, top, ' answer ', outTone);
      const lines = wrap(ascii(item.answer), ansW);
      const total = lines.join(' ').length;
      let budget = Math.floor(total * seg(t, answerAt + 0.2, answerAt + 2.2));
      lines.forEach((line, i) => {
        const shown = line.slice(0, Math.max(0, budget));
        budget -= line.length + 1;
        if (shown) g.text(ansX, top + 1 + i, shown, BRIGHT);
      });
      if (t >= answerAt + 2.5) {
        const [first, ...others] = ascii(item.done).toLowerCase().split(' / ');
        g.text(ansX, top + 6, '[ok]', ACCENT);
        g.text(ansX + 5, top + 6, first, TEXT);
        if (others.length) g.text(ansX + 5, top + 7, others.join(' / '), SOFT);
      }

      field(g, time);
      if (t > T - 0.45) g.ghost();
    },
  };

  // --- map: a tool for each thing, or a map of everything -------------------

  const MAP_TOOLS = [
    'get_customer', 'list_invoices', 'get_ticket_by_id', 'search_orders', 'update_ticket',
    'list_renewals', 'get_usage_by_day', 'find_account', 'refund_invoice', 'list_open_risks',
    'get_plan', 'search_users',
  ];
  const MAP_NODES = {
    accounts: [31, 3], invoices: [47, 3], tickets: [31, 6], usage: [40, 6],
    payments: [47, 6], 'sla.md': [31, 8], 'crm api': [40, 8],
  };
  const MAP_EDGES = {
    'accounts-invoices': [[40, 3, '-'], [41, 3, '-'], [42, 3, '-'], [43, 3, '-'], [44, 3, '-'], [45, 3, '-']],
    'accounts-tickets': [[34, 4, '|'], [34, 5, '|']],
    'accounts-usage': [[38, 4, '\\'], [39, 5, '\\']],
    'invoices-payments': [[50, 4, '|'], [50, 5, '|']],
    'tickets-sla.md': [[34, 7, '|']],
    'usage-crm api': [[42, 7, '|']],
  };
  const MAP_PATHS = [
    ['accounts', 'invoices', 'payments'],
    ['accounts', 'tickets', 'sla.md'],
    ['accounts', 'usage', 'crm api'],
  ];

  scenes.map = {
    cols: 62,
    rows: 14,
    poster: 3.6,
    draw(g, time) {
      const T = 12;
      const t = time % T;

      // Left: every question is a tool somebody wrote.
      g.text(2, 1, 'a tool for each thing', SOFT);
      const count = Math.min(MAP_TOOLS.length, 4 + Math.floor(t / 0.75));
      const first = Math.max(0, count - 7);
      let broken = 0;
      for (let i = first; i < count; i++) {
        const stale = i % 3 === 1 && t > (i - 3) * 0.75 + 2.4;
        if (stale) broken += 1;
        g.text(2, 3 + i - first, stale ? `x ${MAP_TOOLS[i]}  stale` : `[${MAP_TOOLS[i]}]`, stale ? WARM : TEXT);
      }
      g.text(2, 11, `to maintain: ${count}`, TEXT);
      if (broken) g.text(2, 12, `gone stale: ${broken}`, WARM);

      g.vline(28, 1, 12, '|', SOFT);

      // Right: one map, and a path the agent finds for itself.
      g.text(31, 1, 'a map of everything', SOFT);
      const path = MAP_PATHS[Math.floor(t / 4) % MAP_PATHS.length];
      const step = (t % 4) / 0.7;
      for (const [key, cells] of Object.entries(MAP_EDGES)) {
        const [a, b] = key.split('-');
        const at = path.indexOf(b);
        const lit = at > 0 && path[at - 1] === a && step >= at;
        for (const [x, y, c] of cells) g.set(x, y, c, lit ? ACCENT : SOFT);
      }
      for (const [name, [x, y]] of Object.entries(MAP_NODES)) {
        const at = path.indexOf(name);
        g.text(x, y, name, at >= 0 && step >= at ? BRIGHT : TEXT);
      }
      const trail = path.slice(0, clamp(Math.floor(step) + 1, 1, path.length)).join(' > ');
      g.text(31, 10, 'agent wrote:', SOFT);
      g.text(31, 11, trail, ACCENT);
      g.text(31, 12, 'to maintain: 0', TEXT);
    },
  };

  // --- ledger: a claim with nothing behind it does not get out --------------

  scenes.ledger = {
    cols: 56,
    rows: 14,
    poster: 11,
    draw(g, time) {
      const t = time % 14;
      g.text(2, 0, 'run ledger', SOFT);
      g.text(24, 0, 'checked before an answer leaves', SOFT);
      g.hline(2, 53, 1, '-', SOFT);
      const end = script(g, [
        { x: 2, y: 2, text: 'claim    "Hold roast batches 14 and 15."', tone: BRIGHT, at: 0.3 },
        { x: 2, y: 3, text: 'queries  0 ran' },
        { x: 2, y: 4, text: 'rows     none' },
        { x: 2, y: 5, text: 'verdict  BLOCKED - nothing behind the claim', tone: WARM, gap: 0.9 },
        { x: 2, y: 7, text: 'claim    "Meridian Robotics is most at risk."', tone: BRIGHT },
        { x: 2, y: 8, text: 'queries  3 ran' },
        { x: 2, y: 9, text: 'rows     accounts 1 / invoices 2 / tickets 1' },
        { x: 2, y: 10, text: 'verdict  ANSWERED - evidence attached', tone: ACCENT },
      ], t);
      g.hline(2, 53, 11, '-', SOFT);
      if (t > end + 0.3) g.text(2, 12, 'only real tool results count', TEXT);
    },
  };

  // --- gate: rules, a rehearsal, then your yes ------------------------------

  scenes.gate = {
    cols: 56,
    rows: 12,
    poster: 10.5,
    draw(g, time) {
      const t = time % 13;
      g.text(2, 0, 'agent proposes', SOFT);
      g.text(18, 0, 'update 3 rows in support_tickets', BRIGHT);
      const steps = [
        { at: 1.2, name: 'rules  ', text: 'your role may change these rows', mark: 'ok' },
        { at: 3.0, name: 'dry run', text: 'tried in a transaction, rolled back', mark: 'ok' },
        { at: 4.8, name: 'held   ', text: '', mark: '' },
        { at: 8.6, name: 'run    ', text: 'that exact query, once, as you', mark: 'done' },
      ];
      steps.forEach((step, i) => {
        const y = 2 + i * 2;
        const on = t >= step.at;
        g.set(6, y - 1, '|', on ? ACCENT : SOFT);
        g.text(2, y, `[ ${step.name} ]`, on ? ACCENT : SOFT);
        if (!on) return;
        if (i === 2) {
          const approved = t >= 7.6;
          const dots = '. '.repeat(1 + (Math.floor((t - step.at) * 3) % 6));
          g.text(14, y, approved ? 'you looked at what it would change' : `waiting for you ${dots}`, approved ? TEXT : WARM);
          g.text(50, y, approved ? 'yes' : '?', approved ? ACCENT : WARM);
          return;
        }
        g.text(14, y, step.text.slice(0, Math.ceil(step.text.length * seg(t, step.at, step.at + 0.6))), TEXT);
        if (t >= step.at + 0.7) g.text(50, y, step.mark, ACCENT);
      });
      if (t >= 9.8) g.text(2, 10, 'nothing changed until you said yes', TEXT);
    },
  };

  // --- env: reset, act, and let the database mark it ------------------------

  const ENV_EPISODES = [
    { answer: '"There are 4 open support tickets."', value: '4', work: '1 query, computed by the database', reward: '1.00', note: 'matches / method ok / nothing else moved', ok: true },
    { answer: '"Roughly 6 tickets are open right now."', value: '6', work: '1 query, computed by the database', reward: '0.00', note: 'fluent, confident, wrong', ok: false },
    { answer: '"4 tickets are open."', value: '4', work: '1 query, computed by the database', reward: '1.00', note: 'matches / method ok / nothing else moved', ok: true },
    { answer: '"There are 4 open support tickets."', value: '4', work: '1 query, computed by the database', reward: '1.00', note: 'matches / method ok / nothing else moved', ok: true },
    { answer: '"4"', value: '4', work: 'no query ran', reward: '0.00', note: 'right number, no work behind it', ok: false },
  ];
  const ENV_STAGES = [['reset', 2], ['task', 12], ['agent works', 21], ['answer', 37], ['oracle', 48]];

  scenes.env = {
    cols: 58,
    rows: 15,
    poster: 4.4,
    draw(g, time) {
      const T = 5;
      const n = Math.floor(time / T);
      const t = time % T;
      const ep = ENV_EPISODES[n % ENV_EPISODES.length];
      const stage = Math.min(5, Math.floor(t / 0.55));

      g.text(2, 0, `episode ${pad(412 + n, 4)}`, TEXT);
      g.text(19, 0, `world ${(n % 2) + 1} of 2`, SOFT);
      g.text(34, 0, 'marked by the database', SOFT);
      g.hline(2, 55, 1, '-', SOFT);

      ENV_STAGES.forEach(([label, x], i) => {
        g.text(x, 3, label, stage === i ? BRIGHT : stage > i ? TEXT : SOFT);
        if (i < ENV_STAGES.length - 1) {
          const next = ENV_STAGES[i + 1][1];
          g.text(x + label.length + 1, 3, '-'.repeat(Math.max(1, next - x - label.length - 3)) + '>', stage > i ? ACCENT : SOFT);
        }
      });
      const back = stage >= 5;
      g.set(4, 4, '^', back ? ACCENT : SOFT);
      g.set(50, 4, '|', back ? ACCENT : SOFT);
      g.set(4, 5, '+', back ? ACCENT : SOFT);
      g.set(50, 5, '+', back ? ACCENT : SOFT);
      g.hline(5, 49, 5, '-', back ? ACCENT : SOFT);
      g.text(23, 5, ' reward ', back ? (ep.ok ? ACCENT : WARM) : SOFT);

      if (stage >= 1) g.text(2, 7, 'task    "How many support tickets are open?"', TEXT);
      if (stage >= 2) {
        g.text(2, 8, 'work', TEXT);
        g.text(10, 8, ep.work, ep.work === 'no query ran' ? WARM : SOFT);
      }
      if (stage >= 3) {
        g.text(2, 9, `answer  ${ep.answer}`, BRIGHT);
        g.text(51, 9, `= ${ep.value}`, TEXT);
      }
      if (stage >= 4) {
        g.text(2, 10, 'oracle  a hidden read-only query, same world', TEXT);
        g.text(51, 10, '= 4', TEXT);
      }
      if (stage >= 5) {
        g.text(2, 11, `reward  ${ep.reward}`, ep.ok ? ACCENT : WARM);
        g.text(16, 11, ep.note, ep.ok ? TEXT : WARM);
      }

      g.text(2, 13, 'last episodes', SOFT);
      for (let i = 0; i < 18; i++) {
        const past = ENV_EPISODES[(((n - 18 + i) % ENV_EPISODES.length) + ENV_EPISODES.length) % ENV_EPISODES.length];
        g.set(17 + i * 2, 13, past.ok ? '+' : '-', past.ok ? ACCENT : WARM);
      }
    },
  };

  // --- watch: nothing happened, and that was the news -----------------------

  const WATCH_POLLS = [3, 2, 4, 0, 0, 0, 0];

  scenes.watch = {
    cols: 56,
    rows: 12,
    poster: 9,
    draw(g, time) {
      const t = time % 13;
      g.text(2, 0, 'standing question', SOFT);
      g.text(21, 0, '"any shipment scans?"', BRIGHT);
      g.hline(2, 53, 1, '-', SOFT);
      WATCH_POLLS.forEach((rows, i) => {
        const at = 0.4 + i * 0.75;
        if (t < at) return;
        const y = 2 + i;
        const last = i === WATCH_POLLS.length - 1;
        g.text(2, y, `${pad(i, 2)}:14`, SOFT);
        g.text(9, y, 'poll', SOFT);
        g.text(15, y, `${rows} rows`, rows ? TEXT : WARM);
        if (!last) g.text(23, y, rows ? 'quiet' : 'still nothing', SOFT);
        if (last && t >= at + 0.4) g.text(23, y, '! nothing in 4 hours', WARM);
      });
      const fired = 0.4 + (WATCH_POLLS.length - 1) * 0.75 + 1.1;
      if (t >= fired) g.text(2, 10, '-> it tells you. nobody had to ask.', ACCENT);
      g.text(2, 11, 'kept alive by database polls, not model calls', SOFT);
    },
  };

  // --- sources: many systems, one map, one agent ----------------------------

  const SOURCE_LANES = [
    ['postgres', 'mysql'], ['snowflake', 'redshift'], ['mongodb', 'cassandra'], ['oracle', 'sql server'],
    ['openapi specs', 'bigquery'], ['s3 / gcs', 'local files'], ['source code', 'sqlite'],
  ];

  scenes.sources = {
    cols: 56,
    rows: 11,
    poster: 1.5,
    draw(g, time) {
      const mid = 4;
      SOURCE_LANES.forEach((names, i) => {
        const y = 1 + i;
        const d = Math.abs(y - mid);
        const end = 30 - d;
        const name = names[Math.floor((time + i * 0.6) / 5) % names.length];
        g.text(2, y, name, TEXT);
        for (let x = 16; x < end; x++) {
          const moving = (((x - Math.floor(time * 9) - i * 3) % 7) + 7) % 7 === 0;
          g.set(x, y, moving ? '>' : '-', moving ? ACCENT : SOFT);
        }
        g.set(end, y, y === mid ? '+' : y < mid ? '\\' : '/', ACCENT);
      });
      const beat = Math.floor(time * 6) % 3;
      g.text(31, mid, '==>', beat === 0 ? ACCENT : SOFT);
      g.text(35, mid, 'one map', BRIGHT);
      g.text(43, mid, '==>', beat === 1 ? ACCENT : SOFT);
      g.text(47, mid, 'one agent', BRIGHT);
      g.text(2, 9, 'a few lines of config for each, not a new set of tools', SOFT);
    },
  };

  // --- engine: GraphQL in, one statement out --------------------------------

  const ENGINE_IN = [
    'query {', '  customers(limit: 5) {', '    full_name', '    purchases {', '      quantity',
    '      product { price }', '    }', '  }', '}',
  ];
  const ENGINE_OUT = [
    'SELECT json_agg(row) FROM (', '  SELECT c.full_name, p.items', '  FROM customers c',
    '  LEFT JOIN LATERAL (', '    SELECT json_agg(...) items', '    FROM purchases p',
    '    JOIN products pr ON ...', '    WHERE p.customer_id = c.id', '  ) p ON true LIMIT 5', ') q;',
  ];

  scenes.engine = {
    cols: 60,
    rows: 14,
    poster: 12,
    draw(g, time) {
      const t = time % 15;
      g.text(2, 0, 'graphql in', SOFT);
      g.text(29, 0, 'one statement out', SOFT);
      g.vline(26, 0, 11, '|', SOFT);
      const left = ENGINE_IN.map((text, i) => ({ x: 2, y: 2 + i, text, tone: BRIGHT, gap: 0.05 }));
      const mid = script(g, left, t - 0.3, { cps: 55 });
      const right = ENGINE_OUT.map((text, i) => ({ x: 29, y: 2 + i, text, tone: i === 0 || i === 9 ? ACCENT : TEXT, gap: 0.05 }));
      const end = script(g, right, t - 0.3 - mid - 0.5, { cps: 75 }) + mid + 0.8;
      if (t > mid + 0.4 && t < end && Math.floor(t * 4) % 2 === 0) g.set(26, 6, '>', WARM);
      if (t > end) g.text(2, 13, '1 statement / 0 resolvers / no N+1', ACCENT);
    },
  };

  // --- how: one question, start to finish -----------------------------------

  const HOW_STEPS = [
    { name: 'ask', text: '"who are our top customers by spend?"', tone: BRIGHT },
    { name: 'find', text: 'in the map: customers, purchases, products' },
    { name: 'write', text: 'query { customers(limit: 5) { ... } }' },
    { name: 'check', text: 'your access rules, before anything runs', mark: 'ok' },
    { name: 'compile', text: 'one SQL statement, joins included' },
    { name: 'run', text: 'your database does the joins and sums' },
    { name: 'answer', text: '5 rows, and what was run to get them', tone: BRIGHT },
  ];

  scenes.how = {
    cols: 60,
    rows: 15,
    poster: 10,
    draw(g, time) {
      const T = 12.5;
      const pace = 1.15;
      const t = time % T;
      HOW_STEPS.forEach((step, i) => {
        const y = 1 + i * 2;
        const at = 0.4 + i * pace;
        const on = t >= at;
        const done = t >= at + pace;
        if (i > 0) {
          const arriving = t >= at - 0.35 && t < at;
          g.set(2, y - 1, arriving ? '*' : '|', arriving ? WARM : on ? ACCENT : SOFT);
        }
        g.set(2, y, 'o', on ? ACCENT : SOFT);
        g.text(5, y, step.name, on ? (done ? ACCENT : BRIGHT) : SOFT);
        if (!on) return;
        g.text(14, y, step.text.slice(0, Math.ceil(step.text.length * seg(t, at, at + 0.6))), step.tone ?? TEXT);
        if (step.mark && t >= at + 0.75) g.text(57, y, step.mark, ACCENT);
      });
      if (t > T - 0.45) g.ghost();
    },
  };

  const api = {
    scenes,
    Grid,
    render(name, t, data) {
      const scene = scenes[name];
      if (!scene) return null;
      const g = new Grid(scene.cols, scene.rows);
      scene.draw(g, t ?? scene.poster, data);
      return g;
    },
  };
  globalThis.DeepOrgAscii = api;

  if (typeof document === 'undefined') return;

  const motionOK = window.matchMedia('(prefers-reduced-motion: no-preference)').matches;
  const FRAME_MS = 1000 / 14;

  for (const pre of document.querySelectorAll('pre[data-ascii]')) {
    const scene = scenes[pre.dataset.ascii];
    if (!scene) continue;
    let data = null;
    if (pre.dataset.asciiData) {
      try {
        data = JSON.parse(document.getElementById(pre.dataset.asciiData).textContent);
      } catch {
        data = null;
      }
    }
    let last = '';
    const draw = (t) => {
      const html = api.render(pre.dataset.ascii, t, data).toHTML();
      if (html !== last) {
        pre.innerHTML = html;
        last = html;
      }
    };
    draw(scene.poster);
    if (!motionOK) continue;

    const figure = pre.closest('[data-ascii-frame]');
    const toggle = figure?.querySelector('[data-ascii-toggle]');
    let visible = !('IntersectionObserver' in window);
    let paused = false;
    let raf = 0;
    let clock = scene.poster;
    let lastTick = 0;
    let lastDraw = 0;
    const tick = (now) => {
      raf = requestAnimationFrame(tick);
      const delta = Math.min(now - lastTick, 250);
      lastTick = now;
      clock += delta / 1000;
      if (now - lastDraw < FRAME_MS) return;
      lastDraw = now;
      draw(clock);
    };
    const sync = () => {
      const run = visible && !paused && !document.hidden;
      if (run && !raf) {
        lastTick = performance.now();
        raf = requestAnimationFrame(tick);
      } else if (!run && raf) {
        cancelAnimationFrame(raf);
        raf = 0;
      }
    };
    if ('IntersectionObserver' in window) {
      new IntersectionObserver((entries) => {
        visible = entries.some((entry) => entry.isIntersecting);
        sync();
      }, { rootMargin: '80px' }).observe(pre);
    }
    document.addEventListener('visibilitychange', sync);
    if (toggle) {
      toggle.hidden = false;
      toggle.addEventListener('click', () => {
        paused = !paused;
        toggle.textContent = paused ? 'play' : 'pause';
        toggle.setAttribute('aria-pressed', String(paused));
        sync();
      });
    }
    sync();
  }
})();
