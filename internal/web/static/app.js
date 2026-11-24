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

  function render(snap) {
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

  const es = new EventSource("events");
  es.onopen = () => setConn("live", "Live");
  es.onmessage = (e) => render(JSON.parse(e.data));
  es.onerror = () => setConn("lost", "Reconnecting…"); // EventSource retries on its own
})();
