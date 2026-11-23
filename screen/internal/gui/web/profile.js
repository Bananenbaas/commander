/* SPDX-License-Identifier: GPL-2.0-or-later */
"use strict";

// The KB7 page's Mixer and Profile tabs: the volumes the keyboard's mixer
// page follows, read every second while the tab shows, and the profile's
// name on the keyboard, its slot, the game profiles that use it, Duplicate
// and Delete. Built with createElement and textContent only.

(function () {
  var MAX_NAME = 19; // the longest name the keyboard keeps
  var POLL_MS = 1000;
  var FIVE = "The KB7 has five profiles and all five are in use.";

  // Plain functions, no page involved.

  function str(v) {
    return typeof v === "string" ? v : "";
  }

  // level is a volume as a whole percentage from 0 to 100.
  function level(v) {
    var n = Math.round(Number(v));
    if (!isFinite(n) || n < 0) {
      return 0;
    }
    return n > 100 ? 100 : n;
  }

  // orderPrograms puts the programs that play sound first, each group in
  // the order Commander gave.
  function orderPrograms(list) {
    var progs = (Array.isArray(list) ? list : []).filter(function (p) {
      return p && typeof p === "object";
    });
    return progs.filter(function (p) {
      return !!p.active;
    }).concat(progs.filter(function (p) {
      return !p.active;
    }));
  }

  function slotLine(n) {
    return "Slot " + (n + 1) + " of 5";
  }

  function usedByLine(names) {
    var list = (Array.isArray(names) ? names : []).filter(function (n) {
      return typeof n === "string" && n;
    });
    if (list.length === 0) {
      return "No game profile uses this profile.";
    }
    return "Used by: " + list.join(", ");
  }

  // changeLine is the line after a change that went through.
  function changeLine(res) {
    return res.change === "apply" ? "Putting the profile on the KB7" : "";
  }

  function migratedLine(res) {
    return res.migrated ? "Programs that switch profiles are now game profiles in the sidebar." : "";
  }

  function backups(res) {
    return Array.isArray(res.backups) ? res.backups : [];
  }

  // nameNotes are the lines after a name save that went through.
  function nameNotes(res) {
    var lines = ["Saved.", changeLine(res)];
    var b = backups(res);
    if (b.length > 0) {
      lines.push("Your original file was copied to " + b.join(", ") + " before saving; Commander does not keep comments.");
    }
    lines.push(migratedLine(res));
    return lines.filter(Boolean);
  }

  // duplicateNotes are the lines after a duplicate, shown on the new
  // profile.
  function duplicateNotes(from, res) {
    var lines = ["This is a copy of " + from + ".", changeLine(res)];
    var b = backups(res);
    if (b.length > 0) {
      lines.push("Your original file was copied to " + b.join(", ") + " before saving; Commander does not keep comments.");
    }
    lines.push(migratedLine(res));
    return lines.filter(Boolean);
  }

  // deleteNotes are the lines after a delete, shown on the profile the page
  // goes to.
  function deleteNotes(name, res) {
    var lines = [name + " was deleted."];
    var b = backups(res);
    if (b.length > 0) {
      lines.push("Its file was moved to " + b.join(", ") + ", so it can be restored.");
    }
    lines.push(changeLine(res));
    lines.push(migratedLine(res));
    return lines.filter(Boolean);
  }

  // refusal is the line for an answer that did not go through: Commander's
  // sentence as it is (it may name a game such as "Elite: Dangerous").
  function refusal(lead, res) {
    return lead + ": " + (str(res.error) || "Commander did not say why");
  }

  var logic = {
    level: level,
    orderPrograms: orderPrograms,
    slotLine: slotLine,
    usedByLine: usedByLine,
    nameNotes: nameNotes,
    duplicateNotes: duplicateNotes,
    deleteNotes: deleteNotes,
    refusal: refusal
  };

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
    n.hidden = !text;
  }

  // The Mixer tab. api: call, el, plainError. start and stop say whether
  // the tab shows: while it does, it asks every second, one call at a time.

  function createMixer(api) {
    var el = api.el;
    var m = {
      on: false,
      asking: false, // a mixer call runs
      timer: null,
      answer: null,
      error: ""
    };

    var node = el("div", "mixer");
    var lead = el("p", "mixer-lead", "The keyboard's volume mixer page follows Windows by itself while Commander runs.");
    var problem = el("p", "note error");
    problem.hidden = true;
    var body = el("div", "mixer-body");
    add(node, lead, problem, body);

    function row(label, sub, v) {
      var r = el("div", "mrow");
      var name = el("span", "mname", label);
      name.title = label;
      r.appendChild(name);
      var n = level(v.level);
      var bar = el("div", "mbar" + (v.muted ? " muted" : ""));
      bar.setAttribute("role", "meter");
      bar.setAttribute("aria-label", label);
      bar.setAttribute("aria-valuemin", "0");
      bar.setAttribute("aria-valuemax", "100");
      bar.setAttribute("aria-valuenow", String(n));
      bar.setAttribute("aria-valuetext", n + " %" + (v.muted ? ", muted" : ""));
      var fill = el("span", "mfill");
      fill.style.width = n + "%";
      bar.appendChild(fill);
      r.appendChild(bar);
      r.appendChild(el("span", "mlevel", n + " %"));
      r.appendChild(el("span", "mstate", v.muted ? "muted" : sub));
      return r;
    }

    function draw() {
      setText(problem, m.error);
      var a = m.answer;
      var kids = [];
      if (!a) {
        if (!m.error) {
          kids.push(el("p", "note", "Reading the volumes"));
        }
      } else if (!a.available) {
        kids.push(el("p", "note", "The volume mixer is not available right now."));
      } else {
        var sys = a.system && typeof a.system === "object" ? a.system : {};
        var sysSec = el("div", "msec");
        sysSec.appendChild(el("h3", "label-caps", "System volume"));
        sysSec.appendChild(row(str(sys.name) || "Speakers", "", sys));
        kids.push(sysSec);
        var progSec = el("div", "msec");
        progSec.appendChild(el("h3", "label-caps", "Programs"));
        var progs = orderPrograms(a.programs);
        if (progs.length === 0) {
          progSec.appendChild(el("p", "note", "No program uses sound right now."));
        }
        progs.forEach(function (p) {
          progSec.appendChild(row(str(p.name) || "Program", p.active ? "playing" : "", p));
        });
        kids.push(progSec);
      }
      body.replaceChildren.apply(body, kids);
    }

    function ask() {
      m.timer = null;
      if (!m.on || m.asking) {
        return;
      }
      m.asking = true;
      api.call("mixer").then(function (res) {
        m.asking = false;
        m.answer = res && typeof res === "object" ? res : { available: false };
        m.error = "";
        next();
      }, function (err) {
        m.asking = false;
        m.error = api.plainError("Could not read the volume mixer", err);
        next();
      });
    }

    // next draws the answer and, while the tab shows, asks again in a
    // second.
    function next() {
      if (!m.on) {
        return;
      }
      draw();
      if (m.timer === null) {
        m.timer = setTimeout(ask, POLL_MS);
      }
    }

    function start() {
      if (m.on) {
        return;
      }
      m.on = true;
      // A call that still runs asks again when it answers.
      if (!m.asking) {
        ask();
      }
    }

    function stop() {
      m.on = false;
      if (m.timer !== null) {
        clearTimeout(m.timer);
        m.timer = null;
      }
    }

    draw();
    return { node: node, start: start, stop: stop };
  }

  // The Profile tab: one editor for the window's life, shown for the KB7
  // profile picked. api: call, el, plainError, onDirtyChange,
  // whenSaved(go) to run go once unsaved changes are dealt with,
  // onDuplicated(n) after a duplicate made profile n, onDeleted(n) after
  // profile n was deleted.

  function createProfile(api) {
    var el = api.el;

    var ed = {
      profile: -1,
      saved: null, // the profileInfo answer
      draft: "", // the name being edited
      loadError: "",
      saving: null, // the Promise of a name save that runs
      notes: [],
      barError: "",
      changed: false, // the name changed outside Commander since it was read
      duplicating: false,
      dupNote: "",
      asking: false, // the delete question shows
      deleting: false,
      deleteNote: "",
      gen: 0, // counts shows, so an overtaken answer is ignored
      wasDirty: false
    };

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

    function section(title) {
      var sec = el("section", "section");
      sec.appendChild(el("h2", "", title));
      var card = el("div", "card");
      sec.appendChild(card);
      return { node: sec, card: card };
    }

    // Nodes, made once and kept: a redraw only changes them, so the name
    // being typed keeps its focus and text.

    var node = el("div", "pedit");

    var bar = el("div", "bbar");
    bar.hidden = true;
    var barText = el("p", "bbar-text");
    barText.setAttribute("aria-live", "polite");
    // Save and Discard are the window's own bar (app.js): one place for
    // every page.
    bar.appendChild(barText);

    var problem = el("p", "note error");
    problem.hidden = true;

    var form = el("div", "pform");

    var nameSec = section("Name on the keyboard");
    var nameRow = el("div", "row");
    var nameIn = el("input", "binput pname");
    nameIn.type = "text";
    nameIn.id = "pf-name";
    nameIn.autocomplete = "off";
    nameIn.maxLength = MAX_NAME;
    nameIn.setAttribute("aria-label", "Name on the keyboard");
    nameIn.setAttribute("aria-describedby", "pf-name-hint");
    nameIn.addEventListener("input", function () {
      if (ed.saved) {
        ed.draft = nameIn.value;
        changed();
      }
    });
    add(nameRow, nameIn);
    var hint = el("p", "note", "Empty means: leave the keyboard's name as it is.");
    hint.id = "pf-name-hint";
    add(nameSec.card, nameRow, hint);

    var aboutSec = section("Profile");
    var slot = el("p", "pline");
    var users = el("p", "pline");
    add(aboutSec.card, slot, users);

    var acts = el("div", "row pacts");
    var dupBtn = btn("Duplicate", "btn", function () {
      duplicate();
    });
    var deleteBtn = btn("Delete", "btn danger", function () {
      askDelete();
    });
    add(acts, dupBtn, deleteBtn);
    var dupNote = el("p", "note");
    dupNote.hidden = true;

    var ask = el("div", "banner ask");
    ask.setAttribute("role", "group");
    ask.setAttribute("aria-label", "Delete profile");
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

    add(form, nameSec.node, aboutSec.node, acts, dupNote, ask, deleteNote);
    add(node, bar, problem, form);

    // Drawing

    function label() {
      var s = ed.saved || {};
      return str(s.name) || "Profile " + (ed.profile + 1);
    }

    function drawBar() {
      var dirty = isDirty();
      var lines = [];
      var cls = "bbar-text";
      if (ed.changed) {
        lines.push("This profile's name was changed outside Commander since you opened it. Save again to replace that change, or Discard to load it.");
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
      setText(problem, ed.loadError);
      var ready = !!ed.saved;
      form.hidden = !ready;
      if (ready) {
        if (nameIn.value !== ed.draft) {
          nameIn.value = ed.draft;
        }
        setText(slot, slotLine(ed.profile));
        setText(users, usedByLine(ed.saved.usedBy));
        var full = !ed.saved.canDuplicate;
        busy(dupBtn, ed.duplicating, "Duplicate", "Duplicating");
        if (full && !ed.duplicating) {
          dupBtn.setAttribute("aria-disabled", "true");
        }
        dupNote.classList.toggle("error", !full && !!ed.dupNote);
        setText(dupNote, full ? FIVE : ed.dupNote);
        ask.hidden = !ed.asking;
        setText(askText, "Delete " + label() + "? Its file is moved to the backup folder, so it can be restored.");
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

    function changed() {
      ed.notes = [];
      ed.barError = "";
      ed.dupNote = "";
      ed.deleteNote = "";
      draw();
    }

    // Loading and saving

    function isDirty() {
      return !!ed.saved && ed.draft.trim() !== str(ed.saved.name);
    }

    // load fetches the profile. A name with changes stays (an edit may have
    // come in while the call ran); otherwise the field becomes the file's.
    function load() {
      var gen = ed.gen;
      return api.call("profileInfo", { profile: ed.profile }).then(function (res) {
        if (gen !== ed.gen || !res || typeof res !== "object") {
          return;
        }
        var wasDirty = isDirty();
        ed.loadError = "";
        if (ed.saved && wasDirty && str(res.name) === str(ed.saved.name)) {
          // Only another part of the file changed: the name being edited
          // still replaces the one it started from.
          ed.saved.version = str(res.version);
          ed.saved.usedBy = res.usedBy;
          ed.saved.canDuplicate = res.canDuplicate;
        } else if (!wasDirty || !ed.saved) {
          ed.saved = res;
          ed.draft = str(res.name);
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

    // show puts KB7 profile n in the editor.
    function show(n) {
      if (n === ed.profile) {
        return;
      }
      ed.gen += 1;
      ed.profile = n;
      ed.saved = null;
      ed.draft = "";
      ed.loadError = "";
      ed.notes = [];
      ed.barError = "";
      ed.changed = false;
      ed.dupNote = "";
      ed.asking = false;
      ed.deleteNote = "";
      draw();
      if (n >= 0) {
        load();
      }
    }

    // refresh reads the profile again when nothing runs, so a change made
    // elsewhere (the Buttons tab, another program) shows. A name being
    // edited stays.
    function refresh() {
      if (ed.profile < 0 || ed.saving || ed.duplicating || ed.deleting || ed.changed) {
        return;
      }
      load();
    }

    function discard() {
      if (!ed.saved) {
        return;
      }
      var outside = ed.changed;
      ed.draft = str(ed.saved.name);
      ed.changed = false;
      changed();
      // The name as it is in the file now.
      if (outside && ed.profile >= 0) {
        load();
      }
    }

    // save saves the name. The file is read first: a save of the Buttons or
    // Lighting tab changes the file's version but not its name, and only
    // the name is replaced. Its Promise says whether it went through.
    function save() {
      if (ed.saving) {
        return ed.saving;
      }
      if (!ed.saved || ed.duplicating || ed.deleting) {
        return Promise.resolve(false);
      }
      var gen = ed.gen;
      var sent = ed.draft.trim();
      ed.barError = "";
      ed.dupNote = "";
      ed.deleteNote = "";
      var fail = function (err) {
        ed.saving = null;
        if (gen === ed.gen) {
          ed.barError = api.plainError("Could not save", err);
          draw();
        }
        return false;
      };
      ed.saving = api.call("profileInfo", { profile: ed.profile }).then(function (now) {
        if (gen !== ed.gen) {
          ed.saving = null;
          return false;
        }
        var version = str(ed.saved.version);
        if (now && typeof now === "object" && str(now.name) === str(ed.saved.name) && str(now.version)) {
          version = now.version;
        }
        return api.call("saveProfileName", { profile: ed.profile, version: version, name: sent }).then(function (res) {
          ed.saving = null;
          if (gen !== ed.gen) {
            return false;
          }
          res = res && typeof res === "object" ? res : {};
          if (res.ok) {
            ed.changed = false;
            ed.saved.name = sent;
            ed.saved.version = str(res.version);
            if (ed.draft.trim() === sent) {
              ed.draft = sent;
            }
            ed.notes = nameNotes(res);
            draw();
            load();
            return true;
          }
          if (res.changed) {
            ed.changed = true;
            draw();
            // The file's version now, so Save again replaces that change;
            // Discard loads it.
            api.call("profileInfo", { profile: ed.profile }).then(function (now) {
              if (gen === ed.gen && ed.saved && now && typeof now === "object") {
                ed.saved.version = str(now.version);
              }
            }, function () {});
            return false;
          }
          ed.barError = refusal("Could not save", res);
          draw();
          return false;
        }, fail);
      }, fail);
      draw();
      return ed.saving;
    }

    function duplicate() {
      if (!ed.saved || ed.saving || ed.duplicating || ed.deleting || !ed.saved.canDuplicate) {
        return;
      }
      var go = function () {
        if (!ed.saved || ed.saving || ed.duplicating || ed.deleting) {
          return;
        }
        var gen = ed.gen;
        var from = label();
        ed.duplicating = true;
        ed.dupNote = "";
        ed.notes = [];
        draw();
        api.call("duplicateProfile", { profile: ed.profile }).then(function (res) {
          ed.duplicating = false;
          if (gen !== ed.gen) {
            return;
          }
          res = res && typeof res === "object" ? res : {};
          if (res.ok && typeof res.newProfile === "number") {
            if (api.onDuplicated) {
              api.onDuplicated(res.newProfile);
            }
            ed.notes = duplicateNotes(from, res);
            draw();
            return;
          }
          ed.dupNote = refusal("Could not duplicate", res);
          draw();
          load();
        }, function (err) {
          ed.duplicating = false;
          if (gen === ed.gen) {
            ed.dupNote = api.plainError("Could not duplicate", err);
            draw();
          }
        });
      };
      // The copy is made of the file as saved.
      if (api.whenSaved) {
        api.whenSaved(go);
      } else {
        go();
      }
    }

    function askDelete() {
      if (!ed.saved || ed.saving || ed.duplicating || ed.deleting) {
        return;
      }
      ed.asking = true;
      ed.deleteNote = "";
      draw();
      askKeepBtn.focus();
    }

    function doDelete() {
      if (!ed.saved || ed.saving || ed.duplicating || ed.deleting) {
        return;
      }
      var gen = ed.gen;
      var n = ed.profile;
      var name = label();
      ed.deleting = true;
      draw();
      api.call("deleteProfile", { profile: n, version: str(ed.saved.version) }).then(function (res) {
        ed.deleting = false;
        if (gen !== ed.gen) {
          return;
        }
        res = res && typeof res === "object" ? res : {};
        ed.asking = false;
        if (res.ok) {
          // Nothing is left to save.
          ed.draft = str(ed.saved.name);
          draw();
          if (api.onDeleted) {
            api.onDeleted(n);
          }
          ed.notes = deleteNotes(name, res);
          draw();
          return;
        }
        if (res.changed) {
          ed.deleteNote = "This profile was changed outside Commander since you opened it. Check it and delete again if you still want to.";
          load();
        } else {
          ed.deleteNote = refusal("Could not delete", res);
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
      node: node,
      show: show,
      refresh: refresh,
      dirty: isDirty,
      save: save,
      discard: discard,
      // Nothing is recorded on this tab.
      stopRecording: function () {}
    };
  }

  window.CommanderProfile = { createMixer: createMixer, create: createProfile, logic: logic };
})();
