/* SPDX-License-Identifier: GPL-2.0-or-later */
"use strict";

// The game profile pages: a game's name, the program that switches to it and
// what each device does, or the profile Desktop shows, edited as a draft
// until Save. Built with createElement and textContent only.

(function () {
  var SLOTS = 5; // the KB7 has five profiles
  var NONE = "none"; // the "(don't switch)" choice of a device

  // Draft logic: plain functions, no page involved.

  function str(v) {
    return typeof v === "string" ? v : "";
  }

  // fileName is the last part of a path, as a program is named.
  function fileName(path) {
    var parts = str(path).split(/[\\/]/);
    return parts[parts.length - 1];
  }

  // draftOf is the part of a gameProfile answer the player edits.
  function draftOf(res) {
    return {
      name: str(res && res.name),
      program: str(res && res.program),
      kb7: res && typeof res.kb7 === "number" ? res.kb7 : null
    };
  }

  function same(a, b) {
    return a.name === b.name && a.program === b.program && a.kb7 === b.kb7;
  }

  function copy(d) {
    return { name: d.name, program: d.program, kb7: d.kb7 };
  }

  // tidy is the draft as it is saved: without surrounding spaces.
  function tidy(d) {
    return { name: d.name.trim(), program: d.program.trim(), kb7: d.kb7 };
  }

  // programChoices are the programs the select offers: the running ones,
  // and the game's own program among them in its own spelling.
  function programChoices(running, program) {
    var out = [];
    var low = program.toLowerCase();
    var found = false;
    (Array.isArray(running) ? running : []).forEach(function (p) {
      if (typeof p !== "string" || !p) {
        return;
      }
      if (low && p.toLowerCase() === low) {
        if (!found) {
          out.push(program);
          found = true;
        }
        return;
      }
      out.push(p);
    });
    if (program && !found) {
      out.unshift(program);
    }
    return out;
  }

  // profileChoices are the KB7 profiles a game can switch to, by name and
  // slot, after "(don't switch)" (which Desktop does not have), then the
  // slots that have no profile yet: choosing one makes a new, empty profile
  // there when the game is saved.
  function profileChoices(profiles, desktop) {
    var out = [];
    if (!desktop) {
      out.push({ value: NONE, label: "(don't switch)" });
    }
    var list = (Array.isArray(profiles) ? profiles : []).filter(function (p) {
      return p && typeof p.number === "number";
    }).sort(function (a, b) {
      return a.number - b.number;
    });
    list.forEach(function (p) {
      out.push({ value: String(p.number), label: (str(p.name) || "Profile " + (p.number + 1)) + " (slot " + (p.number + 1) + ")" });
    });
    for (var n = 0; n < SLOTS; n++) {
      if (!list.some(function (p) { return p.number === n; })) {
        out.push({ value: String(n), label: "New profile (slot " + (n + 1) + ")" });
      }
    }
    return out;
  }

  function choiceValue(kb7) {
    return typeof kb7 === "number" ? String(kb7) : NONE;
  }

  function choiceKB7(value) {
    return value === NONE || value === "" ? null : Number(value);
  }

  function backupLine(res, done) {
    var backups = Array.isArray(res.backups) ? res.backups : [];
    if (backups.length === 0) {
      return "";
    }
    return "Your original file was copied to " + backups.join(", ") + " before " + done + "; Commander does not keep comments.";
  }

  // saveNotes are the lines after a save that went through.
  function saveNotes(res) {
    var lines = ["Saved.", "Commander uses this game profile now."];
    var b = backupLine(res, "saving");
    if (b) {
      lines.push(b);
    }
    if (res.migrated) {
      lines.push("Programs that switch profiles are now game profiles in the sidebar.");
    }
    return lines;
  }

  // deleteNotes are the lines after a delete that went through.
  function deleteNotes(name, res) {
    var lines = [name + " was deleted."];
    var b = backupLine(res, "deleting");
    if (b) {
      lines.push(b);
    }
    if (res.migrated) {
      lines.push("Programs that switch profiles are now game profiles in the sidebar.");
    }
    return lines;
  }

  var logic = {
    fileName: fileName,
    draftOf: draftOf,
    same: same,
    tidy: tidy,
    programChoices: programChoices,
    profileChoices: profileChoices,
    saveNotes: saveNotes,
    deleteNotes: deleteNotes
  };

  // The editor: one for the window's life, shown on the page of the game it
  // holds. api: call, el, plainError, onDirtyChange, and
  // onSaved(oldId, newId) when a save gives the game another id,
  // onDeleted() after a delete, openProfile(n) for an Edit link.

  function create(api) {
    var el = api.el;

    var ed = {
      id: null, // "desktop", the game's id, or "" for a new one; null before the first show
      desktop: false,
      saved: null, // the gameProfile answer the draft started from
      draft: null,
      loadError: "",
      saving: null, // the Promise of a save that runs
      deleting: false,
      asking: false, // the delete question shows
      deleteNote: "",
      notes: [], // lines after a save or a delete
      barError: "",
      fieldErrors: {}, // a refusal of the last save per field: name, program
      changed: false, // the file changed outside Commander since it was read
      gen: 0, // counts shows, so an overtaken answer is ignored
      wasDirty: false,
      running: [], // the runningPrograms answer
      runningBusy: false,
      runningError: "",
      picking: false, // the file dialog is open
      pickError: ""
    };

    // Nodes, made once and kept: a redraw only changes them, so a field
    // being typed in keeps its focus and text.

    var head = el("h1", "", "");
    var node = el("div", "gedit");

    function add(parent) {
      for (var i = 1; i < arguments.length; i++) {
        parent.appendChild(arguments[i]);
      }
      return parent;
    }

    function btn(text, cls, onClick) {
      var b = el("button", cls, text);
      b.type = "button";
      b.addEventListener("click", function () {
        if (b.getAttribute("aria-disabled") !== "true") {
          onClick();
        }
      });
      return b;
    }

    function busy(b, on, text, busyText) {
      b.textContent = on ? busyText : text;
      if (on) {
        b.setAttribute("aria-busy", "true");
        b.setAttribute("aria-disabled", "true");
      } else {
        b.removeAttribute("aria-busy");
        b.removeAttribute("aria-disabled");
      }
    }

    function setText(n, text) {
      if (n.textContent !== text) {
        n.textContent = text;
      }
      n.hidden = !text;
    }

    function section(title) {
      var sec = el("section", "section");
      sec.appendChild(el("h2", "", title));
      var card = el("div", "card");
      sec.appendChild(card);
      return { node: sec, card: card };
    }

    // The bar of unsaved changes and notes, as in the other editors.
    var bar = el("div", "bbar");
    bar.hidden = true;
    var barText = el("p", "bbar-text");
    barText.setAttribute("aria-live", "polite");
    // Save and Discard are the window's own bar (app.js): one place for
    // every page.
    bar.appendChild(barText);

    var problem = el("p", "note error");
    problem.hidden = true;

    var form = el("div", "gform");

    // Name
    var nameSec = section("Name");
    var nameIn = el("input", "binput gname");
    nameIn.type = "text";
    nameIn.id = "gp-name";
    nameIn.autocomplete = "off";
    nameIn.setAttribute("aria-label", "Name");
    nameIn.addEventListener("input", function () {
      if (ed.draft) {
        ed.draft.name = nameIn.value;
        changed();
      }
    });
    var nameErr = el("p", "note error");
    nameErr.hidden = true;
    add(nameSec.card, nameIn, nameErr);

    // Switch to this when
    var progSec = section("Switch to this when");
    var progRow = el("div", "row");
    var progIn = el("select", "binput gprogram");
    progIn.id = "gp-program";
    progIn.setAttribute("aria-label", "Program");
    progIn.addEventListener("change", function () {
      if (ed.draft) {
        ed.draft.program = progIn.value;
        changed();
      }
    });
    var refreshBtn = btn("Refresh", "btn", function () {
      loadRunning();
    });
    refreshBtn.setAttribute("aria-label", "Refresh the running programs");
    var browseBtn = btn("Browse", "btn", function () {
      browse();
    });
    add(progRow, progIn, refreshBtn, browseBtn);
    var progErr = el("p", "note error");
    progErr.hidden = true;
    add(progSec.card, progRow, progErr);
    var progShape = null;

    // What each device does
    var devSec = section("What each device does");
    var devRow = el("div", "row gdevice");
    var kb7Label = el("label", "gdevice-name", "KB7");
    kb7Label.htmlFor = "gp-kb7";
    var kb7In = el("select", "binput");
    kb7In.id = "gp-kb7";
    kb7In.addEventListener("change", function () {
      if (ed.draft) {
        ed.draft.kb7 = choiceKB7(kb7In.value);
        changed();
      }
    });
    var editBtn = btn("Edit", "btn", function () {
      if (ed.draft && typeof ed.draft.kb7 === "number" && api.openProfile) {
        api.openProfile(ed.draft.kb7);
      }
    });
    add(devRow, kb7Label, kb7In, editBtn);
    devSec.card.appendChild(devRow);
    var kb7Shape = null;

    var endLine = el("p", "gline");

    // Delete (saving is the window's own bar)
    var acts = el("div", "row gacts");
    var deleteBtn = btn("Delete", "btn danger", function () {
      askDelete();
    });
    add(acts, deleteBtn);

    var ask = el("div", "banner ask");
    ask.setAttribute("role", "group");
    ask.setAttribute("aria-label", "Delete game profile");
    ask.hidden = true;
    var askText = el("p", "");
    askText.setAttribute("role", "alert");
    var askRow = el("div", "row");
    var askDelBtn = btn("Delete", "btn danger", function () {
      doDelete();
    });
    var askKeepBtn = btn("Keep", "btn", function () {
      if (!ed.deleting) {
        ed.asking = false;
        draw();
        deleteBtn.focus();
      }
    });
    add(askRow, askDelBtn, askKeepBtn);
    add(ask, askText, askRow);
    var deleteNote = el("p", "note error");
    deleteNote.hidden = true;

    add(form, nameSec.node, progSec.node, devSec.node, endLine, acts, ask, deleteNote);
    add(node, bar, problem, form);

    // Drawing

    function savedName() {
      var s = ed.saved || {};
      return str(s.name) || str(s.program) || "this game profile";
    }

    function title() {
      if (ed.desktop) {
        return "Desktop";
      }
      var d = ed.draft;
      var name = d ? d.name.trim() || d.program.trim() : "";
      if (name) {
        return name;
      }
      return ed.id === "" ? "New game profile" : "Game profile";
    }

    function fillSelect(sel, choices) {
      sel.replaceChildren.apply(sel, choices.map(function (c) {
        var o = el("option", "", c.label);
        o.value = c.value;
        return o;
      }));
    }

    function drawProgram() {
      var choices = programChoices(ed.running, ed.draft.program).map(function (p) {
        return { value: p, label: p };
      });
      if (!ed.draft.program) {
        choices.unshift({ value: "", label: "Choose a program" });
      }
      var shape = JSON.stringify(choices);
      if (shape !== progShape) {
        progShape = shape;
        fillSelect(progIn, choices);
      }
      if (progIn.value !== ed.draft.program) {
        progIn.value = ed.draft.program;
      }
      busy(refreshBtn, ed.runningBusy, "Refresh", "Refreshing");
      busy(browseBtn, ed.picking, "Browse", "Choosing");
      setText(progErr, [ed.runningError, ed.pickError, ed.fieldErrors.program].filter(Boolean).join("\n"));
      setText(nameErr, ed.fieldErrors.name || "");
    }

    function drawDevices() {
      var choices = profileChoices(ed.saved.profiles, ed.desktop);
      var shape = JSON.stringify(choices);
      if (shape !== kb7Shape) {
        kb7Shape = shape;
        fillSelect(kb7In, choices);
      }
      var v = choiceValue(ed.draft.kb7);
      if (kb7In.value !== v) {
        kb7In.value = v;
      }
      var n = ed.draft.kb7;
      // A slot without a profile gets one when the game is saved; until
      // then there is nothing to edit.
      var made = (Array.isArray(ed.saved.profiles) ? ed.saved.profiles : []).some(function (p) {
        return p && p.number === n;
      });
      editBtn.hidden = typeof n !== "number" || !made;
      if (typeof n === "number") {
        var label = "Profile " + (n + 1);
        choices.forEach(function (c) {
          if (c.value === String(n)) {
            label = c.label;
          }
        });
        editBtn.setAttribute("aria-label", "Edit " + label);
      }
    }

    function drawBar() {
      var dirty = isDirty();
      var lines = [];
      var cls = "bbar-text";
      if (ed.changed) {
        lines.push("This game profile was changed outside Commander since you opened it. Save to replace that change, or Discard to load it.");
        cls += " warm";
      } else if (!dirty) {
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
      bar.hidden = lines.length === 0;
      bar.classList.toggle("dirty", !!ed.changed);
    }

    function draw() {
      var t = title();
      if (head.textContent !== t) {
        head.textContent = t;
      }
      setText(problem, ed.loadError);
      var ready = !!ed.saved && !!ed.draft;
      form.hidden = !ready;
      if (ready) {
        nameSec.node.hidden = ed.desktop;
        progSec.node.hidden = ed.desktop;
        if (nameIn.value !== ed.draft.name) {
          nameIn.value = ed.draft.name;
        }
        if (!ed.desktop) {
          drawProgram();
        }
        drawDevices();
        setText(endLine, ed.desktop ? "Shown when no game runs." : "When the program ends: back to Desktop.");
        deleteBtn.hidden = ed.desktop || ed.id === "";
        ask.hidden = !ed.asking || deleteBtn.hidden;
        setText(askText, "Delete " + savedName() + "?");
        busy(askDelBtn, ed.deleting, "Delete", "Deleting");
        setText(deleteNote, ed.deleteNote);
      }
      drawBar();
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
      ed.fieldErrors = {};
      ed.deleteNote = "";
      draw();
    }

    // Loading and saving

    function isDirty() {
      return !!ed.saved && !!ed.draft && !same(ed.draft, draftOf(ed.saved));
    }

    // load fetches the game profile. A draft with changes stays (an edit may
    // have come in while the call ran); otherwise the draft becomes the file.
    function load() {
      var gen = ed.gen;
      return api.call("gameProfile", { id: ed.id }).then(function (res) {
        if (gen !== ed.gen || !res || typeof res !== "object") {
          return;
        }
        var wasDirty = isDirty();
        if (!Array.isArray(res.profiles)) {
          res.profiles = [];
        }
        ed.loadError = "";
        ed.saved = res;
        if (!wasDirty || !ed.draft) {
          ed.draft = draftOf(res);
        }
        draw();
      }, function (err) {
        if (gen !== ed.gen) {
          return;
        }
        ed.loadError = api.plainError("Could not read this game profile", err);
        draw();
      });
    }

    // loadRunning asks which programs run now.
    function loadRunning() {
      if (ed.runningBusy) {
        return;
      }
      ed.runningBusy = true;
      ed.runningError = "";
      draw();
      api.call("runningPrograms").then(function (res) {
        ed.runningBusy = false;
        ed.running = res && Array.isArray(res.programs) ? res.programs : [];
        draw();
      }, function (err) {
        ed.runningBusy = false;
        ed.runningError = api.plainError("Could not list the running programs", err);
        draw();
      });
    }

    // browse lets the player pick the game's program in a file dialog.
    function browse() {
      if (ed.picking) {
        return;
      }
      var gen = ed.gen;
      ed.picking = true;
      ed.pickError = "";
      draw();
      api.call("__pickProgram").then(function (res) {
        ed.picking = false;
        var name = fileName(res && res.path);
        if (gen === ed.gen && name && ed.draft && !ed.desktop) {
          ed.draft.program = name;
          changed();
          return;
        }
        draw();
      }, function (err) {
        ed.picking = false;
        ed.pickError = api.plainError("Could not open the file dialog", err);
        draw();
      });
    }

    // show puts the game with this id in the editor: "desktop", a game's
    // id, or "" for a new one.
    function show(id) {
      if (id === ed.id) {
        return;
      }
      ed.gen += 1;
      ed.id = id;
      ed.desktop = id === "desktop";
      ed.saved = null;
      ed.draft = null;
      ed.loadError = "";
      ed.notes = [];
      ed.barError = "";
      ed.fieldErrors = {};
      ed.changed = false;
      ed.asking = false;
      ed.deleteNote = "";
      ed.pickError = "";
      draw();
      load();
      if (!ed.desktop) {
        loadRunning();
      }
    }

    // refresh reads the game profile again when nothing is being edited,
    // so a change made outside Commander shows.
    function refresh() {
      if (ed.id === null || ed.saving || ed.deleting || ed.changed) {
        return;
      }
      if (isDirty()) {
        outside();
        return;
      }
      load();
    }

    // outside looks whether the file was changed outside Commander while
    // the player has unsaved edits here: the edits stay, and the bar says
    // so. Save then replaces that change, Discard loads it.
    function outside() {
      var was = ed.saved ? ed.saved.version : "";
      var p = load();
      if (p && p.then) {
        p.then(function () {
          if (ed.saved && was && ed.saved.version !== was && isDirty() && !ed.saving) {
            ed.changed = true;
            draw();
          }
        });
      }
    }

    function discard() {
      if (!ed.saved) {
        return;
      }
      ed.draft = draftOf(ed.saved);
      ed.changed = false;
      ed.asking = false;
      changed();
    }

    // save saves the draft. Its Promise says whether it went through.
    function save() {
      if (ed.saving) {
        return ed.saving;
      }
      if (!ed.saved || !ed.draft || ed.deleting) {
        return Promise.resolve(false);
      }
      var gen = ed.gen;
      var oldId = ed.id;
      var before = copy(ed.draft);
      var sent = tidy(ed.draft);
      if (ed.desktop) {
        sent.program = "";
      }
      ed.barError = "";
      ed.fieldErrors = {};
      ed.deleteNote = "";
      ed.saving = api.call("saveGameProfile", {
        id: ed.id, version: str(ed.saved.version), name: sent.name, program: sent.program, kb7: sent.kb7
      }).then(function (res) {
        ed.saving = null;
        if (gen !== ed.gen) {
          return false;
        }
        res = res && typeof res === "object" ? res : {};
        if (res.ok) {
          ed.changed = false;
          if (!ed.desktop && str(res.id)) {
            ed.id = res.id;
          }
          ed.saved.id = ed.id;
          ed.saved.version = str(res.version);
          ed.saved.name = sent.name;
          ed.saved.program = sent.program;
          ed.saved.kb7 = sent.kb7;
          if (same(ed.draft, before)) {
            ed.draft = copy(sent);
          }
          ed.notes = saveNotes(res);
          draw();
          if (ed.id !== oldId && api.onSaved) {
            api.onSaved(oldId, ed.id);
          }
          // The game as written.
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
        // A refusal about one field is said at that field.
        if (res.field === "name" || res.field === "program") {
          ed.fieldErrors[res.field] = str(res.error);
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

    function askDelete() {
      if (ed.saving || ed.deleting) {
        return;
      }
      ed.asking = true;
      ed.deleteNote = "";
      draw();
      askKeepBtn.focus();
    }

    function doDelete() {
      if (ed.saving || ed.deleting || !ed.saved) {
        return;
      }
      var gen = ed.gen;
      var name = savedName();
      ed.deleting = true;
      draw();
      api.call("deleteGameProfile", { id: ed.id, version: str(ed.saved.version) }).then(function (res) {
        ed.deleting = false;
        if (gen !== ed.gen) {
          return;
        }
        res = res && typeof res === "object" ? res : {};
        ed.asking = false;
        if (res.ok) {
          // Nothing is left to save.
          ed.draft = draftOf(ed.saved);
          draw();
          if (api.onDeleted) {
            api.onDeleted();
          }
          ed.notes = deleteNotes(name, res);
          draw();
          return;
        }
        if (res.changed) {
          ed.deleteNote = "This game profile was changed outside Commander since you opened it. Check it and delete again if you still want to.";
          load();
        } else {
          ed.deleteNote = api.plainError("Could not delete", str(res.error) || "Commander did not say why");
        }
        draw();
      }, function (err) {
        ed.deleting = false;
        if (gen === ed.gen) {
          ed.asking = false;
          ed.deleteNote = api.plainError("Could not delete", err);
          draw();
        }
      });
    }

    draw();

    return {
      head: head,
      node: node,
      show: show,
      // shows is the id of the game in the editor.
      shows: function () {
        return ed.id;
      },
      refresh: refresh,
      dirty: isDirty,
      save: save,
      discard: discard
    };
  }

  window.CommanderGames = { create: create, logic: logic };
})();
