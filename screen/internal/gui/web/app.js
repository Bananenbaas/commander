/* SPDX-License-Identifier: GPL-2.0-or-later */
"use strict";

// Commander's window. Every value from Commander is treated as text: the
// page is built with createElement and textContent only.

(function () {
  var view = window.chrome && window.chrome.webview;
  var nav = document.getElementById("nav");
  var main = document.getElementById("main");
  var statusText = document.getElementById("status");
  var dot = document.getElementById("dot");

  var app = {
    state: null,
    stateError: "",
    settings: null,
    settingsError: "",
    update: null,
    updateError: "",
    page: "kb7", // "kb7", "desktop", "game:<index>", "settings", "updates"
    tab: "buttons",
    busy: {}, // control key: true while its call runs
    notes: {}, // control key: {text, error}
    checked: false, // a check ran in this window
    started: false // the first state and settings answers are in
  };

  // Calls

  var nextId = 0;
  var pending = {};

  function post(obj) {
    view.postMessage(JSON.stringify(obj));
  }

  function call(name, args) {
    return new Promise(function (resolve, reject) {
      nextId += 1;
      var id = nextId;
      pending[id] = { resolve: resolve, reject: reject };
      var msg = { id: id, call: name };
      if (args !== undefined) {
        msg.args = args;
      }
      post(msg);
    });
  }

  function onMessage(e) {
    var m = e.data;
    if (typeof m === "string") {
      try {
        m = JSON.parse(m);
      } catch (err) {
        return;
      }
    }
    // The binding echoes every posted message back: those carry "call".
    if (!m || typeof m !== "object" || Object.prototype.hasOwnProperty.call(m, "call")) {
      return;
    }
    if (typeof m.event === "string") {
      if (m.event === "state" && m.data && typeof m.data === "object") {
        app.state = m.data;
        app.stateError = "";
        if (app.started) {
          render();
        }
      }
      return;
    }
    if (typeof m.id !== "number" || !Object.prototype.hasOwnProperty.call(pending, m.id)) {
      return;
    }
    var p = pending[m.id];
    delete pending[m.id];
    if (typeof m.error === "string") {
      p.reject(new Error(m.error));
    } else {
      p.resolve(m.result);
    }
  }

  // Theme

  var dark = window.matchMedia("(prefers-color-scheme: dark)");
  var postedTheme = "";

  function applyTheme() {
    var choice = app.settings ? app.settings.theme : "system";
    var effective = choice === "light" || choice === "dark" ? choice : (dark.matches ? "dark" : "light");
    document.documentElement.setAttribute("data-theme", effective);
    if (effective !== postedTheme) {
      postedTheme = effective;
      nextId += 1;
      // Answered by the window process itself, with no reply.
      post({ id: nextId, call: "__theme", args: effective });
    }
  }

  // Helpers

  function el(tag, cls, text) {
    var n = document.createElement(tag);
    if (cls) {
      n.className = cls;
    }
    if (text !== undefined && text !== null) {
      n.textContent = String(text);
    }
    return n;
  }

  function str(v) {
    return typeof v === "string" ? v : "";
  }

  function capital(s) {
    return s ? s.charAt(0).toUpperCase() + s.slice(1) : s;
  }

  function profiles() {
    return app.state && Array.isArray(app.state.profiles) ? app.state.profiles : [];
  }

  function games() {
    return app.state && Array.isArray(app.state.games) ? app.state.games : [];
  }

  function keyboard() {
    return (app.state && app.state.keyboard) || { connected: false, profile: -1 };
  }

  // profileLabel is the name of the KB7 profile with this number, or
  // "Profile N" by its slot.
  function profileLabel(n) {
    var list = profiles();
    for (var i = 0; i < list.length; i++) {
      if (list[i].number === n && str(list[i].name)) {
        return list[i].name;
      }
    }
    return "Profile " + (n + 1);
  }

  function gameName(g) {
    return str(g.name) || str(g.program) || "Game profile";
  }

  // activeGame is the index of the game whose KB7 profile is on the keyboard
  // now, or -1.
  function activeGame() {
    var kb = keyboard();
    if (!kb.connected || typeof kb.profile !== "number" || kb.profile < 0) {
      return -1;
    }
    var list = games();
    for (var i = 0; i < list.length; i++) {
      if (typeof list[i].kb7 === "number" && list[i].kb7 === kb.profile) {
        return i;
      }
    }
    return -1;
  }

  function setNote(key, text, error) {
    app.notes[key] = { text: text, error: !!error };
  }

  function noteLine(key) {
    var n = app.notes[key];
    var p = el("p", "note");
    p.setAttribute("aria-live", "polite");
    if (n) {
      p.textContent = n.text;
      if (n.error) {
        p.classList.add("error");
      }
    }
    return p;
  }

  // run starts a call for the control key: busy until it answers, then the
  // answer goes to done, an error to the line under the control.
  function run(key, name, args, done) {
    if (app.busy[key]) {
      return;
    }
    app.busy[key] = true;
    delete app.notes[key];
    render();
    call(name, args).then(function (result) {
      app.busy[key] = false;
      done(result);
      render();
    }, function (err) {
      app.busy[key] = false;
      setNote(key, err.message, true);
      render();
    });
  }

  function button(key, cls, label, busyLabel, onClick) {
    var b = el("button", cls, app.busy[key] ? busyLabel : label);
    b.type = "button";
    b.setAttribute("data-key", key);
    if (app.busy[key]) {
      b.setAttribute("aria-busy", "true");
      b.setAttribute("aria-disabled", "true");
    }
    b.addEventListener("click", function () {
      if (!app.busy[key]) {
        onClick();
      }
    });
    return b;
  }

  // Top strip

  function renderTop() {
    var st = app.state;
    var kb = keyboard();
    dot.className = "dot";
    if (!st) {
      statusText.textContent = app.stateError ? "Commander did not answer" : "Connecting to Commander";
      return;
    }
    if (st.failed) {
      dot.classList.add("bad");
    } else if (kb.connected) {
      dot.classList.add("ok");
    }
    var text;
    if (kb.connected && !st.failed) {
      text = "Running \u00b7 " + (str(kb.name) || profileLabel(kb.profile));
    } else {
      text = capital(str(st.status));
    }
    statusText.textContent = text;
    statusText.title = text;
  }

  // Sidebar

  function navItem(page, label, sub, subClass) {
    var b = el("button", "nav-item");
    b.type = "button";
    b.setAttribute("data-key", "nav:" + page);
    if (app.page === page) {
      b.setAttribute("aria-current", "page");
    }
    var l = el("span", "label", label);
    l.title = label;
    b.appendChild(l);
    if (sub) {
      var s = el("span", "sub" + (subClass ? " " + subClass : ""), sub);
      s.title = sub;
      b.appendChild(s);
    }
    b.addEventListener("click", function () {
      if (app.page !== page) {
        app.page = page;
        main.scrollTop = 0;
        if (page === "updates" && !app.update) {
          loadUpdate();
        }
        render();
      }
    });
    return b;
  }

  function renderNav() {
    var st = app.state || {};
    var kids = [];
    kids.push(el("h2", "", "Game profiles"));
    var desk = st.desktop || {};
    kids.push(navItem("desktop", str(desk.name) || "Desktop", "default"));
    var active = activeGame();
    games().forEach(function (g, i) {
      kids.push(navItem("game:" + i, gameName(g), i === active ? "active" : ""));
    });

    kids.push(el("h2", "", "Devices"));
    var kb = keyboard();
    if (kb.connected) {
      kids.push(navItem("kb7", "KB7", "connected", "ok"));
    } else {
      kids.push(navItem("kb7", "KB7", str(st.status) || "not connected", "warm"));
    }

    kids.push(el("h2", "", "Commander"));
    kids.push(navItem("settings", "Settings", ""));
    kids.push(navItem("updates", "Updates", str(st.version)));
    nav.replaceChildren.apply(nav, kids);
  }

  // Pages

  function head(title, lead) {
    var out = [el("h1", "", title)];
    if (lead) {
      out.push(el("p", "lead", lead));
    }
    return out;
  }

  var tabs = [
    { id: "buttons", label: "Buttons" },
    { id: "lighting", label: "Lighting" },
    { id: "mixer", label: "Mixer" },
    { id: "profile", label: "Profile" }
  ];

  function kb7Page() {
    var st = app.state || {};
    var out = head("KB7", "Turtle Beach Command Series \u00b7 touchscreen, per-key lighting, volume mixer");
    if (str(st.loadError)) {
      var banner = el("div", "banner");
      banner.setAttribute("role", "alert");
      banner.appendChild(el("strong", "", "The configuration could not be read"));
      banner.appendChild(el("span", "", st.loadError));
      out.push(banner);
    }
    var chips = el("div", "chips");
    var kb = keyboard();
    profiles().forEach(function (p) {
      var c = el("span", "chip");
      if (kb.connected && p.number === kb.profile) {
        c.classList.add("on");
      }
      var name = str(p.name) || "Profile " + (p.number + 1);
      var n = el("span", "name", name);
      n.title = name;
      c.appendChild(n);
      c.appendChild(el("span", "slot", p.number + 1));
      chips.appendChild(c);
    });
    if (chips.childNodes.length > 0) {
      chips.setAttribute("aria-label", "Profiles on the keyboard");
      out.push(chips);
    } else if (!str(st.loadError)) {
      out.push(el("p", "lead", "There are no profiles in the configuration folder yet."));
    }

    var bar = el("div", "tabs");
    bar.setAttribute("role", "tablist");
    tabs.forEach(function (t) {
      var b = el("button", "tab", t.label);
      b.type = "button";
      b.id = "tab-" + t.id;
      b.setAttribute("role", "tab");
      b.setAttribute("data-key", "tab:" + t.id);
      b.setAttribute("aria-controls", "tabpanel");
      b.setAttribute("aria-selected", app.tab === t.id ? "true" : "false");
      b.addEventListener("click", function () {
        app.tab = t.id;
        render();
      });
      bar.appendChild(b);
    });
    out.push(bar);
    var panel = el("div", "tabpanel", "Comes in a later version.");
    panel.id = "tabpanel";
    panel.setAttribute("role", "tabpanel");
    panel.setAttribute("aria-labelledby", "tab-" + app.tab);
    out.push(panel);
    return out;
  }

  function kb7Line(n) {
    if (typeof n !== "number" || n < 0) {
      return "KB7: does not switch";
    }
    return "KB7: " + profileLabel(n) + " (slot " + (n + 1) + ")";
  }

  function gamePage(name, when, kb7) {
    var out = head(name, when);
    var sec = el("section", "section");
    sec.appendChild(el("h2", "", "What each device does"));
    var card = el("div", "card");
    var list = el("ul", "devices");
    list.appendChild(el("li", "", kb7Line(kb7)));
    card.appendChild(list);
    sec.appendChild(card);
    out.push(sec);
    return out;
  }

  function desktopPage() {
    var desk = (app.state && app.state.desktop) || {};
    return gamePage(str(desk.name) || "Desktop", "Shown when no game runs", desk.kb7);
  }

  function gameProfilePage(i) {
    var g = games()[i];
    var when = str(g.program) ? "Switches when " + g.program + " is running" : "Has no program to switch on";
    return gamePage(gameName(g), when, g.kb7);
  }

  function saveSetting(key, args) {
    run(key, "saveSettings", args, function (result) {
      if (result && typeof result === "object") {
        app.settings = result;
        app.settingsError = "";
        applyTheme();
      }
      setNote(key, "Saved", false);
    });
  }

  function toggle(key, label, on, field) {
    var b = el("button", "toggle");
    b.type = "button";
    b.setAttribute("role", "switch");
    b.setAttribute("data-key", key);
    b.setAttribute("aria-checked", on ? "true" : "false");
    if (app.busy[key]) {
      b.setAttribute("aria-busy", "true");
      b.setAttribute("aria-disabled", "true");
    }
    var track = el("span", "track");
    track.appendChild(el("span", "knob"));
    b.appendChild(track);
    b.appendChild(el("span", "", label));
    b.addEventListener("click", function () {
      if (app.busy[key] || !app.settings) {
        return;
      }
      var args = {};
      args[field] = !app.settings[field];
      saveSetting(key, args);
    });
    var f = el("div", "field");
    f.appendChild(b);
    f.appendChild(noteLine(key));
    return f;
  }

  function settingsPage() {
    var out = head("Settings");
    var s = app.settings;
    if (!s) {
      var p = el("p", "note error", app.settingsError ? "Commander could not read the settings: " + app.settingsError : "Loading the settings");
      out.push(p);
      return out;
    }

    var general = el("section", "section");
    general.appendChild(el("h2", "", "General"));
    var card = el("div", "card");
    card.appendChild(toggle("startWithWindows", "Start with Windows", !!s.startWithWindows, "startWithWindows"));
    card.appendChild(toggle("checkUpdates", "Check for updates automatically", !!s.checkUpdates, "checkUpdates"));
    general.appendChild(card);
    out.push(general);

    var look = el("section", "section");
    look.appendChild(el("h2", "", "Theme"));
    var lc = el("div", "card");
    var seg = el("div", "seg");
    seg.setAttribute("role", "group");
    seg.setAttribute("aria-label", "Theme");
    if (app.busy.theme) {
      seg.setAttribute("aria-busy", "true");
    }
    [["system", "Follow Windows"], ["light", "Light"], ["dark", "Dark"]].forEach(function (o) {
      var b = el("button", "", o[1]);
      b.type = "button";
      b.setAttribute("data-key", "theme:" + o[0]);
      b.setAttribute("aria-pressed", s.theme === o[0] ? "true" : "false");
      if (app.busy.theme) {
        b.setAttribute("aria-disabled", "true");
      }
      b.addEventListener("click", function () {
        if (!app.busy.theme && app.settings.theme !== o[0]) {
          saveSetting("theme", { theme: o[0] });
        }
      });
      seg.appendChild(b);
    });
    lc.appendChild(seg);
    lc.appendChild(noteLine("theme"));
    look.appendChild(lc);
    out.push(look);

    var files = el("section", "section");
    files.appendChild(el("h2", "", "Files"));
    var fc = el("div", "card");
    var folder = el("div", "field");
    folder.appendChild(el("p", "label-caps", "Configuration folder"));
    var row = el("div", "row");
    row.appendChild(el("span", "path", str(s.folder)));
    row.appendChild(button("openFolder", "btn", "Open folder", "Opening", function () {
      run("openFolder", "openFolder", undefined, function () {});
    }));
    folder.appendChild(row);
    folder.appendChild(noteLine("openFolder"));
    fc.appendChild(folder);

    var log = el("div", "field");
    log.appendChild(el("p", "label-caps", "Log"));
    var lrow = el("div", "row");
    lrow.appendChild(el("span", "path", str(s.logPath)));
    lrow.appendChild(button("openLog", "btn", "Open log", "Opening", function () {
      run("openLog", "openLog", undefined, function () {});
    }));
    log.appendChild(lrow);
    log.appendChild(noteLine("openLog"));
    fc.appendChild(log);
    files.appendChild(fc);
    out.push(files);
    return out;
  }

  function setUpdate(result) {
    if (result && typeof result === "object") {
      app.update = result;
      app.updateError = "";
    }
  }

  function loadUpdate() {
    call("update").then(function (result) {
      setUpdate(result);
      render();
    }, function (err) {
      app.updateError = err.message;
      render();
    });
  }

  function updatesPage() {
    var st = app.state || {};
    var u = app.update;
    var version = (u && str(u.current)) || str(st.version);
    var out = head("Updates");
    var sec = el("section", "section");
    var card = el("div", "card");
    card.appendChild(el("p", "version", "Commander " + version));

    var dev = u && u.current === "dev";
    if (dev) {
      card.appendChild(el("p", "", "This is a development build; it does not look for updates."));
    }
    card.appendChild(button("checkUpdate", "btn", "Check now", "Checking", function () {
      run("checkUpdate", "checkUpdate", undefined, function (result) {
        setUpdate(result);
        app.checked = true;
        if (!(result && str(result.available)) && !(result && result.current === "dev")) {
          setNote("checkUpdate", "No newer version was found.", false);
        }
      });
    }));
    card.appendChild(noteLine("checkUpdate"));
    if (app.updateError) {
      card.appendChild(el("p", "note error", app.updateError));
    }

    if (u && str(u.available)) {
      var offer = el("div", "offer");
      offer.appendChild(el("p", "", "Commander " + u.available + " is available"));
      offer.appendChild(button("installUpdate", "btn primary", "Update", "Updating", function () {
        run("installUpdate", "installUpdate", undefined, setUpdate);
      }));
      offer.appendChild(noteLine("installUpdate"));
      card.appendChild(offer);
    }
    sec.appendChild(card);
    out.push(sec);
    return out;
  }

  function renderMain() {
    var page = app.page;
    var out;
    if (!app.state) {
      out = [el("p", "note error", app.stateError ? "Commander did not answer: " + app.stateError : "Connecting to Commander")];
    } else if (page === "desktop") {
      out = desktopPage();
    } else if (page.indexOf("game:") === 0 && games()[Number(page.slice(5))]) {
      out = gameProfilePage(Number(page.slice(5)));
    } else if (page === "settings") {
      out = settingsPage();
    } else if (page === "updates") {
      out = updatesPage();
    } else {
      app.page = "kb7";
      out = kb7Page();
    }
    var top = main.scrollTop;
    main.replaceChildren.apply(main, out);
    main.scrollTop = top;
  }

  function render() {
    if (!app.started) {
      return;
    }
    // Keep the focus on the same control across a redraw.
    var focused = document.activeElement;
    var key = focused && focused.getAttribute ? focused.getAttribute("data-key") : null;
    renderTop();
    renderMain();
    renderNav();
    if (key) {
      var again = document.querySelector('[data-key="' + key.replace(/["\\]/g, "") + '"]');
      if (again) {
        again.focus({ preventScroll: true });
      }
    }
  }

  // Start

  if (!view) {
    statusText.textContent = "Open this page from Commander";
    main.replaceChildren(el("p", "note error", "This page only works in Commander's window."));
    return;
  }
  view.addEventListener("message", onMessage);
  dark.addEventListener("change", function () {
    applyTheme();
  });
  applyTheme();

  var gotState = call("state").then(function (result) {
    if (result && typeof result === "object") {
      app.state = result;
    }
  }, function (err) {
    app.stateError = err.message;
  });
  var gotSettings = call("settings").then(function (result) {
    if (result && typeof result === "object") {
      app.settings = result;
      applyTheme();
    }
  }, function (err) {
    app.settingsError = err.message;
  });
  Promise.all([gotState, gotSettings]).then(function () {
    app.started = true;
    render();
  });
  loadUpdate();
})();
