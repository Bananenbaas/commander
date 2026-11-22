/* SPDX-License-Identifier: GPL-2.0-or-later */
"use strict";

// The Buttons tab of the KB7 page: the grid of one KB7 profile's buttons,
// drawn by Commander's own renderer, and the draft the player edits until
// Save. Built with createElement and textContent only.

(function () {
  var COLUMNS = 3;
  var PLACES = 12; // three columns, four rows, counted row by row from 0
  var PAGES = [1, 2, 3];
  var NEW_COLOR = "#00AAFF";

  // Draft logic: plain functions over lists of buttons, no page involved.

  function copy(v) {
    return JSON.parse(JSON.stringify(v));
  }

  // hasPlace tells whether a page shows this position (page 3 has no 11).
  function hasPlace(page, position) {
    return position >= 0 && position < PLACES && !(page === 3 && position === PLACES - 1);
  }

  function find(buttons, page, position) {
    for (var i = 0; i < buttons.length; i++) {
      if (buttons[i].page === page && buttons[i].position === position) {
        return buttons[i];
      }
    }
    return null;
  }

  function newButton(page, position) {
    return {
      page: page, position: position, title: "", label: "", color: NEW_COLOR,
      symbol: "", picture: "", steps: [], hold: false, confirmMs: 0, state: ""
    };
  }

  // move puts the button at from onto to: an empty place takes it, another
  // button swaps places with it. It returns a new list; nothing to move or
  // a place the page does not show leaves the list as it was.
  function move(buttons, from, to) {
    var out = copy(buttons);
    if (!hasPlace(to.page, to.position) || (from.page === to.page && from.position === to.position)) {
      return out;
    }
    var a = find(out, from.page, from.position);
    if (!a) {
      return out;
    }
    var b = find(out, to.page, to.position);
    if (b) {
      b.page = from.page;
      b.position = from.position;
    }
    a.page = to.page;
    a.position = to.position;
    return out;
  }

  function sorted(buttons) {
    return buttons.slice().sort(function (x, y) {
      return x.page - y.page || x.position - y.position;
    });
  }

  // canonical writes a value with its object keys in order, so two buttons
  // that hold the same compare equal whatever order their keys came in.
  function canonical(v) {
    if (Array.isArray(v)) {
      return "[" + v.map(canonical).join(",") + "]";
    }
    if (v && typeof v === "object") {
      return "{" + Object.keys(v).sort().map(function (k) {
        return JSON.stringify(k) + ":" + canonical(v[k]);
      }).join(",") + "}";
    }
    return JSON.stringify(v === undefined ? null : v);
  }

  function same(a, b) {
    return canonical(sorted(a)) === canonical(sorted(b));
  }

  // lookKey is what the drawing of a button depends on.
  function lookKey(b) {
    return canonical([b.title, b.label, b.color, b.symbol, b.picture]);
  }

  // saveNotes are the lines after a save that went through.
  function saveNotes(res) {
    var lines = ["Saved."];
    if (res.change === "apply") {
      lines.push("Putting the buttons on the KB7");
    } else if (res.change === "reload") {
      lines.push("Commander uses the new steps now");
    }
    var backups = Array.isArray(res.backups) ? res.backups : [];
    if (backups.length > 0) {
      lines.push("Your original file was copied to " + backups.join(", ") + " before saving; Commander does not keep comments.");
    }
    if (res.migrated) {
      lines.push("Programs that switch profiles are now game profiles in the sidebar.");
    }
    return lines;
  }

  // Keys

  // KEY_CODES maps KeyboardEvent.code to Commander's key name. A code that
  // is not here is a key Commander cannot press; recording skips it.
  var KEY_CODES = (function () {
    var t = {};
    var i;
    for (i = 0; i < 26; i++) {
      t["Key" + String.fromCharCode(65 + i)] = String.fromCharCode(65 + i);
    }
    for (i = 0; i <= 9; i++) {
      t["Digit" + i] = String(i);
      t["Numpad" + i] = "Num" + i;
    }
    for (i = 1; i <= 24; i++) {
      t["F" + i] = "F" + i;
    }
    var named = {
      NumpadAdd: "NumPlus", NumpadSubtract: "NumMinus", NumpadMultiply: "NumMultiply",
      NumpadDivide: "NumDivide", NumpadDecimal: "NumDecimal", NumpadEnter: "NumEnter",
      ControlLeft: "LCtrl", ShiftLeft: "LShift", AltLeft: "LAlt", MetaLeft: "LWin",
      ControlRight: "RCtrl", ShiftRight: "RShift", AltRight: "RAlt", MetaRight: "RWin",
      Equal: "Equals", BracketLeft: "LeftBracket", BracketRight: "RightBracket",
      Backquote: "Grave", ArrowUp: "Up", ArrowDown: "Down", ArrowLeft: "Left",
      ArrowRight: "Right", IntlBackslash: "NonUSBackslash",
      AudioVolumeUp: "VolumeUp", AudioVolumeDown: "VolumeDown", AudioVolumeMute: "Mute",
      MediaPlayPause: "PlayPause", MediaTrackNext: "NextTrack",
      MediaTrackPrevious: "PrevTrack", MediaStop: "StopMedia"
    };
    Object.keys(named).forEach(function (k) {
      t[k] = named[k];
    });
    ["Enter", "Escape", "Backspace", "Tab", "Space", "Minus", "Backslash", "Semicolon",
      "Quote", "Comma", "Period", "Slash", "CapsLock", "ScrollLock", "Pause", "Insert",
      "Home", "PageUp", "Delete", "End", "PageDown", "NumLock"].forEach(function (n) {
      t[n] = n;
    });
    return t;
  })();

  // KEY_LIST is every key Commander can press, in the order the key picker
  // lists them.
  var KEY_LIST = (function () {
    var out = ["LCtrl", "LShift", "LAlt", "LWin", "RCtrl", "RShift", "RAlt", "RWin"];
    var i;
    for (i = 0; i < 26; i++) {
      out.push(String.fromCharCode(65 + i));
    }
    for (i = 1; i <= 10; i++) {
      out.push(String(i % 10));
    }
    for (i = 1; i <= 24; i++) {
      out.push("F" + i);
    }
    out = out.concat(["Enter", "Escape", "Backspace", "Tab", "Space", "Minus", "Equals",
      "LeftBracket", "RightBracket", "Backslash", "NonUSBackslash", "Semicolon", "Quote",
      "Grave", "Comma", "Period", "Slash", "CapsLock", "PrintScreen", "ScrollLock", "Pause",
      "Insert", "Delete", "Home", "End", "PageUp", "PageDown", "Up", "Down", "Left", "Right",
      "NumLock"]);
    for (i = 0; i <= 9; i++) {
      out.push("Num" + i);
    }
    return out.concat(["NumPlus", "NumMinus", "NumMultiply", "NumDivide", "NumDecimal",
      "NumEnter", "VolumeUp", "VolumeDown", "Mute", "PlayPause", "NextTrack", "PrevTrack",
      "StopMedia"]);
  })();

  // CAPS are the key caps that do not read as their name: [text, tooltip].
  var CAPS = {
    NumPlus: ["Num +"], NumMinus: ["Num -"], NumMultiply: ["Num *"], NumDivide: ["Num /"],
    NumDecimal: ["Num ."], NumEnter: ["Num Enter"], NumLock: ["Num Lock"],
    PageUp: ["Page Up"], PageDown: ["Page Down"], CapsLock: ["Caps Lock"],
    ScrollLock: ["Scroll Lock"], PrintScreen: ["Print Screen"], Escape: ["Esc", "Escape"],
    Minus: ["-", "Minus"], Equals: ["=", "Equals"], LeftBracket: ["[", "Left bracket"],
    RightBracket: ["]", "Right bracket"], Backslash: ["\\", "Backslash"],
    NonUSBackslash: ["\\", "Extra backslash key next to Shift"], Semicolon: [";", "Semicolon"],
    Quote: ["'", "Quote"], Grave: ["`", "Grave accent"], Comma: [",", "Comma"],
    Period: [".", "Period"], Slash: ["/", "Slash"], VolumeUp: ["Volume Up"],
    VolumeDown: ["Volume Down"], PlayPause: ["Play/Pause"], NextTrack: ["Next Track"],
    PrevTrack: ["Previous Track"], StopMedia: ["Stop", "Stop media"]
  };

  // keyCap is how a key name is shown: the text on the cap and a tooltip.
  function keyCap(name) {
    var m = /^([LR])(Ctrl|Shift|Alt|Win)$/.exec(name);
    if (m) {
      return { text: m[2], title: m[1] === "R" ? "Right " + m[2] : "" };
    }
    m = /^Num(\d)$/.exec(name);
    if (m) {
      return { text: "Num " + m[1], title: "" };
    }
    if (CAPS[name]) {
      return { text: CAPS[name][0], title: CAPS[name][1] || "" };
    }
    return { text: name, title: "" };
  }

  // keyWords names a key in a list, where the side of a modifier counts.
  function keyWords(name) {
    var m = /^L(Ctrl|Shift|Alt|Win)$/.exec(name);
    if (m) {
      return "Left " + m[1];
    }
    var cap = keyCap(name);
    return cap.title && cap.title.indexOf(" ") > 0 ? cap.title : cap.text;
  }

  // Recording keys: a chord is the keys held together, ending when all are
  // released; a pause of PAUSE_MIN or more before the next key is a wait.

  var PAUSE_MIN = 100;
  var WAIT_MAX = 60000;

  function newRecording() {
    return { held: [], chord: [], lastUp: null, skipped: [] };
  }

  // recordDown takes a key going down at time t (ms) and returns the steps
  // it finishes: a wait for the pause before it, if any.
  function recordDown(rec, code, t) {
    var name = KEY_CODES[code];
    if (!name) {
      var what = code || "a key without a name";
      if (rec.skipped.indexOf(what) < 0) {
        rec.skipped.push(what);
      }
      return [];
    }
    if (rec.held.indexOf(code) >= 0) {
      return []; // held down: the key repeats
    }
    var out = [];
    if (rec.held.length === 0 && rec.lastUp !== null) {
      var pause = t - rec.lastUp;
      if (pause >= PAUSE_MIN) {
        out.push({ waitMs: Math.min(WAIT_MAX, Math.round(pause / 10) * 10) });
      }
    }
    rec.held.push(code);
    if (rec.chord.indexOf(name) < 0) {
      rec.chord.push(name);
    }
    return out;
  }

  // recordUp takes a key going up and returns the chord it finishes, if it
  // was the last key held.
  function recordUp(rec, code, t) {
    var i = rec.held.indexOf(code);
    if (i < 0) {
      return [];
    }
    rec.held.splice(i, 1);
    if (rec.held.length > 0) {
      return [];
    }
    var out = [{ keys: rec.chord }];
    rec.chord = [];
    rec.lastUp = t;
    return out;
  }

  // recordEnd returns the chord still held when recording stops.
  function recordEnd(rec) {
    var out = rec.chord.length > 0 ? [{ keys: rec.chord }] : [];
    rec.chord = [];
    rec.held = [];
    return out;
  }

  // Steps

  function stepKind(s) {
    if (Array.isArray(s.keys)) {
      return "keys";
    }
    if (s.waitMs !== undefined && s.waitMs !== null) {
      return "wait";
    }
    if (typeof s.run === "string") {
      return "run";
    }
    if (typeof s.open === "string") {
      return "open";
    }
    return "";
  }

  // splitArgs reads the arguments field: words split by spaces, a quoted
  // part is one word.
  function splitArgs(text) {
    var out = [];
    var cur = "";
    var quoted = false;
    var has = false;
    for (var i = 0; i < text.length; i++) {
      var ch = text.charAt(i);
      if (ch === "\"") {
        quoted = !quoted;
        has = true;
      } else if (!quoted && /\s/.test(ch)) {
        if (has) {
          out.push(cur);
          cur = "";
          has = false;
        }
      } else {
        cur += ch;
        has = true;
      }
    }
    if (has) {
      out.push(cur);
    }
    return out;
  }

  function joinArgs(args) {
    return (Array.isArray(args) ? args : []).map(function (a) {
      a = String(a);
      return a === "" || /[\s"]/.test(a) ? "\"" + a.replace(/"/g, "") + "\"" : a;
    }).join(" ");
  }

  // holdAllowed: Hold while touched needs exactly one Press keys step.
  function holdAllowed(b) {
    return b.steps.length === 1 && stepKind(b.steps[0]) === "keys";
  }

  // tidy drops the options the steps no longer allow.
  function tidy(b) {
    if (b.hold && !holdAllowed(b)) {
      b.hold = false;
    }
    if (b.steps.length === 0) {
      b.confirmMs = 0;
    }
  }

  // pageChecks are the checks the page makes before it asks Commander to
  // save, in the shape of Commander's own field errors.
  function pageChecks(buttons) {
    var out = [];
    sorted(buttons).forEach(function (b) {
      function bad(field, step, message) {
        out.push({ page: b.page, position: b.position, field: field, step: step, message: message });
      }
      var steps = Array.isArray(b.steps) ? b.steps : [];
      if (steps.length === 0 && !b.state) {
        bad("action", -1, "Add at least one step, or pick a lighting state");
      }
      steps.forEach(function (s, i) {
        var kind = stepKind(s);
        if (kind === "keys" && s.keys.length === 0) {
          bad("action", i, "Pick the keys for this step.");
        } else if (kind === "wait" && !(s.waitMs >= 1 && s.waitMs <= WAIT_MAX && Math.floor(s.waitMs) === s.waitMs)) {
          bad("action", i, "A wait is 1 to 60000 ms.");
        } else if (kind === "run" && !s.run.trim()) {
          bad("action", i, "Type the program to start.");
        } else if (kind === "open" && !s.open.trim()) {
          bad("action", i, "Type the website, file or folder to open.");
        }
      });
    });
    return out;
  }

  var logic = {
    hasPlace: hasPlace, find: find, newButton: newButton, move: move,
    same: same, sorted: sorted, saveNotes: saveNotes,
    KEY_CODES: KEY_CODES, KEY_LIST: KEY_LIST, keyCap: keyCap, keyWords: keyWords,
    newRecording: newRecording, recordDown: recordDown, recordUp: recordUp, recordEnd: recordEnd,
    stepKind: stepKind, splitArgs: splitArgs, joinArgs: joinArgs, holdAllowed: holdAllowed,
    tidy: tidy, pageChecks: pageChecks
  };

  var PALETTE = ["#00AAFF", "#FF9A3D", "#3DDC84", "#FF4D6D", "#B388FF", "#FFD23F", "#E8EDF7", "#10172A"];
  var LOOK_PAUSE = 150; // ms after typing stops before the grid draws again
  var RECORD_IDLE = 10000; // ms without a key that ends a recording

  // The editor

  function create(api) {
    var el = api.el;
    var str = api.str;

    var ed = {
      profile: -1,
      saved: null, // the deviceProfile answer the draft started from
      draft: [],
      page: 1,
      selected: -1, // position on ed.page, or -1
      loadError: "",
      saving: null, // the Promise of a save that runs
      notes: [], // lines after a save
      barError: "",
      changed: false, // the file changed outside Commander since it was read
      errors: {}, // "page:position" -> [{field, step, message}]
      gen: 0, // counts profile loads, so an overtaken answer is ignored
      wasDirty: false
    };

    var pngs = {}; // lookKey -> {src} or {error}
    var drawing = {}; // lookKey -> true while renderButton runs
    var dragFrom = -1;

    // Nodes, made once and kept: a redraw only changes them, so focus and
    // a drag in progress survive.

    var node = el("div", "bedit");

    var bar = el("div", "bbar");
    bar.hidden = true;
    var barText = el("p", "bbar-text");
    barText.setAttribute("aria-live", "polite");
    var barActs = el("div", "bbar-acts");
    var saveBtn = el("button", "btn primary", "Save");
    saveBtn.type = "button";
    saveBtn.addEventListener("click", function () {
      save();
    });
    var discardBtn = el("button", "btn", "Discard");
    discardBtn.type = "button";
    discardBtn.addEventListener("click", function () {
      if (!ed.saving) {
        discard();
      }
    });
    barActs.appendChild(saveBtn);
    barActs.appendChild(discardBtn);
    bar.appendChild(barText);
    bar.appendChild(barActs);

    var problem = el("p", "note error");
    problem.hidden = true;

    var work = el("div", "bwork");
    var left = el("div", "bleft");

    var pageChips = el("div", "chips pages");
    pageChips.setAttribute("role", "group");
    pageChips.setAttribute("aria-label", "Pages");
    var pageBtns = PAGES.map(function (p) {
      var b = el("button", "chip", String(p));
      b.type = "button";
      b.setAttribute("aria-label", "Page " + p);
      b.addEventListener("click", function () {
        showPage(p);
      });
      pageChips.appendChild(b);
      return b;
    });

    var grid = el("div", "bgrid");
    grid.setAttribute("role", "group");
    var places = [];
    for (var i = 0; i < PLACES; i++) {
      places.push(makePlace(i));
      grid.appendChild(places[i].node);
    }

    var pictures = el("p", "note bpictures");

    left.appendChild(pageChips);
    left.appendChild(grid);
    left.appendChild(pictures);

    // The edit panel to the right of the grid. panelShow is the one place
    // that hears of a change of the selected button.
    var panel = el("div", "bpanel");
    panel.setAttribute("aria-label", "Selected button");

    work.appendChild(left);
    work.appendChild(panel);
    node.appendChild(bar);
    node.appendChild(problem);
    node.appendChild(work);

    function makePlace(position) {
      var b = el("button", "bplace");
      b.type = "button";
      var img = el("img");
      img.alt = "";
      img.draggable = false;
      var plus = el("span", "bplus", "+");
      plus.setAttribute("aria-hidden", "true");
      var mark = el("span", "bmark");
      b.appendChild(img);
      b.appendChild(plus);
      b.appendChild(mark);
      b.addEventListener("click", function () {
        pick(position, false);
      });
      b.addEventListener("keydown", function (e) {
        onPlaceKey(e, position);
      });
      b.addEventListener("dragstart", function (e) {
        if (!current(position)) {
          e.preventDefault();
          return;
        }
        dragFrom = position;
        e.dataTransfer.effectAllowed = "move";
        e.dataTransfer.setData("text/plain", String(position));
        b.classList.add("dragging");
      });
      b.addEventListener("dragend", function () {
        dragFrom = -1;
        b.classList.remove("dragging");
        places.forEach(function (p) {
          p.node.classList.remove("drop");
        });
      });
      b.addEventListener("dragover", function (e) {
        if (dragFrom < 0) {
          return;
        }
        e.preventDefault();
        e.dataTransfer.dropEffect = "move";
        b.classList.toggle("drop", dragFrom !== position);
      });
      b.addEventListener("dragleave", function () {
        b.classList.remove("drop");
      });
      b.addEventListener("drop", function (e) {
        e.preventDefault();
        b.classList.remove("drop");
        if (dragFrom < 0 || dragFrom === position) {
          return;
        }
        var from = dragFrom;
        dragFrom = -1;
        ed.draft = move(ed.draft, { page: ed.page, position: from }, { page: ed.page, position: position });
        ed.selected = position;
        // The problems of the last save named places; those moved.
        ed.errors = {};
        changed();
      });
      return { node: b, img: img, mark: mark };
    }

    function current(position) {
      return find(ed.draft, ed.page, position);
    }

    function selectedButton() {
      return ed.selected >= 0 ? current(ed.selected) : null;
    }

    // pick selects a place; an empty place gets a new button. With open,
    // the edit panel takes the focus.
    function pick(position, open) {
      if (!ed.saved || !hasPlace(ed.page, position)) {
        return;
      }
      if (!current(position)) {
        ed.draft.push(newButton(ed.page, position));
        ed.selected = position;
        changed();
      } else if (ed.selected !== position) {
        ed.selected = position;
        draw();
      }
      if (open) {
        panelOpen();
      }
    }

    function onPlaceKey(e, position) {
      var row = Math.floor(position / COLUMNS);
      var col = position % COLUMNS;
      var to = -1;
      if (e.key === "ArrowLeft" && col > 0) {
        to = position - 1;
      } else if (e.key === "ArrowRight" && col < COLUMNS - 1) {
        to = position + 1;
      } else if (e.key === "ArrowUp" && row > 0) {
        to = position - COLUMNS;
      } else if (e.key === "ArrowDown" && position + COLUMNS < PLACES) {
        to = position + COLUMNS;
      } else if (e.key === "Enter") {
        e.preventDefault();
        pick(position, true);
        return;
      } else if (e.key.indexOf("Arrow") !== 0) {
        return;
      }
      e.preventDefault();
      if (to >= 0 && hasPlace(ed.page, to)) {
        ed.selected = to;
        draw();
        places[to].node.focus();
      }
    }

    function showPage(p) {
      if (ed.page === p) {
        return;
      }
      ed.page = p;
      ed.selected = -1;
      draw();
    }

    // Drawing

    function render(b) {
      var key = lookKey(b);
      if (pngs[key] || drawing[key]) {
        return;
      }
      drawing[key] = true;
      var look = newButton(b.page, b.position);
      look.title = b.title;
      look.label = b.label;
      look.color = b.color;
      look.symbol = b.symbol;
      look.picture = b.picture;
      api.call("renderButton", { button: look }).then(function (res) {
        delete drawing[key];
        pngs[key] = { src: res && typeof res.png === "string" ? res.png : "" };
        drawGrid();
      }, function (err) {
        delete drawing[key];
        pngs[key] = { error: api.plainError("Cannot draw this button", err) };
        drawGrid();
      });
    }

    function drawGrid() {
      grid.setAttribute("aria-label", "Buttons on page " + ed.page);
      var focusAt = ed.selected >= 0 ? ed.selected : 0;
      places.forEach(function (p, position) {
        var n = p.node;
        var shown = hasPlace(ed.page, position);
        n.hidden = !shown;
        var b = shown ? current(position) : null;
        var row = Math.floor(position / COLUMNS) + 1;
        var col = (position % COLUMNS) + 1;
        var where = "Row " + row + ", place " + col;
        // While the player types, the place keeps the look it had.
        var held = b && lookHold && lookHold.page === b.page && lookHold.position === b.position;
        var drawn = b ? pngs[held ? lookHold.key : lookKey(b)] : null;
        if (b && !held) {
          render(b);
        }
        n.classList.toggle("empty", !b);
        n.classList.toggle("broken", !!(drawn && drawn.error));
        n.classList.toggle("flagged", !!(b && ed.errors[b.page + ":" + b.position]));
        n.draggable = !!b;
        n.setAttribute("aria-pressed", ed.selected === position ? "true" : "false");
        n.tabIndex = position === focusAt ? 0 : -1;
        var src = drawn && drawn.src ? drawn.src : "";
        if (p.img.getAttribute("src") !== src) {
          if (src) {
            p.img.src = src;
          } else {
            p.img.removeAttribute("src");
          }
        }
        p.img.hidden = !src;
        var mark = "";
        if (b && drawn && drawn.error) {
          mark = "!";
        } else if (b && !drawn) {
          mark = str(b.title);
        }
        if (p.mark.textContent !== mark) {
          p.mark.textContent = mark;
        }
        var label = b ? where + ": " + (str(b.title) || "button without a title") : where + ": empty, add a button";
        n.setAttribute("aria-label", label);
        n.title = drawn && drawn.error ? drawn.error : "";
      });
      pageBtns.forEach(function (b, i) {
        b.setAttribute("aria-pressed", PAGES[i] === ed.page ? "true" : "false");
      });
    }

    function drawBar() {
      var dirty = isDirty();
      var lines = [];
      var cls = "bbar-text";
      if (ed.changed) {
        lines.push("This profile was changed outside Commander since you opened it. Save again to replace that change, or Discard to load it.");
        cls += " warm";
      } else if (dirty) {
        lines.push("Unsaved changes");
      } else {
        lines = ed.notes.slice();
      }
      if (ed.barError) {
        lines.push(ed.barError);
        cls += " error";
      }
      var text = lines.join("\n");
      if (barText.textContent !== text) {
        barText.textContent = text;
      }
      barText.className = cls;
      var acts = dirty || ed.changed;
      barActs.hidden = !acts;
      bar.hidden = lines.length === 0 && !acts;
      bar.classList.toggle("dirty", acts);
      saveBtn.textContent = ed.saving ? "Saving" : "Save";
      if (ed.saving) {
        saveBtn.setAttribute("aria-busy", "true");
        saveBtn.setAttribute("aria-disabled", "true");
        discardBtn.setAttribute("aria-disabled", "true");
      } else {
        saveBtn.removeAttribute("aria-busy");
        saveBtn.removeAttribute("aria-disabled");
        discardBtn.removeAttribute("aria-disabled");
      }
    }

    function draw() {
      var ready = !!ed.saved;
      problem.hidden = !ed.loadError;
      problem.textContent = ed.loadError;
      work.hidden = !ready;
      if (ready) {
        drawGrid();
        var used = typeof ed.saved.picturesUsed === "number" ? ed.saved.picturesUsed : 0;
        var slots = typeof ed.saved.pictureSlots === "number" ? ed.saved.pictureSlots : 15;
        var text = "Pictures: " + used + " of " + slots + " used";
        if (pictures.textContent !== text) {
          pictures.textContent = text;
        }
      }
      drawBar();
      var b = selectedButton();
      panelShow(b, b ? ed.errors[b.page + ":" + b.position] || [] : []);
      var dirty = isDirty();
      if (dirty !== ed.wasDirty) {
        ed.wasDirty = dirty;
        if (api.onDirtyChange) {
          api.onDirtyChange(dirty);
        }
      }
    }

    // changed is called after every change to the draft.
    function changed() {
      ed.notes = [];
      ed.barError = "";
      draw();
    }

    // The edit panel. Its controls are made once; panelShow only updates
    // them, so a field being typed in keeps its focus and caret.

    var shownPlace = ""; // profile:page:position of the button shown
    var lookMode = "style"; // "style" (Commander style) or "picture" (own picture)
    var pickBusy = ""; // "symbol" or "picture" while the file dialog is open
    var pickError = { symbol: "", picture: "" };
    var rec = null; // the recording that runs: {mode, target, data, picked, timer}
    var recNoteText = "";
    var stepShape = null;
    var stepRows = [];
    var stateShape = null;
    var lookHold = null; // {page, position, key}: the look a place keeps while typing
    var lookTimer = null;

    function add(parent) {
      for (var i = 1; i < arguments.length; i++) {
        parent.appendChild(arguments[i]);
      }
      return parent;
    }

    function setText(n, text) {
      if (n.textContent !== text) {
        n.textContent = text;
      }
    }

    // setValue changes a field the player is not typing in.
    function setValue(input, v) {
      if (document.activeElement !== input && input.value !== v) {
        input.value = v;
      }
    }

    function smallBtn(text, cls) {
      var b = el("button", "bsmall" + (cls ? " " + cls : ""), text);
      b.type = "button";
      return b;
    }

    function bigBtn(text, cls) {
      var b = el("button", "btn" + (cls ? " " + cls : ""), text);
      b.type = "button";
      return b;
    }

    function errLine() {
      var p = el("p", "note error");
      p.hidden = true;
      return p;
    }

    function showErr(p, text) {
      setText(p, text);
      p.hidden = !text;
    }

    function section(title, forId) {
      var sec = el("div", "bsec");
      var head = el(forId ? "label" : "h3", "label-caps bhead", title);
      if (forId) {
        head.htmlFor = forId;
      }
      sec.appendChild(head);
      return sec;
    }

    function checkbox(id, text) {
      var wrap = el("label", "bcheck");
      var box = el("input");
      box.type = "checkbox";
      box.id = id;
      add(wrap, box, el("span", "", text));
      return { node: wrap, box: box };
    }

    var pnone = el("p", "muted");
    var form = el("div", "bform");
    var otherErr = errLine(); // errors for fields the panel has no control for

    // Title
    var titleSec = section("Title", "bp-title");
    var titleIn = el("input", "binput");
    titleIn.type = "text";
    titleIn.id = "bp-title";
    titleIn.autocomplete = "off";
    titleIn.addEventListener("input", function () {
      var v = titleIn.value;
      holdLook();
      edit("title", function (b) {
        b.title = v;
      });
    });
    var titleErr = errLine();
    add(titleSec, titleIn, titleErr);

    // Look
    var lookSec = section("Look");
    var lookSeg = el("div", "seg");
    lookSeg.setAttribute("role", "group");
    lookSeg.setAttribute("aria-label", "Look");
    var styleChoice = el("button", "", "Commander style");
    styleChoice.type = "button";
    var pictureChoice = el("button", "", "Own picture");
    pictureChoice.type = "button";
    styleChoice.addEventListener("click", function () {
      lookMode = "style";
      var b = selectedButton();
      if (b && b.picture) {
        edit("picture", function (x) {
          x.picture = "";
        });
      } else {
        draw();
      }
    });
    pictureChoice.addEventListener("click", function () {
      lookMode = "picture";
      draw();
    });
    add(lookSeg, styleChoice, pictureChoice);

    var styleBox = el("div", "blook");
    var swatches = el("div", "bswatches");
    swatches.setAttribute("role", "group");
    swatches.setAttribute("aria-label", "Colour");
    var swatchBtns = PALETTE.map(function (c) {
      var s = el("button", "bswatch");
      s.type = "button";
      s.style.backgroundColor = c;
      s.setAttribute("aria-label", "Colour " + c);
      s.title = c;
      s.addEventListener("click", function () {
        edit("color", function (b) {
          b.color = c;
        });
      });
      swatches.appendChild(s);
      return s;
    });
    var anyWrap = el("label", "bany");
    var anyIn = el("input");
    anyIn.type = "color";
    anyIn.addEventListener("input", function () {
      var v = String(anyIn.value).toUpperCase();
      holdLook();
      edit("color", function (b) {
        b.color = v;
      });
    });
    add(anyWrap, anyIn, el("span", "", "Any colour"));
    swatches.appendChild(anyWrap);
    var colorErr = errLine();
    var symbolText = el("p", "note");
    var symbolRow = el("div", "row");
    var chooseSymbol = smallBtn("Choose symbol");
    chooseSymbol.addEventListener("click", function () {
      choose("symbol");
    });
    var noSymbol = smallBtn("No symbol");
    noSymbol.addEventListener("click", function () {
      pickError.symbol = "";
      edit("symbol", function (b) {
        b.symbol = "";
      });
    });
    add(symbolRow, chooseSymbol, noSymbol);
    var symbolErr = errLine();
    add(styleBox, swatches, colorErr, symbolText, symbolRow, symbolErr);

    var pictureBox = el("div", "blook");
    var pictureText = el("p", "note");
    var chooseRow = el("div", "row");
    var choosePicture = smallBtn("Choose picture");
    choosePicture.addEventListener("click", function () {
      choose("picture");
    });
    chooseRow.appendChild(choosePicture);
    var pictureErr = errLine();
    add(pictureBox, el("p", "note", "The title is not drawn on own pictures."), pictureText, chooseRow, pictureErr);
    add(lookSec, lookSeg, styleBox, pictureBox);

    // When tapped
    var stepsSec = section("When tapped");
    var stepList = el("ol", "bsteps");
    var noSteps = el("p", "note", "No steps yet: add one, or record keys.");
    var stepsErr = errLine();
    var stepActs = el("div", "row");
    var addStepBtn = bigBtn("Add step");
    addStepBtn.setAttribute("aria-expanded", "false");
    addStepBtn.addEventListener("click", function () {
      setMenu(addMenu.hidden);
    });
    var recordBtn = bigBtn("Record keys");
    recordBtn.addEventListener("click", function () {
      if (rec && rec.mode === "steps") {
        stopRecording(true);
      } else {
        startRecording("steps", -1);
      }
    });
    add(stepActs, addStepBtn, recordBtn);

    var addMenu = el("div", "bmenu");
    addMenu.hidden = true;
    addMenu.setAttribute("role", "group");
    addMenu.setAttribute("aria-label", "Add step");
    [
      ["keys", "Press keys"],
      ["wait", "Wait"],
      ["run", "Start a program"],
      ["open", "Open a website, file or folder"]
    ].forEach(function (k) {
      var b = smallBtn(k[1]);
      b.addEventListener("click", function () {
        setMenu(false);
        addStep(k[0]);
      });
      addMenu.appendChild(b);
    });

    var recBox = el("div", "brec");
    recBox.hidden = true;
    var recText = el("p", "brec-text");
    recText.setAttribute("aria-live", "polite");
    var recCaps = el("div", "bcaps");
    var recPick = el("select", "binput");
    recPick.setAttribute("aria-label", "Add a key from the list");
    recPick.addEventListener("change", function () {
      var name = recPick.value;
      recPick.value = "";
      if (rec && rec.mode === "chord" && name && rec.picked.indexOf(name) < 0) {
        rec.picked.push(name);
        armRecording();
        drawRecording();
      }
    });
    var pickFirst = el("option", "", "Add a key from the list");
    pickFirst.value = "";
    recPick.appendChild(pickFirst);
    KEY_LIST.forEach(function (name) {
      var o = el("option", "", keyWords(name));
      o.value = name;
      recPick.appendChild(o);
    });
    var recRow = el("div", "row");
    var recDone = smallBtn("Done");
    recDone.addEventListener("click", function () {
      stopRecording(true);
    });
    var recCancel = smallBtn("Cancel");
    recCancel.addEventListener("click", function () {
      stopRecording(false);
    });
    add(recRow, recPick, recDone, recCancel);
    add(recBox, recText, recCaps, recRow);
    var recNote = el("p", "note");
    recNote.hidden = true;
    var winNote = el("p", "note", "Windows keeps some keys for itself, such as the Windows key combinations and Print Screen; add those with Add step.");
    add(stepsSec, stepList, noSteps, stepsErr, stepActs, addMenu, recBox, recNote, winNote);

    // Options
    var optSec = section("Options");
    var hold = checkbox("bp-hold", "Hold while touched");
    var holdWhy = el("p", "note");
    var holdErr = errLine();
    hold.box.addEventListener("change", function () {
      var on = !!hold.box.checked;
      edit("mode", function (b) {
        b.hold = on && holdAllowed(b) && !(b.confirmMs > 0);
      });
    });
    var confirmBox = checkbox("bp-confirm", "Hold to confirm");
    var confirmWhy = el("p", "note");
    var confirmRow = el("div", "row bconfirm");
    var confirmIn = el("input", "brange");
    confirmIn.type = "range";
    confirmIn.min = "0.2";
    confirmIn.max = "5";
    confirmIn.step = "0.1";
    confirmIn.setAttribute("aria-label", "How long to hold");
    var confirmVal = el("span", "bconfirm-val");
    add(confirmRow, confirmIn, confirmVal);
    var confirmErr = errLine();
    confirmBox.box.addEventListener("change", function () {
      var on = !!confirmBox.box.checked;
      edit("confirm", function (b) {
        if (on && !b.hold && b.steps.length > 0) {
          b.confirmMs = b.confirmMs > 0 ? b.confirmMs : 1000;
        } else {
          b.confirmMs = 0;
        }
      });
    });
    confirmIn.addEventListener("input", function () {
      var ms = Math.round(parseFloat(confirmIn.value) * 10) * 100;
      edit("confirm", function (b) {
        b.confirmMs = Math.min(5000, Math.max(200, ms));
      });
    });
    var stateWrap = el("div", "bstate");
    var stateLabel = el("label", "", "Flips lighting state");
    stateLabel.htmlFor = "bp-state";
    var stateIn = el("select", "binput");
    stateIn.id = "bp-state";
    stateIn.addEventListener("change", function () {
      var v = stateIn.value;
      edit("state", function (b) {
        b.state = v;
      });
    });
    var stateErr = errLine();
    add(stateWrap, stateLabel, stateIn, stateErr);
    add(optSec, hold.node, holdWhy, holdErr, confirmBox.node, confirmWhy, confirmRow, confirmErr, stateWrap);

    // Save and Delete
    var panelActs = el("div", "row bacts");
    var panelSave = bigBtn("Save", "primary");
    panelSave.addEventListener("click", function () {
      save();
    });
    var panelDelete = bigBtn("Delete", "danger");
    panelDelete.addEventListener("click", deleteSelected);
    add(panelActs, panelSave, panelDelete);

    add(form, otherErr, titleSec, lookSec, stepsSec, optSec, panelActs);
    add(panel, pnone, form);

    // edit changes the selected button: fn changes it in place. The
    // problems a save found with that field go, since it was changed.
    function edit(field, fn) {
      changeSelected(function (b) {
        fn(b);
        tidy(b);
        dropErrors(b, function (e) {
          // A lighting state also answers "add at least one step".
          return e.field === field || (field === "state" && e.field === "action" && e.step < 0);
        });
      });
    }

    // dropErrors forgets the problems of button b that gone tells.
    function dropErrors(b, gone) {
      var key = b.page + ":" + b.position;
      var list = (ed.errors[key] || []).filter(function (e) {
        return !gone(e);
      });
      if (list.length > 0) {
        ed.errors[key] = list;
      } else {
        delete ed.errors[key];
      }
    }

    // holdLook keeps the grid on the selected button's current look until
    // the player has stopped typing for LOOK_PAUSE.
    function holdLook() {
      var b = selectedButton();
      if (!b) {
        return;
      }
      if (!lookHold || lookHold.page !== b.page || lookHold.position !== b.position) {
        lookHold = { page: b.page, position: b.position, key: lookKey(b) };
      }
      if (lookTimer) {
        clearTimeout(lookTimer);
      }
      lookTimer = setTimeout(releaseLook, LOOK_PAUSE);
    }

    function releaseLook() {
      if (lookTimer) {
        clearTimeout(lookTimer);
      }
      lookTimer = null;
      if (lookHold) {
        lookHold = null;
        drawGrid();
      }
    }

    // choose asks for a PNG with the file dialog and copies it into the
    // configuration folder as a symbol or a picture of the button.
    function choose(kind) {
      var b = selectedButton();
      if (!b || pickBusy) {
        return;
      }
      var gen = ed.gen;
      var page = b.page;
      var position = b.position;
      pickBusy = kind;
      pickError[kind] = "";
      draw();
      function done() {
        pickBusy = "";
        if (gen === ed.gen) {
          draw();
        }
      }
      api.call("__pickPicture").then(function (res) {
        var path = res && typeof res.path === "string" ? res.path : "";
        if (!path) {
          return null; // cancelled
        }
        return api.call("importPicture", { path: path, kind: kind });
      }).then(function (res) {
        pickBusy = "";
        if (!res || gen !== ed.gen) {
          done();
          return;
        }
        var rel = str(res.path);
        var target = find(ed.draft, page, position);
        if (!rel || !target) {
          done();
          return;
        }
        dropErrors(target, function (e) {
          return e.field === "symbol" || e.field === "picture";
        });
        if (kind === "symbol") {
          target.symbol = rel;
          target.picture = "";
        } else {
          target.picture = rel;
          target.symbol = "";
        }
        changed();
      }, function (err) {
        pickError[kind] = api.plainError("Could not use this file", err);
        done();
      });
    }

    // fileWords names a picked file without its folder and extension.
    function fileWords(path) {
      var name = String(path).split(/[\\/]/).pop();
      return name.replace(/\.png$/i, "");
    }

    function setMenu(open) {
      addMenu.hidden = !open;
      addStepBtn.setAttribute("aria-expanded", open ? "true" : "false");
      if (open && addMenu.firstChild) {
        addMenu.firstChild.focus();
      }
    }

    function addStep(kind) {
      if (kind === "keys") {
        startRecording("chord", -1);
        return;
      }
      var step = kind === "wait" ? { waitMs: 100 } : kind === "run" ? { run: "", args: [] } : { open: "" };
      edit("action", function (b) {
        b.steps.push(step);
      });
      var row = stepRows[stepRows.length - 1];
      if (row && row.first) {
        row.first.focus();
      }
    }

    function moveStep(i, by) {
      var b = selectedButton();
      var j = i + by;
      if (!b || j < 0 || j >= b.steps.length) {
        return;
      }
      edit("action", function (x) {
        var s = x.steps[i];
        x.steps[i] = x.steps[j];
        x.steps[j] = s;
      });
      var row = stepRows[j];
      if (row) {
        var to = by < 0 ? row.up : row.down;
        (to.disabled ? (by < 0 ? row.down : row.up) : to).focus();
      }
    }

    function removeStep(i) {
      edit("action", function (b) {
        b.steps.splice(i, 1);
      });
      var row = stepRows[Math.min(i, stepRows.length - 1)];
      (row ? row.remove : addStepBtn).focus();
    }

    function deleteSelected() {
      var b = selectedButton();
      if (!b) {
        return;
      }
      var position = b.position;
      ed.draft = ed.draft.filter(function (x) {
        return x !== b;
      });
      delete ed.errors[b.page + ":" + b.position];
      ed.selected = -1;
      changed();
      places[position].node.focus();
    }

    // Recording keys. The page listens on the document only while a
    // recording runs, before anything else hears the keys.

    function startRecording(mode, target) {
      stopRecording(true);
      setMenu(false);
      rec = { mode: mode, target: target, data: newRecording(), picked: [], timer: null };
      recNoteText = "";
      document.addEventListener("keydown", onRecordKey, true);
      document.addEventListener("keyup", onRecordKey, true);
      window.addEventListener("blur", onRecordBlur);
      armRecording();
      drawRecording();
      (mode === "chord" ? recDone : recordBtn).focus();
    }

    function armRecording() {
      if (rec.timer) {
        clearTimeout(rec.timer);
      }
      rec.timer = setTimeout(function () {
        stopRecording(true);
      }, RECORD_IDLE);
    }

    function onRecordBlur() {
      stopRecording(true);
    }

    function onRecordKey(e) {
      if (!rec) {
        return;
      }
      // The key list works with the keyboard while picking a chord.
      if (rec.mode === "chord" && e.target === recPick) {
        return;
      }
      e.preventDefault();
      e.stopPropagation();
      var t = typeof e.timeStamp === "number" ? e.timeStamp : Date.now();
      var out = e.type === "keydown" ? recordDown(rec.data, e.code, t) : recordUp(rec.data, e.code, t);
      armRecording();
      if (rec.mode === "chord") {
        var chord = out.filter(function (s) {
          return s.keys;
        });
        if (chord.length > 0) {
          stopRecording(true, chord[0].keys);
          return;
        }
      } else if (out.length > 0) {
        edit("action", function (b) {
          out.forEach(function (s) {
            b.steps.push(s);
          });
        });
        return;
      }
      drawRecording();
    }

    // stopRecording ends the recording. With keep, what it caught goes into
    // the button: a chord still held, or the picked chord.
    function stopRecording(keep, chord) {
      if (!rec) {
        return;
      }
      var r = rec;
      rec = null;
      clearTimeout(r.timer);
      document.removeEventListener("keydown", onRecordKey, true);
      document.removeEventListener("keyup", onRecordKey, true);
      window.removeEventListener("blur", onRecordBlur);
      var left = recordEnd(r.data);
      if (r.data.skipped.length > 0) {
        recNoteText = "Skipped, Commander cannot press it: " + r.data.skipped.join(", ") + ".";
      }
      var steps = [];
      if (keep && r.mode === "steps") {
        steps = left;
      } else if (keep) {
        var keys = r.picked.slice();
        (chord || (left[0] ? left[0].keys : [])).forEach(function (k) {
          if (keys.indexOf(k) < 0) {
            keys.push(k);
          }
        });
        if (keys.length > 0) {
          steps = [{ keys: keys }];
        }
      }
      if (steps.length > 0 && selectedButton()) {
        edit("action", function (b) {
          if (r.mode === "chord" && r.target >= 0 && b.steps[r.target] && stepKind(b.steps[r.target]) === "keys") {
            b.steps[r.target] = steps[0];
          } else {
            steps.forEach(function (s) {
              b.steps.push(s);
            });
          }
        });
      } else {
        drawRecording();
      }
    }

    function capsInto(box, keys) {
      var kids = keys.map(function (k) {
        var cap = keyCap(k);
        var n = el("kbd", "bcap", cap.text);
        if (cap.title) {
          n.title = cap.title;
        }
        return n;
      });
      box.replaceChildren.apply(box, kids);
    }

    function drawRecording() {
      recBox.hidden = !rec;
      showErr(recNote, recNoteText);
      recNote.className = "note";
      recordBtn.textContent = rec && rec.mode === "steps" ? "Stop" : "Record keys";
      recordBtn.setAttribute("aria-pressed", rec && rec.mode === "steps" ? "true" : "false");
      if (!rec) {
        return;
      }
      var chord = rec.mode === "chord";
      setText(recText, chord ?
        "Press the keys for this step together, or add them from the list." :
        "Recording: press keys; a pause between them becomes a Wait step. Stop, or 10 seconds without a key, ends it.");
      var keys = rec.picked.slice();
      rec.data.chord.forEach(function (k) {
        if (keys.indexOf(k) < 0) {
          keys.push(k);
        }
      });
      capsInto(recCaps, keys);
      recCaps.hidden = keys.length === 0;
      // Recording steps ends with Record keys, which reads Stop meanwhile.
      recRow.hidden = !chord;
      if (rec.data.skipped.length > 0) {
        showErr(recNote, "Skipped, Commander cannot press it: " + rec.data.skipped.join(", ") + ".");
      }
    }

    // The steps

    function stepsShape(b) {
      return canonical(b.steps.map(function (s) {
        var kind = stepKind(s);
        return kind === "keys" ? s.keys : kind;
      }));
    }

    function makeStepRow(s, i, count) {
      var kind = stepKind(s);
      var li = el("li", "bstep");
      var num = el("span", "bstep-num", String(i + 1));
      num.setAttribute("aria-hidden", "true");
      var body = el("div", "bstep-body");
      var row = { node: li, kind: kind, first: null, inputs: {} };
      function input(cls, label, onInput) {
        var n = el("input", "binput " + cls);
        n.type = "text";
        n.autocomplete = "off";
        n.setAttribute("aria-label", label + ", step " + (i + 1));
        n.addEventListener("input", function () {
          var v = n.value;
          edit("action", function (b) {
            if (b.steps[i]) {
              onInput(b.steps[i], v);
            }
          });
        });
        return n;
      }
      if (kind === "keys") {
        body.appendChild(el("span", "bstep-what", "Press keys"));
        var caps = el("span", "bcaps");
        capsInto(caps, s.keys);
        body.appendChild(caps);
        var change = smallBtn("Change");
        change.setAttribute("aria-label", "Change the keys of step " + (i + 1));
        change.addEventListener("click", function () {
          startRecording("chord", i);
        });
        body.appendChild(change);
        row.first = change;
      } else if (kind === "wait") {
        body.appendChild(el("span", "bstep-what", "Wait"));
        var ms = input("bms", "Wait in milliseconds", function (st, v) {
          var n = Number(v);
          st.waitMs = v.trim() === "" || !isFinite(n) ? 0 : n;
        });
        ms.type = "number";
        ms.min = "1";
        ms.max = String(WAIT_MAX);
        ms.step = "10";
        body.appendChild(ms);
        body.appendChild(el("span", "bstep-unit", "ms"));
        row.inputs.wait = ms;
        row.first = ms;
      } else if (kind === "run") {
        body.appendChild(el("span", "bstep-what", "Start a program"));
        var prog = input("bwide", "Program to start", function (st, v) {
          st.run = v;
        });
        prog.placeholder = "Program";
        var args = input("bwide", "Arguments", function (st, v) {
          st.args = splitArgs(v);
        });
        args.placeholder = "Arguments, if any";
        add(body, prog, args);
        row.inputs.run = prog;
        row.inputs.args = args;
        row.first = prog;
      } else if (kind === "open") {
        body.appendChild(el("span", "bstep-what", "Open"));
        var target = input("bwide", "Website, file or folder to open", function (st, v) {
          st.open = v;
        });
        target.placeholder = "A website, file or folder";
        body.appendChild(target);
        row.inputs.open = target;
        row.first = target;
      } else {
        body.appendChild(el("span", "bstep-what", "A step Commander cannot show"));
      }
      var acts = el("div", "bstep-acts");
      row.up = smallBtn(String.fromCharCode(0x2191), "bicon");
      row.up.setAttribute("aria-label", "Move step " + (i + 1) + " up");
      row.up.disabled = i === 0;
      row.up.addEventListener("click", function () {
        moveStep(i, -1);
      });
      row.down = smallBtn(String.fromCharCode(0x2193), "bicon");
      row.down.setAttribute("aria-label", "Move step " + (i + 1) + " down");
      row.down.disabled = i === count - 1;
      row.down.addEventListener("click", function () {
        moveStep(i, 1);
      });
      row.remove = smallBtn(String.fromCharCode(0xd7), "bicon");
      row.remove.setAttribute("aria-label", "Remove step " + (i + 1));
      row.remove.addEventListener("click", function () {
        removeStep(i);
      });
      add(acts, row.up, row.down, row.remove);
      row.err = errLine();
      body.appendChild(row.err);
      add(li, num, body, acts);
      if (!row.first) {
        row.first = row.up.disabled ? row.remove : row.up;
      }
      return row;
    }

    function drawSteps(b, errs, fresh) {
      var shape = stepsShape(b);
      if (fresh || shape !== stepShape) {
        stepShape = shape;
        stepRows = b.steps.map(function (s, i) {
          return makeStepRow(s, i, b.steps.length);
        });
        stepList.replaceChildren.apply(stepList, stepRows.map(function (r) {
          return r.node;
        }));
      }
      stepRows.forEach(function (row, i) {
        var s = b.steps[i];
        if (row.inputs.wait) {
          setValue(row.inputs.wait, typeof s.waitMs === "number" && s.waitMs > 0 ? String(s.waitMs) : "");
        }
        if (row.inputs.run) {
          setValue(row.inputs.run, s.run);
          // The arguments field keeps what was typed while it reads the same.
          if (document.activeElement !== row.inputs.args && canonical(splitArgs(row.inputs.args.value)) !== canonical(s.args || [])) {
            row.inputs.args.value = joinArgs(s.args);
          }
        }
        if (row.inputs.open) {
          setValue(row.inputs.open, s.open);
        }
        showErr(row.err, errs.filter(function (e) {
          return e.field === "action" && e.step === i;
        }).map(function (e) {
          return e.message;
        }).join("\n"));
      });
      noSteps.hidden = b.steps.length > 0;
    }

    function drawOptions(b) {
      var holdOK = holdAllowed(b);
      var confirmOn = b.confirmMs > 0;
      hold.box.checked = !!b.hold;
      hold.box.disabled = !b.hold && (!holdOK || confirmOn);
      setText(holdWhy, !holdOK ? "Only for a button whose one step is Press keys: the keys stay down while you touch it." :
        confirmOn ? "Not together with Hold to confirm." : "The keys stay down while you touch the button.");
      confirmBox.box.checked = confirmOn;
      confirmBox.box.disabled = !confirmOn && (!!b.hold || b.steps.length === 0);
      setText(confirmWhy, b.hold ? "Not together with Hold while touched." :
        b.steps.length === 0 ? "Add a step first: there is nothing to confirm." :
          "The steps run only after you hold the button this long.");
      confirmRow.hidden = !confirmOn;
      var secs = Math.min(5, Math.max(0.2, (b.confirmMs || 1000) / 1000));
      setValue(confirmIn, String(secs));
      setText(confirmVal, (Math.round(b.confirmMs / 100) / 10).toFixed(1) + " s");
      var states = ed.saved && Array.isArray(ed.saved.states) ? ed.saved.states.slice() : [];
      if (b.state && states.indexOf(b.state) < 0) {
        states.push(b.state);
      }
      stateWrap.hidden = states.length === 0;
      var shape = canonical(states);
      if (shape !== stateShape) {
        stateShape = shape;
        var none = el("option", "", "None");
        none.value = "";
        stateIn.replaceChildren.apply(stateIn, [none].concat(states.map(function (s) {
          var o = el("option", "", s);
          o.value = s;
          return o;
        })));
      }
      if (stateIn.value !== str(b.state)) {
        stateIn.value = str(b.state);
      }
    }

    function messages(errs, field) {
      return errs.filter(function (e) {
        return e.field === field && !(field === "action" && e.step >= 0);
      }).map(function (e) {
        return e.message;
      }).join("\n");
    }

    // panelShow is called on every draw with the selected button, or null.
    function panelShow(b, errs) {
      var place = b ? ed.profile + ":" + b.page + ":" + b.position : "";
      var fresh = place !== shownPlace;
      if (fresh) {
        shownPlace = place;
        stopRecording(false);
        recNoteText = "";
        setMenu(false);
        pickError = { symbol: "", picture: "" };
        lookMode = b && b.picture ? "picture" : "style";
      }
      pnone.hidden = !!b;
      setText(pnone, ed.saved ? "Pick a button, or an empty place to add one." : "");
      form.hidden = !b;
      if (!b) {
        stepShape = null;
        return;
      }
      if (b.picture) {
        lookMode = "picture";
      }
      setValue(titleIn, str(b.title));
      showErr(titleErr, messages(errs, "title"));

      styleChoice.setAttribute("aria-pressed", lookMode === "style" ? "true" : "false");
      pictureChoice.setAttribute("aria-pressed", lookMode === "picture" ? "true" : "false");
      styleBox.hidden = lookMode !== "style";
      pictureBox.hidden = lookMode !== "picture";
      var color = str(b.color).toUpperCase();
      var inPalette = false;
      swatchBtns.forEach(function (s, i) {
        var on = PALETTE[i] === color;
        inPalette = inPalette || on;
        s.setAttribute("aria-pressed", on ? "true" : "false");
      });
      anyWrap.classList.toggle("on", !inPalette && /^#[0-9A-F]{6}$/.test(color));
      if (/^#[0-9A-F]{6}$/.test(color)) {
        setValue(anyIn, color.toLowerCase());
      }
      showErr(colorErr, messages(errs, "color"));
      setText(symbolText, b.symbol ? "Symbol: " + fileWords(b.symbol) : "No symbol");
      noSymbol.hidden = !b.symbol;
      chooseSymbol.textContent = pickBusy === "symbol" ? "Choosing" : "Choose symbol";
      showErr(symbolErr, [messages(errs, "symbol"), pickError.symbol].filter(Boolean).join("\n"));
      setText(pictureText, b.picture ? "Picture: " + fileWords(b.picture) : "No picture chosen yet.");
      choosePicture.textContent = pickBusy === "picture" ? "Choosing" : "Choose picture";
      showErr(pictureErr, [messages(errs, "picture"), pickError.picture].filter(Boolean).join("\n"));

      drawSteps(b, errs, fresh);
      showErr(stepsErr, messages(errs, "action"));
      drawRecording();

      drawOptions(b);
      showErr(holdErr, messages(errs, "mode"));
      showErr(confirmErr, messages(errs, "confirm"));
      showErr(stateErr, messages(errs, "state"));
      // Problems with a field the panel has no control for show on top.
      var shownFields = ["title", "color", "symbol", "picture", "action", "mode", "confirm", "state"];
      showErr(otherErr, errs.filter(function (e) {
        return shownFields.indexOf(e.field) < 0;
      }).map(function (e) {
        return e.message;
      }).join("\n"));

      panelSave.textContent = ed.saving ? "Saving" : "Save";
      if (ed.saving) {
        panelSave.setAttribute("aria-busy", "true");
      } else {
        panelSave.removeAttribute("aria-busy");
      }
    }

    function panelOpen() {
      var first = panel.querySelector("button, input, select, textarea");
      if (first) {
        first.focus();
      }
    }

    // changeSelected lets the edit panel change the selected button of the
    // draft: fn gets the button and changes it in place.
    function changeSelected(fn) {
      var b = selectedButton();
      if (b) {
        fn(b);
        changed();
      }
    }

    // Loading and saving

    function isDirty() {
      return !!ed.saved && !same(ed.draft, ed.saved.buttons);
    }

    function validAnswer(res) {
      return res && typeof res === "object" && Array.isArray(res.buttons);
    }

    // load fetches the profile. A draft that differs from what was saved
    // when the answer arrives stays (an edit may have come in while the
    // call ran); otherwise the draft becomes the file.
    function load() {
      var gen = ed.gen;
      var profile = ed.profile;
      return api.call("deviceProfile", { device: "kb7", profile: profile }).then(function (res) {
        if (gen !== ed.gen || !validAnswer(res)) {
          return;
        }
        var wasDirty = isDirty();
        var fresh = !ed.saved || ed.saved.version !== res.version;
        ed.loadError = "";
        if (wasDirty) {
          ed.saved = res;
        } else {
          if (fresh) {
            pngs = {}; // pictures on disk may have changed with the file
          }
          ed.saved = res;
          ed.draft = copy(res.buttons);
        }
        draw();
      }, function (err) {
        if (gen !== ed.gen) {
          return;
        }
        ed.loadError = api.plainError("Could not read this profile", err);
        draw();
      });
    }

    function show(profile) {
      if (profile === ed.profile) {
        return;
      }
      ed.gen += 1;
      ed.profile = profile;
      ed.saved = null;
      ed.draft = [];
      ed.page = 1;
      ed.selected = -1;
      ed.loadError = "";
      ed.notes = [];
      ed.barError = "";
      ed.changed = false;
      ed.errors = {};
      releaseLook();
      draw();
      if (profile >= 0) {
        load();
      }
    }

    // refresh reads the profile again when nothing is being edited, so a
    // change made outside Commander shows. A draft with changes stays; a
    // save finds out whether the file moved on.
    function refresh() {
      if (ed.profile < 0 || ed.saving || ed.changed || isDirty()) {
        return;
      }
      load();
    }

    function discard() {
      if (!ed.saved) {
        return;
      }
      ed.draft = copy(ed.saved.buttons);
      ed.changed = false;
      ed.errors = {};
      if (ed.selected >= 0 && !current(ed.selected)) {
        ed.selected = -1;
      }
      changed();
    }

    // showErrors keeps the field errors of a save per button and selects
    // the first button with one.
    function showErrors(list) {
      ed.errors = {};
      list.forEach(function (e) {
        var key = e.page + ":" + e.position;
        (ed.errors[key] = ed.errors[key] || []).push({ field: str(e.field), step: typeof e.step === "number" ? e.step : -1, message: str(e.message) });
      });
      var first = list[0];
      if (PAGES.indexOf(first.page) >= 0) {
        ed.page = first.page;
        ed.selected = first.position;
      }
      ed.notes = [];
      ed.barError = "Not saved: a button needs a fix.";
      draw();
    }

    // save saves the whole draft. Its Promise says whether it went through.
    // The page's own checks come first; Commander is not asked when one
    // fails.
    function save() {
      if (ed.saving) {
        return ed.saving;
      }
      if (!ed.saved) {
        return Promise.resolve(false);
      }
      var bad = pageChecks(ed.draft);
      if (bad.length > 0) {
        showErrors(bad);
        return Promise.resolve(false);
      }
      var gen = ed.gen;
      var sent = copy(sorted(ed.draft));
      ed.barError = "";
      ed.saving = api.call("saveDeviceProfile", {
        device: "kb7", profile: ed.profile, version: ed.saved.version, buttons: sent
      }).then(function (res) {
        ed.saving = null;
        if (gen !== ed.gen) {
          return false;
        }
        res = res && typeof res === "object" ? res : {};
        if (res.ok) {
          ed.errors = {};
          ed.changed = false;
          ed.saved.version = str(res.version);
          ed.saved.buttons = sent;
          ed.notes = saveNotes(res);
          draw();
          // The file as written: tidied values and the pictures count.
          load();
          return true;
        }
        if (res.changed) {
          ed.changed = true;
          draw();
          // The file's new version, for the next Save; Discard loads it.
          load();
          return false;
        }
        if (Array.isArray(res.errors) && res.errors.length > 0) {
          showErrors(res.errors);
          return false;
        }
        ed.barError = api.plainError("Could not save", str(res.error) || "Commander did not say why");
        draw();
        return false;
      }, function (err) {
        ed.saving = null;
        if (gen === ed.gen) {
          ed.barError = api.plainError("Could not save", err);
          draw();
        }
        return false;
      });
      draw();
      return ed.saving;
    }

    draw();

    return {
      node: node,
      show: show,
      refresh: refresh,
      dirty: isDirty,
      save: save,
      discard: discard,
      changeSelected: changeSelected,
      // stopRecording ends a key recording, for when the tab is left.
      stopRecording: function () {
        stopRecording(false);
      }
    };
  }

  window.CommanderButtons = { create: create, logic: logic };
})();
