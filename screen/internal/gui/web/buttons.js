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

  var logic = {
    hasPlace: hasPlace, find: find, newButton: newButton, move: move,
    same: same, sorted: sorted, saveNotes: saveNotes
  };

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
        var drawn = b ? pngs[lookKey(b)] : null;
        if (b) {
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

    // The edit panel. Until it holds the fields, it names the selected
    // button and lists the problems a save found with it.
    function panelShow(b, errs) {
      var kids = [];
      if (!b) {
        kids.push(el("p", "muted", ed.saved ? "Pick a button, or an empty place to add one." : ""));
      } else {
        kids.push(el("p", "label-caps", str(b.title) || "Button without a title"));
        errs.forEach(function (e) {
          kids.push(el("p", "note error", str(e.message)));
        });
      }
      panel.replaceChildren.apply(panel, kids);
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

    // save saves the whole draft. Its Promise says whether it went through.
    function save() {
      if (ed.saving) {
        return ed.saving;
      }
      if (!ed.saved) {
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
          ed.errors = {};
          res.errors.forEach(function (e) {
            var key = e.page + ":" + e.position;
            (ed.errors[key] = ed.errors[key] || []).push({ field: str(e.field), step: e.step, message: str(e.message) });
          });
          var first = res.errors[0];
          if (PAGES.indexOf(first.page) >= 0) {
            ed.page = first.page;
            ed.selected = first.position;
          }
          ed.barError = "Not saved: a button needs a fix.";
          draw();
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
      changeSelected: changeSelected
    };
  }

  window.CommanderButtons = { create: create, logic: logic };
})();
