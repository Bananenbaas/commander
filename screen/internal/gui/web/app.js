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
    page: "kb7", // "kb7", "desktop", "game:<id>", "new", "settings", "updates"
    tab: "buttons",
    kb7Profile: -1, // the KB7 profile the Buttons tab shows
    kb7Want: -1, // a profile just made, shown before the state lists it
    leaving: null, // what to do once the unsaved-changes question is answered
    busy: {}, // control key: true while its call runs
    notes: {}, // control key: {text, error}
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
          editors().forEach(function (e) {
            e.ed.refresh();
          });
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
    // A game on the Desktop's own slot cannot be told from the Desktop.
    var desk = app.state && app.state.desktop;
    var deskSlot = desk && typeof desk.kb7 === "number" ? desk.kb7 : -1;
    if (kb.profile === deskSlot) {
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

  // plainError turns an error from Commander into words for the player: the
  // call names in front ("saveSettings: ") go, a plain lead comes first.
  function plainError(lead, err) {
    var text = String(err && err.message ? err.message : err).replace(/^([A-Za-z_]+: )+/, "");
    return lead + ": " + text;
  }

  // leads are the words in front of an error, per control.
  var leads = {
    startWithWindows: "Could not save",
    checkUpdates: "Could not save",
    theme: "Could not save",
    openFolder: "Could not open the folder",
    openLog: "Could not open the log",
    checkUpdate: "Could not check for updates",
    installUpdate: "Could not install the update"
  };

  function setNote(key, text, error) {
    app.notes[key] = { text: text, error: !!error };
  }

  // noteEls keeps one live line per control across redraws, so a screen
  // reader hears it when its text changes; syncNotes sets the texts.
  var noteEls = {};

  function noteLine(key) {
    if (!noteEls[key]) {
      var p = el("p", "note");
      p.setAttribute("aria-live", "polite");
      noteEls[key] = p;
    }
    return noteEls[key];
  }

  function syncNotes() {
    Object.keys(noteEls).forEach(function (key) {
      var p = noteEls[key];
      var n = app.notes[key];
      var text = n ? n.text : "";
      p.classList.toggle("error", !!(n && n.error));
      if (p.textContent !== text) {
        p.textContent = text;
      }
    });
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
      setNote(key, plainError(leads[key] || "Something went wrong", err), true);
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
    var cls = "dot";
    var text;
    if (!st) {
      text = app.stateError ? "Commander did not answer" : "Connecting to Commander";
    } else {
      if (st.failed) {
        cls += " bad";
      } else if (kb.connected) {
        cls += " ok";
      }
      if (kb.connected && !st.failed) {
        text = "Running \u00b7 " + (str(kb.name) || profileLabel(kb.profile));
      } else {
        text = capital(str(st.status));
      }
    }
    if (dot.className !== cls) {
      dot.className = cls;
    }
    // Only a real change, so the status is not announced on every redraw.
    if (statusText.textContent !== text) {
      statusText.textContent = text;
      statusText.title = text;
    }
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
      if (app.page === page) {
        return;
      }
      var go = function () {
        app.page = page;
        main.scrollTop = 0;
        // Fetched anew on every visit: the tray menu and the automatic
        // check change them while the window is open.
        if (page === "settings") {
          loadSettings("settings");
        } else if (page === "updates") {
          loadUpdate("updates");
        }
        render();
      };
      whenSaved(go);
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
      kids.push(navItem("game:" + str(g.id), gameName(g), i === active ? "active" : ""));
    });
    kids.push(navItem("new", "+ New", ""));

    kids.push(el("h2", "", "Devices"));
    var kb = keyboard();
    if (kb.connected) {
      kids.push(navItem("kb7", "KB7", "connected", "ok"));
    } else if (st.failed) {
      kids.push(navItem("kb7", "KB7", capital(str(st.status)) || "Error", "bad"));
    } else {
      kids.push(navItem("kb7", "KB7", capital(str(st.status)) || "Not connected", "warm"));
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

  // The Buttons and Lighting tabs: one editor each for the window's life.
  // Its node is put back in the page on every redraw, never rebuilt, so a
  // field being typed in keeps its focus and text.
  var editor = null;
  var buttonsPanel = el("div", "tabpanel");
  var lighting = null;
  var lightingPanel = el("div", "tabpanel");
  var profileEd = null;
  var profilePanel = el("div", "tabpanel");
  var mixer = null;
  var mixerPanel = el("div", "tabpanel");
  // The game profile pages share one editor the same way.
  var game = null;

  // kb7Editors are the KB7 page's editors there are, with the tab each sits
  // in.
  function kb7Editors() {
    var out = [];
    if (editor) {
      out.push({ ed: editor, tab: "buttons", panel: buttonsPanel, label: "Buttons" });
    }
    if (lighting) {
      out.push({ ed: lighting, tab: "lighting", panel: lightingPanel, label: "Lighting" });
    }
    if (profileEd) {
      out.push({ ed: profileEd, tab: "profile", panel: profilePanel, label: "Profile" });
    }
    return out;
  }

  // editors are all the editors there are. Only the page shown can have
  // changes: leaving a page asks first.
  function editors() {
    var out = kb7Editors();
    if (game) {
      out.push({ ed: game, tab: "", panel: null, label: "Game profile" });
    }
    return out;
  }

  function dirtyEditors() {
    return editors().filter(function (e) {
      return e.ed.dirty();
    });
  }

  // onDirtyChange can come in the middle of a redraw (render shows the
  // editor a profile), so the redraw it asks for waits for that one.
  function onDirtyChange(dirty) {
    // The question stays while another tab still has changes.
    if (!dirty && dirtyEditors().length === 0) {
      app.leaving = null;
    }
    Promise.resolve().then(render);
  }

  // whenSaved runs go now or, while a tab has unsaved changes, once the
  // player has answered the question in the page.
  function whenSaved(go) {
    if (dirtyEditors().length === 0) {
      app.leaving = null;
      go();
      return;
    }
    app.leaving = go;
    render();
    var b = document.querySelector('[data-key="ask:save"]');
    if (b) {
      b.focus();
    }
  }

  // unsavedText names the editors with changes, so Discard says what it
  // drops.
  function unsavedText() {
    var names = dirtyEditors().map(function (e) {
      return e.label;
    });
    if (names.length === 0) {
      return "You have unsaved changes.";
    }
    var list = names.length === 1 ? names[0] :
      names.slice(0, -1).join(", ") + " and " + names[names.length - 1];
    return "You have unsaved changes in " + list + ".";
  }

  // The question asked before leaving unsaved changes. The way out is taken
  // at the click: saving or discarding clears app.leaving on the way.
  function unsavedQuestion() {
    var box = el("div", "banner ask");
    box.setAttribute("role", "group");
    box.setAttribute("aria-label", "Unsaved changes");
    var text = el("p", "", unsavedText());
    text.setAttribute("role", "alert");
    box.appendChild(text);
    var row = el("div", "row");
    row.appendChild(button("ask:save", "btn primary", "Save", "Saving", function () {
      var go = app.leaving;
      app.busy["ask:save"] = true;
      render();
      // Each editor with changes saves in turn; the first that does not go
      // through stops the way out.
      var failed = null;
      dirtyEditors().reduce(function (p, e) {
        return p.then(function () {
          if (failed) {
            return null;
          }
          return e.ed.save().then(function (ok) {
            if (!ok) {
              failed = e;
            }
          });
        });
      }, Promise.resolve()).then(function () {
        app.busy["ask:save"] = false;
        app.leaving = null;
        if (!failed && go) {
          go();
          return;
        }
        // That editor says why.
        if (failed && failed.tab) {
          app.tab = failed.tab;
        }
        render();
      });
    }));
    [["ask:discard", "Discard"], ["ask:keep", "Keep editing"]].forEach(function (o) {
      row.appendChild(button(o[0], "btn", o[1], o[1], function () {
        if (app.busy["ask:save"]) {
          return;
        }
        var go = app.leaving;
        app.leaving = null;
        if (o[0] === "ask:discard") {
          editors().forEach(function (e) {
            e.ed.discard();
          });
          if (go) {
            go();
            return;
          }
        }
        render();
      }));
    });
    box.appendChild(row);
    return box;
  }

  // shownProfile is the KB7 profile for the Buttons tab: one just made or
  // gone to (until the state lists it), else the one picked, else the one on
  // the keyboard, else the lowest.
  function shownProfile() {
    var list = profiles();
    var has = function (n) {
      return list.some(function (p) {
        return p.number === n;
      });
    };
    if (app.kb7Want >= 0) {
      if (!has(app.kb7Want)) {
        return app.kb7Want;
      }
      app.kb7Want = -1;
    }
    if (has(app.kb7Profile)) {
      return app.kb7Profile;
    }
    var kb = keyboard();
    if (kb.connected && has(kb.profile)) {
      return kb.profile;
    }
    var low = -1;
    list.forEach(function (p) {
      if (typeof p.number === "number" && (low < 0 || p.number < low)) {
        low = p.number;
      }
    });
    return low;
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
      banner.appendChild(el("strong", "", "The configuration could not be read"));
      banner.appendChild(el("span", "", st.loadError));
      out.push(banner);
    }
    if (!str(st.loadError)) {
      // While the folder does not load the list is empty; keep the profile
      // (and a draft in it) until it loads again.
      var shown = shownProfile();
      if (!(dirtyEditors().length > 0 && app.kb7Profile >= 0)) {
        app.kb7Profile = shown;
      }
      kb7Editors().forEach(function (e) {
        e.ed.show(app.kb7Profile);
      });
    }
    var chips = el("div", "chips");
    var kb = keyboard();
    profiles().forEach(function (p) {
      var live = kb.connected && p.number === kb.profile;
      var c = el("button", "chip" + (live ? " live" : ""));
      c.type = "button";
      c.setAttribute("data-key", "profile:" + p.number);
      c.setAttribute("aria-pressed", p.number === app.kb7Profile ? "true" : "false");
      var name = str(p.name) || "Profile " + (p.number + 1);
      var n = el("span", "name", name);
      n.title = live ? name + ", on the keyboard now" : name;
      c.appendChild(n);
      c.appendChild(el("span", "slot", p.number + 1));
      if (live) {
        c.appendChild(el("span", "sr-only", ", on the keyboard now"));
      }
      c.addEventListener("click", function () {
        if (p.number === app.kb7Profile) {
          return;
        }
        whenSaved(function () {
          app.kb7Want = -1;
          app.kb7Profile = p.number;
          kb7Editors().forEach(function (e) {
            e.ed.show(p.number);
          });
          render();
        });
      });
      chips.appendChild(c);
    });
    if (chips.childNodes.length > 0) {
      chips.setAttribute("role", "group");
      chips.setAttribute("aria-label", "Profiles on the keyboard");
      out.push(chips);
    } else if (!str(st.loadError)) {
      out.push(el("p", "lead", "There are no profiles in the configuration folder yet."));
    }
    if (app.leaving) {
      out.push(unsavedQuestion());
    }

    var bar = el("div", "tabs");
    bar.setAttribute("role", "tablist");
    bar.setAttribute("aria-label", "KB7");
    // One tab stop for the row; the arrow keys move between the tabs.
    function selectTab(id, focus) {
      app.tab = id;
      // The Profile tab shows the file as it is now: a save on another tab
      // changes it.
      if (id === "profile" && profileEd) {
        profileEd.refresh();
      }
      render();
      if (focus) {
        var b = document.getElementById("tab-" + id);
        if (b) {
          b.focus();
        }
      }
    }
    tabs.forEach(function (t, i) {
      var b = el("button", "tab", t.label);
      var selected = app.tab === t.id;
      b.type = "button";
      b.id = "tab-" + t.id;
      b.setAttribute("role", "tab");
      b.setAttribute("data-key", "tab:" + t.id);
      b.setAttribute("aria-controls", "tabpanel");
      b.setAttribute("aria-selected", selected ? "true" : "false");
      b.tabIndex = selected ? 0 : -1;
      b.addEventListener("click", function () {
        selectTab(t.id, false);
      });
      b.addEventListener("keydown", function (e) {
        var to = -1;
        if (e.key === "ArrowRight") {
          to = (i + 1) % tabs.length;
        } else if (e.key === "ArrowLeft") {
          to = (i + tabs.length - 1) % tabs.length;
        } else if (e.key === "Home") {
          to = 0;
        } else if (e.key === "End") {
          to = tabs.length - 1;
        }
        if (to >= 0) {
          e.preventDefault();
          selectTab(tabs[to].id, true);
        }
      });
      bar.appendChild(b);
    });
    out.push(bar);
    var panel;
    var shownEditor = null;
    kb7Editors().forEach(function (e) {
      if (e.tab === app.tab) {
        shownEditor = e;
      }
    });
    var what = { buttons: "The buttons", lighting: "The lighting", profile: "The profile" }[app.tab];
    if (app.tab === "mixer") {
      // The mixer follows Windows, not the configuration folder.
      if (mixer) {
        panel = mixerPanel;
        if (panel.firstChild !== mixer.node) {
          panel.replaceChildren(mixer.node);
        }
      } else {
        panel = el("div", "tabpanel");
        panel.appendChild(el("p", "note error", "The Mixer tab could not be loaded."));
      }
    } else if (str(st.loadError)) {
      panel = el("div", "tabpanel");
      panel.appendChild(el("p", "note error", what + " cannot be shown while the configuration cannot be read: " + st.loadError));
    } else if (!shownEditor || app.kb7Profile < 0) {
      panel = el("div", "tabpanel", "There are no profiles to show.");
    } else {
      panel = shownEditor.panel;
      if (panel.firstChild !== shownEditor.ed.node) {
        panel.replaceChildren(shownEditor.ed.node);
      }
    }
    panel.id = "tabpanel";
    panel.setAttribute("role", "tabpanel");
    panel.setAttribute("aria-labelledby", "tab-" + app.tab);
    out.push(panel);
    return out;
  }

  // pageOf is the page of the game with this id: "desktop", a game's id,
  // or "" for a new one.
  function pageOf(id) {
    if (id === "desktop") {
      return "desktop";
    }
    return id === "" ? "new" : "game:" + id;
  }

  // gameIdOf is the id of the game a page shows, or null when it shows
  // none. A game's page stays while the editor holds that game, so a save
  // that gives it a new id, or a state event that has not caught up, does
  // not close it.
  function gameIdOf(page) {
    if (page === "desktop") {
      return "desktop";
    }
    if (page === "new") {
      return "";
    }
    if (page.indexOf("game:") !== 0) {
      return null;
    }
    var id = page.slice(5);
    var known = games().some(function (g) {
      return str(g.id) === id;
    });
    return known || (game && game.shows() === id) ? id : null;
  }

  function gamePage(id) {
    if (!game) {
      return [el("p", "note error", "The game profile pages could not be loaded.")];
    }
    game.show(id);
    var out = [game.head];
    if (app.leaving) {
      out.push(unsavedQuestion());
    }
    out.push(game.node);
    return out;
  }

  // openProfile opens the KB7 page on the Buttons tab of profile n, for
  // the Edit link of a game profile.
  function openProfile(n) {
    whenSaved(function () {
      app.page = "kb7";
      app.tab = "buttons";
      app.kb7Want = -1;
      app.kb7Profile = n;
      main.scrollTop = 0;
      render();
    });
  }

  // gameSaved follows a game whose save gave it another id.
  function gameSaved(oldId, newId) {
    if (app.page === pageOf(oldId)) {
      app.page = pageOf(newId);
    }
    render();
  }

  // profileMade shows the profile a duplicate made, on the Profile tab.
  function profileMade(n) {
    app.kb7Want = n;
    app.kb7Profile = n;
    app.tab = "profile";
    kb7Editors().forEach(function (e) {
      e.ed.show(n);
    });
    render();
  }

  // profileGone goes to the lowest profile left after profile n was
  // deleted; what was unsaved in n went with it.
  function profileGone(n) {
    var low = -1;
    profiles().forEach(function (p) {
      if (typeof p.number === "number" && p.number !== n && (low < 0 || p.number < low)) {
        low = p.number;
      }
    });
    app.leaving = null;
    kb7Editors().forEach(function (e) {
      e.ed.discard();
    });
    app.kb7Want = low;
    app.kb7Profile = low;
    kb7Editors().forEach(function (e) {
      e.ed.show(low);
    });
    render();
  }

  function gameDeleted() {
    app.leaving = null;
    app.page = "desktop";
    main.scrollTop = 0;
    render();
  }

  // settingsGen and updateGen count the answers that set app.settings and
  // app.update, so a fetch that was overtaken by a newer answer is ignored.
  var settingsGen = 0;
  var updateGen = 0;

  function setSettings(result) {
    if (result && typeof result === "object") {
      settingsGen += 1;
      app.settings = result;
      app.settingsError = "";
      applyTheme();
    }
  }

  // loadSettings fetches the settings. With onPage, the answer counts only
  // while that page is still open.
  function loadSettings(onPage) {
    var gen = settingsGen;
    return call("settings").then(function (result) {
      if ((onPage && app.page !== onPage) || gen !== settingsGen) {
        return;
      }
      setSettings(result);
      render();
    }, function (err) {
      if ((onPage && app.page !== onPage) || gen !== settingsGen) {
        return;
      }
      app.settingsError = err.message;
      render();
    });
  }

  function saveSetting(key, args) {
    run(key, "saveSettings", args, function (result) {
      setSettings(result);
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
      var p = el("p", "note error", app.settingsError ? plainError("Could not read the settings", app.settingsError) : "Loading the settings");
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
      updateGen += 1;
      app.update = result;
      app.updateError = "";
    }
  }

  // loadUpdate fetches what is on offer. With onPage, the answer counts
  // only while that page is still open.
  function loadUpdate(onPage) {
    var gen = updateGen;
    call("update").then(function (result) {
      if ((onPage && app.page !== onPage) || gen !== updateGen) {
        return;
      }
      setUpdate(result);
      render();
    }, function (err) {
      if ((onPage && app.page !== onPage) || gen !== updateGen) {
        return;
      }
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
    if (!dev) {
      card.appendChild(button("checkUpdate", "btn", "Check now", "Checking", function () {
        run("checkUpdate", "checkUpdate", undefined, function (result) {
          setUpdate(result);
          if (!(result && str(result.available)) && !(result && result.current === "dev")) {
            setNote("checkUpdate", "No newer version was found.", false);
          }
        });
      }));
      card.appendChild(noteLine("checkUpdate"));
    }
    if (app.updateError) {
      card.appendChild(el("p", "note error", plainError("Could not check for updates", app.updateError)));
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
      out = [el("p", "note error", app.stateError ? plainError("Commander did not answer", app.stateError) : "Connecting to Commander")];
    } else if (gameIdOf(page) !== null) {
      out = gamePage(gameIdOf(page));
    } else if (page === "settings") {
      out = settingsPage();
    } else if (page === "updates") {
      out = updatesPage();
    } else {
      app.page = "kb7";
      out = kb7Page();
    }
    var top = main.scrollTop;
    replaceKeeping(main, out);
    main.scrollTop = top;
    // Keys are recorded only while their tab is shown.
    kb7Editors().forEach(function (e) {
      if (out.indexOf(e.panel) < 0) {
        e.ed.stopRecording();
      }
    });
    pollMixer(out.indexOf(mixerPanel) >= 0);
  }

  // pollMixer lets the Mixer tab ask for the volumes while it shows and the
  // window can be seen.
  function pollMixer(shown) {
    if (!mixer) {
      return;
    }
    if (shown && document.visibilityState === "visible") {
      mixer.start();
    } else {
      mixer.stop();
    }
  }

  // replaceKeeping makes nodes the children of parent. A node already in
  // parent stays where it is instead of being taken out and put back, so
  // the focus inside it survives.
  function replaceKeeping(parent, nodes) {
    var keep = new Set(nodes);
    Array.prototype.slice.call(parent.childNodes).forEach(function (c) {
      if (!keep.has(c)) {
        parent.removeChild(c);
      }
    });
    var at = parent.firstChild;
    nodes.forEach(function (n) {
      if (n === at) {
        at = at.nextSibling;
      } else {
        parent.insertBefore(n, at);
      }
    });
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
    syncNotes();
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
  if (window.CommanderButtons) {
    editor = window.CommanderButtons.create({
      call: call, el: el, str: str, plainError: plainError, onDirtyChange: onDirtyChange
    });
  }
  if (window.CommanderLighting) {
    lighting = window.CommanderLighting.create({
      call: call, el: el, str: str, plainError: plainError, onDirtyChange: onDirtyChange
    });
  }
  if (window.CommanderGames) {
    game = window.CommanderGames.create({
      call: call, el: el, plainError: plainError, onDirtyChange: onDirtyChange,
      onSaved: gameSaved, onDeleted: gameDeleted, openProfile: openProfile
    });
  }
  if (window.CommanderProfile) {
    mixer = window.CommanderProfile.createMixer({ call: call, el: el, plainError: plainError });
    profileEd = window.CommanderProfile.create({
      call: call, el: el, plainError: plainError, onDirtyChange: onDirtyChange,
      whenSaved: whenSaved, onDuplicated: profileMade, onDeleted: profileGone
    });
  }
  document.addEventListener("visibilitychange", function () {
    if (app.started) {
      pollMixer(app.page === "kb7" && app.tab === "mixer" && mixerPanel.isConnected);
    }
  });
  window.addEventListener("focus", function () {
    if (app.started) {
      editors().forEach(function (e) {
        e.ed.refresh();
      });
    }
  });

  var gotState = call("state").then(function (result) {
    // A "state" event may have come first: it is newer than this answer.
    if (result && typeof result === "object" && !app.state) {
      app.state = result;
    }
  }, function (err) {
    app.stateError = err.message;
  });
  var gotSettings = loadSettings("");
  Promise.all([gotState, gotSettings]).then(function () {
    app.started = true;
    render();
  });
  loadUpdate();
})();
