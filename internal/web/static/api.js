// Tabs (Dashboard / API) and the API tab: demo keys, endpoint and status-code tables, and a console.
(() => {
  // ---- Tabs: #api in the URL opens the API tab, so the link can be shared ----
  const tabs = [...document.querySelectorAll('[role="tab"]')];

  function select(tab, focus) {
    for (const t of tabs) {
      const on = t === tab;
      t.setAttribute("aria-selected", on);
      t.tabIndex = on ? 0 : -1;
      document.getElementById(t.getAttribute("aria-controls")).hidden = !on;
    }
    if (focus) tab.focus();
  }
  const fromHash = () => select(location.hash === "#api" ? tabs[1] : tabs[0], false);

  for (const t of tabs) {
    t.addEventListener("click", () => {
      history.replaceState(null, "", t.id === "tab-api" ? "#api" : location.pathname);
      select(t, false);
    });
    t.addEventListener("keydown", (e) => { // arrow keys move between tabs (WAI-ARIA tabs pattern)
      const i = tabs.indexOf(t);
      const next = e.key === "ArrowRight" ? tabs[(i + 1) % tabs.length] : e.key === "ArrowLeft" ? tabs[(i - 1 + tabs.length) % tabs.length] : null;
      if (next) { e.preventDefault(); next.click(); next.focus(); }
    });
  }
  addEventListener("hashchange", fromHash);
  fromHash();

  // ---- Reference data ----
  const origin = location.origin;
  document.getElementById("base-url").textContent = `${origin}/api/v1`;

  const keyNotes = {
    "Full access": "200 responses from every endpoint",
    "Readings only": "403 on /machines (missing scope), 200 on /readings",
    "Low rate limit": "429 after 5 requests in a minute",
    "Revoked": "401 api_key_revoked",
  };

  const endpoints = [
    { path: "/api/v1/machines", scope: "machines:read", text: "All machines: state, parts, OEE factors. <code>meta.summary</code> has plant totals." },
    { path: "/api/v1/machines/M03", scope: "machines:read", text: "One machine by ID (<code>M01</code>…<code>M12</code>)." },
    { path: "/api/v1/readings?after=0&limit=20", scope: "readings:read", text: "Rows stored by the IoT sensors, oldest first. Pass <code>meta.nextAfter</code> as <code>after</code> on the next call to get only new rows." },
    { path: "/api/v1/status/503", scope: "none", text: "Test endpoint: returns the code you ask for, so you can test your error handling." },
  ];

  const FULL = "Full access";
  const codes = [
    { code: 200, name: "OK", when: "The request worked. The body has <code>data</code> and <code>meta</code>.", try: { path: "/api/v1/machines", key: FULL } },
    { code: 204, name: "No Content", when: "CORS preflight (<code>OPTIONS</code>) and the test endpoint. No body.", try: { method: "OPTIONS", path: "/api/v1/machines", key: "" } },
    { code: 400, name: "Bad Request", when: "A query parameter is not valid, e.g. <code>limit=0</code> or <code>after=abc</code>.", try: { path: "/api/v1/readings?limit=0", key: FULL } },
    { code: 401, name: "Unauthorized", when: "No key, an unknown key, or a revoked key. Has a <code>WWW-Authenticate</code> header.", try: { path: "/api/v1/machines", key: "" } },
    { code: 403, name: "Forbidden", when: "The key is valid but lacks the scope, e.g. the readings-only key on <code>/machines</code>.", try: { path: "/api/v1/machines", key: "Readings only" } },
    { code: 404, name: "Not Found", when: "Unknown machine ID or unknown path.", try: { path: "/api/v1/machines/M99", key: FULL } },
    { code: 405, name: "Method Not Allowed", when: "Anything other than <code>GET</code>, <code>HEAD</code> or <code>OPTIONS</code>. The <code>Allow</code> header lists what works.", try: { method: "POST", path: "/api/v1/machines", key: FULL } },
    { code: 429, name: "Too Many Requests", when: "The key used up its per-minute limit. Wait for <code>Retry-After</code> seconds.", try: { path: "/api/v1/machines", key: "Low rate limit", repeat: 6 } },
    { code: 500, name: "Internal Server Error", when: "A bug on the server. Retry later and send the <code>requestId</code> with your report. Test it with <code>/status/500</code>.", try: { path: "/api/v1/status/500", key: "" } },
    { code: 503, name: "Service Unavailable", when: "The server is overloaded or restarting. Retry after <code>Retry-After</code>. Test it with <code>/status/503</code>.", try: { path: "/api/v1/status/503", key: "" } },
  ];

  // ---- Console ----
  const form = document.getElementById("console-form");
  const methodEl = document.getElementById("c-method");
  const pathEl = document.getElementById("c-path");
  const keyEl = document.getElementById("c-key");
  const sendBtn = document.getElementById("c-send");
  const curlEl = document.getElementById("c-curl");
  const out = document.getElementById("c-response");
  const keyByName = new Map();
  const WRONG = "mth_test_does_not_exist";

  function curl(method, path, key) {
    const parts = ["curl -i"];
    if (method !== "GET") parts.push(`-X ${method}`);
    if (key) parts.push(`-H "Authorization: Bearer ${key}"`);
    parts.push(`"${origin}${path}"`);
    return parts.join(" \\\n  ");
  }
  const showCurl = () => { curlEl.textContent = curl(methodEl.value, pathEl.value, keyEl.value); };

  function el(tag, cls, text) {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text != null) e.textContent = text;
    return e;
  }

  const SHOWN_HEADERS = ["content-type", "ratelimit-limit", "ratelimit-remaining", "ratelimit-reset", "retry-after", "www-authenticate", "allow", "x-request-id"];

  async function send() {
    const method = methodEl.value;
    const path = pathEl.value.trim() || "/";
    const headers = {};
    if (keyEl.value) headers.Authorization = `Bearer ${keyEl.value}`;
    sendBtn.disabled = true;
    const t0 = performance.now();
    try {
      const res = await fetch(path, { method, headers, cache: "no-store" });
      const ms = Math.round(performance.now() - t0);
      const text = await res.text();
      render(res, ms, text);
    } catch (err) {
      out.replaceChildren(el("p", "response-empty", `Request failed: ${err.message}`));
    } finally {
      sendBtn.disabled = false;
    }
  }

  function render(res, ms, text) {
    const head = el("div", "response-head");
    const pill = el("span", `status s${String(res.status)[0]}xx`, `${res.status} ${res.statusText || ""}`.trim());
    head.append(pill, el("span", "muted", `${ms} ms`));

    const hdrs = el("dl", "headers");
    for (const name of SHOWN_HEADERS) {
      const v = res.headers.get(name);
      if (v == null) continue;
      hdrs.append(el("dt", null, name), el("dd", null, v));
    }

    let body = text;
    try { body = JSON.stringify(JSON.parse(text), null, 2); } catch { /* not JSON */ }
    const pre = el("pre", "code body", body || "(no body)");

    out.replaceChildren(head, hdrs, pre);
  }

  async function run(t) {
    methodEl.value = t.method || "GET";
    pathEl.value = t.path;
    keyEl.value = t.key === "" ? "" : keyByName.get(t.key) ?? "";
    showCurl();
    document.querySelector(".console").scrollIntoView({ block: "nearest", behavior: "smooth" });
    for (let i = 0; i < (t.repeat || 1); i++) await send();
  }

  function tryButton(t, label) {
    const b = el("button", "try", "Try");
    b.type = "button";
    b.setAttribute("aria-label", label);
    b.addEventListener("click", () => run(t));
    return b;
  }

  form.addEventListener("submit", (e) => { e.preventDefault(); send(); });
  for (const x of [methodEl, pathEl, keyEl]) x.addEventListener("input", showCurl);

  // ---- Fill the tables ----
  function fillStatic() {
    const eb = document.getElementById("endpoints-body");
    for (const ep of endpoints) {
      const tr = document.createElement("tr");
      tr.innerHTML = `<td><code>GET ${ep.path}</code><span class="scope">scope: ${ep.scope === "none" ? "no key needed" : ep.scope}</span></td><td>${ep.text}</td><td></td>`;
      const key = ep.scope === "none" ? "" : FULL;
      tr.lastElementChild.append(tryButton({ path: ep.path, key }, `Try GET ${ep.path}`));
      eb.append(tr);
    }
    const cb = document.getElementById("codes-body");
    for (const c of codes) {
      const tr = document.createElement("tr");
      tr.innerHTML = `<td><span class="status s${String(c.code)[0]}xx">${c.code}</span></td><td>${c.name}</td><td>${c.when}</td><td></td>`;
      tr.lastElementChild.append(tryButton(c.try, `Try ${c.code} ${c.name}`));
      cb.append(tr);
    }
  }

  async function loadKeys() {
    const res = await fetch("api/demo-keys", { cache: "no-store" });
    const keys = await res.json();
    const kb = document.getElementById("keys-body");
    keyEl.append(new Option("No key", ""));
    for (const k of keys) {
      keyByName.set(k.name, k.key);
      keyEl.append(new Option(`${k.name} · ${k.key}`, k.key));

      const tr = document.createElement("tr");
      const keyCell = el("td");
      const copy = el("button", "copy", "Copy");
      copy.type = "button";
      copy.setAttribute("aria-label", `Copy ${k.name} key`);
      copy.addEventListener("click", async () => {
        try { await navigator.clipboard.writeText(k.key); copy.textContent = "Copied"; }
        catch { copy.textContent = "Select it"; }
        setTimeout(() => { copy.textContent = "Copy"; }, 1200);
      });
      keyCell.append(el("b", null, k.name), el("br"), el("code", null, k.key), " ", copy);
      tr.append(
        keyCell,
        el("td", null, k.scopes.join(", ")),
        el("td", null, `${k.perMinute}/min`),
        el("td", null, keyNotes[k.name] || ""),
      );
      kb.append(tr);
    }
    keyEl.append(new Option("Wrong key", WRONG));
    keyEl.value = keyByName.get(FULL) || "";
    document.getElementById("quick-curl").textContent = curl("GET", "/api/v1/machines", keyByName.get(FULL));
    showCurl();
  }

  fillStatic();
  loadKeys().catch(() => { document.getElementById("keys-body").innerHTML = "<tr><td colspan=4>Could not load the demo keys.</td></tr>"; });
})();
