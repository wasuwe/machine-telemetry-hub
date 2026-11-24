// Live dashboard: listens to /events (Server-Sent Events) and updates cards in place.
(() => {
  const grid = document.getElementById("grid");
  const tpl = document.getElementById("card-tpl");
  const conn = document.getElementById("conn");
  const connText = document.getElementById("conn-text");
  const cards = new Map(); // machine id -> card element

  const pct = (v) => `${Math.round(v * 100)}%`;
  const num = (v) => v.toLocaleString("en-US");

  function ago(iso) {
    const s = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000));
    if (s < 60) return `${s}s`;
    const m = Math.floor(s / 60);
    return `${m}m ${s % 60}s`;
  }

  function cardFor(m) {
    let card = cards.get(m.id);
    if (!card) {
      card = tpl.content.firstElementChild.cloneNode(true);
      card.querySelector(".name").textContent = m.name;
      grid.appendChild(card);
      cards.set(m.id, card);
    }
    return card;
  }

  function renderMachine(m) {
    const card = cardFor(m);
    card.dataset.state = m.state;
    card.querySelector(".badge").textContent = m.state;
    card.querySelector(".since").textContent = `${m.state.toLowerCase()} for ${ago(m.stateSince)}`;
    card.querySelector(".oee-value").textContent = pct(m.oee);
    for (const el of card.querySelectorAll(".factor")) {
      const v = m[el.dataset.f];
      el.querySelector("i").style.width = pct(v);
      el.querySelector("b").textContent = pct(v);
    }
    card.querySelector(".parts").textContent = num(m.total);
    card.querySelector(".cycle").textContent = m.lastCycleMs
      ? `cycle ${(m.lastCycleMs / 1000).toFixed(2)}s / ideal ${(m.idealCycleMs / 1000).toFixed(1)}s`
      : `ideal ${(m.idealCycleMs / 1000).toFixed(1)}s`;
  }

  const names = new Map(); // machine id -> display name

  function render(snap) {
    for (const m of snap.machines) names.set(m.id, m.name);
    document.getElementById("n-machines").textContent = `${snap.machines.length} machines`;
    const s = snap.summary;
    document.getElementById("k-oee").textContent = pct(s.avgOee);
    document.getElementById("k-running").textContent = s.running;
    document.getElementById("k-idle").textContent = s.idle;
    document.getElementById("k-down").textContent = s.down;
    document.getElementById("k-total").textContent = num(s.total);
    document.getElementById("k-good").textContent = s.total ? pct(s.good / s.total) : "–";
    snap.machines.forEach(renderMachine);
  }

  function setConn(state, text) {
    conn.className = `conn ${state}`;
    connText.textContent = text;
  }

  // Theme toggle: Control room (dark, default) or Shop floor (light), remembered per browser.
  const root = document.documentElement;
  const toggle = document.getElementById("theme-toggle");
  function showTheme() {
    toggle.textContent = root.dataset.theme === "light" ? "Dark theme" : "Light theme";
  }
  toggle.addEventListener("click", () => {
    root.dataset.theme = root.dataset.theme === "light" ? "dark" : "light";
    try { localStorage.setItem("theme", root.dataset.theme); } catch { /* storage blocked: still switches */ }
    showTheme();
  });
  showTheme();

  // ---- Data path ----
  // Every second, read the rows written since the last read from /api/readings,
  // then replay them: each reading travels press -> sensor -> gateway -> database,
  // and one query goes database -> dashboard before the rows appear in the table.
  const flow = document.getElementById("pipe-flow");
  const node = (n) => flow.querySelector(`[data-node="${n}"]`);
  const rowsEl = document.getElementById("rows");
  const rowCount = document.getElementById("n-rows");
  const reduceMotion = matchMedia("(prefers-reduced-motion: reduce)");
  const MAX_TABLE_ROWS = 8;
  const MAX_PACKETS = 10; // per batch; extra rows are still stored and listed, just not animated
  const HOP_MS = 380;
  let lastId = 0;
  let written = 0;

  const wait = (ms) => new Promise((r) => setTimeout(r, ms));
  const animate = () => !reduceMotion.matches && !document.hidden && flow.offsetParent !== null; // not while the API tab is open

  const LANE = 14; // px from the flow's top (or left edge when stacked); matches --lane in style.css
  const stacked = matchMedia("(max-width: 520px)");

  // Point on the wire next to a node: above it, or to its left when the nodes are stacked.
  function stop(el) {
    const f = flow.getBoundingClientRect();
    const r = el.getBoundingClientRect();
    return stacked.matches
      ? [LANE, r.top - f.top + r.height / 2]
      : [r.left - f.left + r.width / 2, LANE];
  }

  function flash(el, cls, ms) {
    el.classList.add(cls);
    setTimeout(() => el.classList.remove(cls), ms);
  }

  // Moves a small labelled pill through the given nodes; resolves when it arrives.
  function travel(label, cls, path) {
    const p = document.createElement("span");
    p.className = `packet ${cls}`;
    p.textContent = label;
    flow.appendChild(p);
    const frames = path.map((n) => {
      const [x, y] = stop(node(n));
      return { transform: `translate(${x}px, ${y}px) translate(-50%, -50%)` };
    });
    const anim = p.animate(frames, { duration: HOP_MS * (path.length - 1), easing: "linear" });
    return anim.finished.catch(() => {}).then(() => p.remove());
  }

  function packetClass(r) {
    if (r.kind === "state") return `s-${r.state}`;
    return r.good ? "good" : "scrap";
  }

  function reading(r) {
    if (r.kind === "state") return `<span class="v-${r.state}">state → ${r.state}</span>`;
    const cls = r.good ? "good" : "scrap";
    return `<span class="v-${cls}">${r.good ? "good part" : "scrap"}</span> · ${(r.cycleMs / 1000).toFixed(2)}s`;
  }

  function time(iso) {
    const d = new Date(iso);
    return d.toLocaleTimeString("en-GB", { hour12: false }) + "." + String(d.getMilliseconds()).padStart(3, "0");
  }

  function showRows(rows) {
    for (const r of rows) {
      const tr = document.createElement("tr");
      tr.className = "new";
      tr.innerHTML = `<td>${r.id}</td><td>${time(r.at)}</td><td></td><td>${reading(r)}</td>`;
      tr.children[2].textContent = names.get(r.machine)?.split(" · ")[0] ?? r.machine;
      rowsEl.prepend(tr);
    }
    while (rowsEl.children.length > MAX_TABLE_ROWS) rowsEl.lastElementChild.remove();
  }

  function sent(r) {
    const card = cards.get(r.machine);
    if (card) flash(card.querySelector(".iot"), "tx", 400);
  }

  function stored(n) {
    written += n;
    rowCount.textContent = num(written);
  }

  async function replay(rows, total) {
    if (!animate()) {
      rows.forEach(sent);
      written = total - rows.length;
      stored(rows.length);
      showRows(rows);
      return;
    }
    const shown = rows.slice(-MAX_PACKETS);
    written = Math.max(written, total - rows.length);
    stored(rows.length - shown.length);
    const gap = Math.min(90, 900 / shown.length);
    await Promise.all(shown.map(async (r, i) => {
      await wait(i * gap);
      sent(r);
      await travel(r.machine, packetClass(r), ["machine", "sensor", "gateway", "db"]);
      stored(1);
      flash(node("db"), "hit", 250);
    }));
    await travel("SELECT", "query", ["db", "app"]);
    flash(node("app"), "hit", 300);
    showRows(rows);
  }

  let busy = false;
  async function poll() {
    if (busy) return; // previous batch is still on screen
    busy = true;
    try {
      const res = await fetch(`api/readings?after=${lastId}&limit=50`, { cache: "no-store" });
      if (res.ok) {
        const { total, rows } = await res.json();
        if (total < lastId) { // server restarted (new container): start over
          lastId = 0;
          written = 0;
          rowsEl.replaceChildren();
        }
        if (rows.length) {
          lastId = rows[rows.length - 1].id;
          await replay(rows, total);
        }
      }
    } catch { /* offline for a moment: try again next tick */ }
    busy = false;
  }
  poll();
  setInterval(poll, 1000);

  const es = new EventSource("events");
  es.onopen = () => setConn("live", "Live");
  es.onmessage = (e) => render(JSON.parse(e.data));
  es.onerror = () => setConn("lost", "Reconnecting…"); // EventSource retries on its own
})();
