// Tabs (LINE / API) and the API reference: key, endpoint and status-code tables, and the console.
(() => {
  const $ = (id) => document.getElementById(id);

  // ---- Tabs. "#api" or a "#ref-…" anchor in the URL opens the API tab ----
  const tabs = [...document.querySelectorAll('[role="tab"]')];
  function select(tab) {
    for (const t of tabs) {
      const on = t === tab;
      t.setAttribute("aria-selected", on);
      t.tabIndex = on ? 0 : -1;
      $(t.getAttribute("aria-controls")).hidden = !on;
    }
  }
  const isApiHash = () => location.hash === "#api" || location.hash.startsWith("#ref-");
  const fromHash = () => select(isApiHash() ? tabs[1] : tabs[0]);

  for (const t of tabs) {
    t.addEventListener("click", () => {
      history.replaceState(null, "", t.id === "tab-api" ? "#api" : location.pathname);
      select(t);
    });
    t.addEventListener("keydown", (e) => { // arrow keys move between tabs (WAI-ARIA tabs pattern)
      const i = tabs.indexOf(t);
      const step = e.key === "ArrowRight" ? 1 : e.key === "ArrowLeft" ? -1 : 0;
      if (!step) return;
      e.preventDefault();
      const next = tabs[(i + step + tabs.length) % tabs.length];
      next.click();
      next.focus();
    });
  }
  addEventListener("hashchange", fromHash);
  fromHash();

  // ---- Reference data ----
  const origin = location.origin;
  const FULL = "Full access";

  const keyResult = {
    "Full access": "200 everywhere",
    "Readings only": "403 on /machines, 200 on /readings",
    "Low rate limit": "429 from the 6th call in a minute",
    "Revoked": "401 api_key_revoked",
  };

  const endpoints = [
    { path: "/api/v1/machines", scope: "machines:read", text: "Every press: state, parts, OEE factors. Plant totals in <code>meta.summary</code>." },
    { path: "/api/v1/machines/M03", scope: "machines:read", text: "One press, <code>M01</code>–<code>M12</code>." },
    { path: "/api/v1/readings?after=0&limit=20", scope: "readings:read", text: "Sensor rows, oldest first. Send <code>meta.nextAfter</code> back as <code>after</code> to get only new rows." },
    { path: "/api/v1/status/503", scope: "–", text: "Returns the status you ask for. For testing error handling." },
  ];

  const codes = [
    { code: 200, name: "OK", when: "Request worked. Body: <code>data</code>, <code>meta</code>.", t: { path: "/api/v1/machines", key: FULL } },
    { code: 204, name: "No Content", when: "CORS preflight (<code>OPTIONS</code>).", t: { method: "OPTIONS", path: "/api/v1/machines", key: "" } },
    { code: 400, name: "Bad Request", when: "Bad query parameter: <code>limit=0</code>, <code>after=abc</code>.", t: { path: "/api/v1/readings?limit=0", key: FULL } },
    { code: 401, name: "Unauthorized", when: "No key, unknown key, or revoked key.", t: { path: "/api/v1/machines", key: "" } },
    { code: 403, name: "Forbidden", when: "Valid key without the scope.", t: { path: "/api/v1/machines", key: "Readings only" } },
    { code: 404, name: "Not Found", when: "Unknown press or path.", t: { path: "/api/v1/machines/M99", key: FULL } },
    { code: 405, name: "Method Not Allowed", when: "Anything but <code>GET</code>, <code>HEAD</code>, <code>OPTIONS</code>. See <code>Allow</code>.", t: { method: "POST", path: "/api/v1/machines", key: FULL } },
    { code: 429, name: "Too Many Requests", when: "Per-minute limit used up. Wait <code>Retry-After</code> seconds.", t: { path: "/api/v1/machines", key: "Low rate limit", repeat: 6 } },
    { code: 500, name: "Internal Server Error", when: "Server bug. Retry later; report the <code>requestId</code>. Test endpoint only.", t: { path: "/api/v1/status/500", key: "" } },
    { code: 503, name: "Service Unavailable", when: "Overloaded or restarting. Retry after <code>Retry-After</code>. Test endpoint only.", t: { path: "/api/v1/status/503", key: "" } },
  ];

  // ---- Console ----
  const methodEl = $("c-method");
  const pathEl = $("c-path");
  const keyEl = $("c-key");
  const sendBtn = $("c-send");
  const curlEl = $("c-curl");
  const out = $("c-response");
  const consoleEl = $("console");
  const keyByName = new Map();
  const WRONG = "mth_test_does_not_exist";

  function curl(method, path, key) {
    const parts = [method === "HEAD" ? "curl -I" : "curl -i"];
    if (method !== "GET" && method !== "HEAD") parts.push(`-X ${method}`);
    if (key) parts.push(`-H "Authorization: Bearer ${key}"`);
    parts.push(`"${origin}${path}"`);
    return parts.join(" \\\n  ");
  }
  const showCurl = () => { curlEl.textContent = `$ ${curl(methodEl.value, pathEl.value, keyEl.value)}`; };

  const SHOWN_HEADERS = ["content-type", "allow", "www-authenticate", "ratelimit-limit", "ratelimit-remaining", "ratelimit-reset", "retry-after", "x-request-id"];

  function span(cls, text) {
    const s = document.createElement("span");
    if (cls) s.className = cls;
    s.textContent = text;
    return s;
  }

  async function send() {
    const method = methodEl.value;
    const path = pathEl.value.trim() || "/";
    const headers = {};
    if (keyEl.value) headers.Authorization = `Bearer ${keyEl.value}`;
    sendBtn.disabled = true;
    const t0 = performance.now();
    try {
      const res = await fetch(path, { method, headers, cache: "no-store" });
      const text = await res.text();
      render(res, Math.round(performance.now() - t0), text);
    } catch (err) {
      out.replaceChildren(span("s5", `request failed: ${err.message}`));
    } finally {
      sendBtn.disabled = false;
    }
  }

  // Prints the response the way curl -i would.
  function render(res, ms, text) {
    const parts = [
      span(`s${String(res.status)[0]}`, `HTTP ${res.status} ${res.statusText}`.trim()),
      span("dim", `   ${ms} ms\n`),
    ];
    for (const name of SHOWN_HEADERS) {
      const v = res.headers.get(name);
      if (v != null) parts.push(span("h", `${name}: `), span(null, `${v}\n`));
    }
    let body = text;
    try { body = JSON.stringify(JSON.parse(text), null, 2); } catch { /* not JSON */ }
    parts.push(span(null, `\n${body || ""}`));
    out.replaceChildren(...parts);
  }

  async function run(t) {
    methodEl.value = t.method || "GET";
    pathEl.value = t.path;
    keyEl.value = t.key ? keyByName.get(t.key) ?? "" : "";
    showCurl();
    setOpen(true);
    consoleEl.scrollIntoView({ block: "nearest", behavior: "smooth" }); // only scrolls when the console is not docked
    for (let i = 0; i < (t.repeat || 1); i++) await send();
  }

  function runButton(t, label) {
    const b = document.createElement("button");
    b.type = "button";
    b.className = "run";
    b.textContent = "run ›";
    b.setAttribute("aria-label", label);
    b.addEventListener("click", () => run(t));
    return b;
  }

  function setOpen(open) {
    consoleEl.classList.toggle("min", !open);
    $("c-toggle").textContent = open ? "HIDE" : "SHOW";
    $("c-toggle").setAttribute("aria-expanded", open);
  }
  $("c-toggle").addEventListener("click", () => setOpen(consoleEl.classList.contains("min")));

  $("console-form").addEventListener("submit", (e) => { e.preventDefault(); send(); });
  for (const x of [methodEl, pathEl, keyEl]) x.addEventListener("input", showCurl);

  // ---- Tables ----
  for (const ep of endpoints) {
    const tr = document.createElement("tr");
    tr.innerHTML = `<td><code>GET ${ep.path}</code></td><td><code>${ep.scope}</code></td><td>${ep.text}</td><td></td>`;
    tr.lastElementChild.append(runButton({ path: ep.path, key: ep.scope === "–" ? "" : FULL }, `Run GET ${ep.path}`));
    $("endpoints-body").append(tr);
  }
  for (const c of codes) {
    const tr = document.createElement("tr");
    tr.innerHTML = `<td class="code code-${String(c.code)[0]}">${c.code}</td><td><b>${c.name}.</b> ${c.when}</td><td></td>`;
    tr.lastElementChild.append(runButton(c.t, `Run ${c.code} ${c.name}`));
    $("codes-body").append(tr);
  }

  async function loadKeys() {
    const keys = await (await fetch("api/demo-keys", { cache: "no-store" })).json();
    keyEl.append(new Option("no key", ""));
    for (const k of keys) {
      keyByName.set(k.name, k.key);
      keyEl.append(new Option(`${k.key}  (${k.name.toLowerCase()})`, k.key));

      const tr = document.createElement("tr");
      tr.innerHTML = `<td><code></code></td><td></td><td class="n"></td><td></td><td></td>`;
      tr.children[0].firstChild.textContent = k.key;
      tr.children[1].textContent = k.scopes.join(" ");
      tr.children[2].textContent = k.revoked ? "–" : `${k.perMinute}/min`;
      tr.children[3].textContent = keyResult[k.name] || "";
      const cp = document.createElement("button");
      cp.type = "button";
      cp.className = "run";
      cp.textContent = "copy";
      cp.setAttribute("aria-label", `Copy ${k.name} key`);
      cp.addEventListener("click", async () => {
        try { await navigator.clipboard.writeText(k.key); cp.textContent = "copied"; } catch { cp.textContent = "select it"; }
        setTimeout(() => { cp.textContent = "copy"; }, 1200);
      });
      tr.children[4].append(cp, runButton({ path: k.scopes.includes("machines:read") ? "/api/v1/machines" : "/api/v1/readings", key: k.name }, `Run with ${k.name} key`));
      $("keys-body").append(tr);
    }
    keyEl.append(new Option(`${WRONG}  (unknown)`, WRONG));
    keyEl.value = keyByName.get(FULL) || "";
    $("auth-example").textContent = `GET /api/v1/machines HTTP/1.1\nHost: ${location.host}\nAuthorization: Bearer ${keyByName.get(FULL)}`;
    showCurl();
  }

  loadKeys().catch(() => { $("keys-body").innerHTML = '<tr><td colspan="5">Could not load the test keys.</td></tr>'; });
})();
