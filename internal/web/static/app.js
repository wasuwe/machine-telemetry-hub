// Line view: machine table fed by /events (Server-Sent Events), and the data path
// fed by /api/readings (rows read back from the store once a second).
(() => {
  const $ = (id) => document.getElementById(id);
  const tbody = $("machines");
  const rows = new Map(); // machine id -> <tr>
  const names = new Map(); // machine id -> "Press 01 · 80T"

  const pct1 = (v) => (v * 100).toFixed(1);
  const pct0 = (v) => Math.round(v * 100);
  const num = (v) => v.toLocaleString("en-US");
  const STATE_SHORT = { RUNNING: "RUN", IDLE: "IDLE", DOWN: "DOWN" };

  function dur(iso) {
    const s = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000));
    if (s < 60) return `${s}s`;
    const m = Math.floor(s / 60);
    if (m < 60) return `${m}m${String(s % 60).padStart(2, "0")}`;
    return `${Math.floor(m / 60)}h${String(m % 60).padStart(2, "0")}`;
  }

  function rowFor(m) {
    let tr = rows.get(m.id);
    if (!tr) {
      tr = document.createElement("tr");
      tr.innerHTML = `
        <td class="id"></td>
        <td class="press"></td>
        <td><span class="state"><span class="w"></span><span class="dur"></span></span></td>
        <td class="n"><span class="oee-cell"><span class="meter"><i></i></span><span class="v"></span></span></td>
        <td class="n opt a"></td>
        <td class="n opt p"></td>
        <td class="n opt q"></td>
        <td class="n parts"></td>
        <td class="n opt ngc"></td>
        <td class="n opt cyc"></td>
        <td><span class="tx" title="Lights when this press's sensor sends a reading"></span></td>`;
      tr.querySelector(".id").textContent = m.id;
      tr.querySelector(".press").textContent = m.name.split(" · ")[1] || "";
      tbody.appendChild(tr);
      rows.set(m.id, tr);
    }
    return tr;
  }

  function renderMachine(m) {
    const tr = rowFor(m);
    const q = (sel) => tr.querySelector(sel);
    tr.className = m.state;
    q(".state .w").textContent = STATE_SHORT[m.state] || m.state;
    q(".state .dur").textContent = dur(m.stateSince);
    q(".oee-cell .v").textContent = pct1(m.oee);
    q(".meter i").style.width = `${pct0(m.oee)}%`;
    q(".a").textContent = pct0(m.availability);
    q(".p").textContent = pct0(m.performance);
    q(".q").textContent = pct0(m.quality);
    q(".parts").textContent = num(m.total);
    const ng = m.total - m.good;
    q(".ngc").textContent = ng;
    q(".ngc").classList.toggle("ng", ng > 0);
    const ideal = (m.idealCycleMs / 1000).toFixed(2);
    const cyc = q(".cyc");
    if (m.lastCycleMs) {
      cyc.textContent = `${(m.lastCycleMs / 1000).toFixed(2)} / ${ideal}`;
      cyc.classList.toggle("slow", m.lastCycleMs > m.idealCycleMs * 1.2);
    } else {
      cyc.textContent = `– / ${ideal}`;
    }
  }

  function render(snap) {
    for (const m of snap.machines) names.set(m.id, m.name);
    const s = snap.summary;
    $("k-oee").textContent = `${pct1(s.avgOee)}%`;
    $("k-running").textContent = s.running;
    $("k-idle").textContent = s.idle;
    $("k-down").textContent = s.down;
    $("k-total").textContent = num(s.total);
    $("k-good").textContent = s.total ? `${pct1(s.good / s.total)}%` : "–";
    $("clock").textContent = new Date(snap.at).toLocaleTimeString("en-GB", { hour12: false });
    snap.machines.forEach(renderMachine);
  }

  const conn = $("conn");
  function setConn(state, text) {
    conn.className = `conn ${state}`;
    $("conn-text").textContent = text;
  }

  // Theme: Control room (dark, default) or Shop floor (light), remembered per browser.
  const root = document.documentElement;
  const toggle = $("theme-toggle");
  const showTheme = () => { toggle.textContent = root.dataset.theme === "light" ? "DARK" : "LIGHT"; };
  toggle.addEventListener("click", () => {
    root.dataset.theme = root.dataset.theme === "light" ? "dark" : "light";
    try { localStorage.setItem("theme", root.dataset.theme); } catch { /* storage blocked: still switches */ }
    showTheme();
  });
  showTheme();

  // ---- Data path ----
  // Every second, read the rows written since the last read, then replay them:
  // each reading travels press -> sensor -> gateway -> DB, then one query goes
  // DB -> screen and the rows are added to the log.
  const flow = $("pipe-flow");
  const stopEl = (n) => flow.querySelector(`[data-node="${n}"]`);
  const logEl = $("log");
  const reduceMotion = matchMedia("(prefers-reduced-motion: reduce)");
  const LOG_LINES = 8;
  const MAX_PACKETS = 10; // per batch; the rest are still logged, just not animated
  const HOP_MS = 340;
  const WIRE_Y = 30; // px from the top of .wire; matches .wire::before in style.css
  let lastId = 0;
  let written = 0;
  let rateFrom = null; // { t, total } for rows/s

  const wait = (ms) => new Promise((r) => setTimeout(r, ms));
  const animate = () => !reduceMotion.matches && !document.hidden && flow.offsetParent !== null;

  function point(n) {
    const f = flow.getBoundingClientRect();
    const r = stopEl(n).getBoundingClientRect();
    return [r.left - f.left + r.width / 2, WIRE_Y];
  }

  function flash(el, cls, ms) {
    el.classList.add(cls);
    setTimeout(() => el.classList.remove(cls), ms);
  }

  function travel(cls, path) {
    const p = document.createElement("span");
    p.className = `packet ${cls}`;
    flow.appendChild(p);
    const frames = path.map((n) => {
      const [x, y] = point(n);
      return { transform: `translate(${x}px, ${y}px) translate(-50%, -50%)` };
    });
    const anim = p.animate(frames, { duration: HOP_MS * (path.length - 1), easing: "linear" });
    return anim.finished.catch(() => {}).then(() => p.remove());
  }

  const packetClass = (r) => (r.kind === "state" ? r.state : r.good ? "ok" : "ng");

  function logLine(r) {
    const li = document.createElement("li");
    li.className = "new";
    const d = new Date(r.at);
    const t = d.toLocaleTimeString("en-GB", { hour12: false });
    const ms = String(d.getMilliseconds()).padStart(3, "0");
    let what;
    if (r.kind === "state") what = `<span class="c-${r.state}">STATE → ${r.state}</span>`;
    else what = r.good ? `SHOT OK  ${(r.cycleMs / 1000).toFixed(2)}s` : `<span class="ng">SHOT NG</span>  ${(r.cycleMs / 1000).toFixed(2)}s`;
    li.innerHTML = `<span>#${r.id}</span><span>${t}<span class="ms">.${ms}</span></span><span>${r.machine}</span><span>${what}</span>`;
    return li;
  }

  function showRows(list) {
    for (const r of list) logEl.prepend(logLine(r));
    while (logEl.children.length > LOG_LINES) logEl.lastElementChild.remove();
  }

  function sent(r) {
    const tx = rows.get(r.machine)?.querySelector(".tx");
    if (tx) flash(tx, "on", 350);
  }

  function stored(n) {
    written += n;
    $("n-rows").textContent = num(written);
  }

  function updateRate(total) {
    const now = performance.now();
    if (!rateFrom || now - rateFrom.t > 10000) rateFrom = { t: now, total };
    else if (now - rateFrom.t > 2000) $("rate").textContent = ((total - rateFrom.total) / ((now - rateFrom.t) / 1000)).toFixed(1);
  }

  async function replay(list, total) {
    if (!animate()) {
      list.forEach(sent);
      written = total - list.length;
      stored(list.length);
      showRows(list);
      return;
    }
    const shown = list.slice(-MAX_PACKETS);
    written = Math.max(written, total - list.length);
    stored(list.length - shown.length);
    const gap = Math.min(90, 900 / shown.length);
    await Promise.all(shown.map(async (r, i) => {
      await wait(i * gap);
      sent(r);
      flash(stopEl("machine"), "hit", 200);
      await travel(packetClass(r), ["machine", "sensor", "gateway", "db"]);
      stored(1);
      flash(stopEl("db"), "hit", 200);
    }));
    await travel("query", ["db", "app"]);
    flash(stopEl("app"), "hit", 300);
    showRows(list);
  }

  let busy = false;
  async function poll() {
    if (busy) return; // previous batch is still on screen
    busy = true;
    try {
      const res = await fetch(`api/readings?after=${lastId}&limit=50`, { cache: "no-store" });
      if (res.ok) {
        const { total, rows: list } = await res.json();
        if (total < lastId) { // server restarted (new container): start over
          lastId = 0;
          written = 0;
          rateFrom = null;
          logEl.replaceChildren();
        }
        updateRate(total);
        if (list.length) {
          lastId = list[list.length - 1].id;
          await replay(list, total);
        }
      }
    } catch { /* offline for a moment: try again next tick */ }
    busy = false;
  }
  poll();
  setInterval(poll, 1000);

  const es = new EventSource("events");
  es.onopen = () => setConn("live", "LIVE");
  es.onmessage = (e) => render(JSON.parse(e.data));
  es.onerror = () => setConn("lost", "RECONNECTING"); // EventSource retries on its own
})();
