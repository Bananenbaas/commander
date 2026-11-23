/* SPDX-License-Identifier: GPL-2.0-or-later */
"use strict";

// The advanced view of the Lighting tab: the whole layer stack, top first,
// with on/off, reordering and removing, and the selected layer's settings.
// Every field is drawn from the effect's settings as lightingInfo lists them
// (the lighting engine's own declarations), so a new effect needs no code
// here. The Lighting tab (lighting.js) holds the draft, the preview and the
// key picker; this view edits the draft's stack through it. Built with
// createElement and textContent only.

(function () {
  var NEW_COLOR = "#00AAFF";
  var SECOND_COLOR = "#FF9A3D";

  // The effects' names as the player reads them; any other effect shows
  // its own name with a capital.
  var EFFECT_NAMES = {
    static: "One colour", fixed: "Keys in one colour", wave: "Wave",
    breathing: "Breathing", ripple: "Ripple", state: "Key state"
  };

  // Logic: plain functions, no page involved.
  //
  // The stack is deviceLight's light: {fps, layers}, bottom first, each
  // layer {effect, enabled, keys, leds, settings} with the settings as the
  // file holds them. A layer whose keys were picked on the picture also
  // holds ledsEdited (its leds are the picked LEDs) and orig (its keys and
  // LEDs before the first pick); neither goes to Commander.

  function copy(v) {
    return JSON.parse(JSON.stringify(v));
  }

  function str(v) {
    return typeof v === "string" ? v : "";
  }

  function num(v) {
    return typeof v === "number" && isFinite(v) ? v : 0;
  }

  function capital(s) {
    s = str(s);
    return s.charAt(0).toUpperCase() + s.slice(1);
  }

  function effectName(name) {
    return Object.prototype.hasOwnProperty.call(EFFECT_NAMES, name) ? EFFECT_NAMES[name] : capital(name);
  }

  // settingLabel is a setting's name as a label.
  function settingLabel(name) {
    return capital(str(name).replace(/_/g, " "));
  }

  function layersOf(light) {
    return light && Array.isArray(light.layers) ? light.layers : [];
  }

  function specOf(effects, name) {
    var list = Array.isArray(effects) ? effects : [];
    for (var i = 0; i < list.length; i++) {
      if (list[i] && list[i].name === name) {
        return list[i];
      }
    }
    return null;
  }

  function settingsOf(spec) {
    return spec && Array.isArray(spec.settings) ? spec.settings : [];
  }

  // displayOrder lists the file's layer indexes (bottom first) as the page
  // shows them: top first.
  function displayOrder(count) {
    var out = [];
    for (var i = count - 1; i >= 0; i--) {
      out.push(i);
    }
    return out;
  }

  // moveLayer moves the layer at index from (bottom first) to index to; a
  // new list.
  function moveLayer(layers, from, to) {
    var out = layers.slice();
    if (from < 0 || from >= out.length || to < 0 || to >= out.length || from === to) {
      return out;
    }
    var moved = out.splice(from, 1)[0];
    out.splice(to, 0, moved);
    return out;
  }

  // parseDuration reads a duration as the file writes it ("4s", "600ms",
  // "1m30s") in seconds; NaN when it is not one.
  var DURATION_UNITS = { ns: 1e-9, us: 1e-6, "µs": 1e-6, "μs": 1e-6, ms: 1e-3, s: 1, m: 60, h: 3600 };
  var DURATION_PART = /^(\d+\.?\d*|\.\d+)(ns|us|µs|μs|ms|s|m|h)/;

  function parseDuration(v) {
    if (typeof v !== "string") {
      return NaN;
    }
    var s = v.trim();
    var sign = 1;
    if (s.charAt(0) === "-" || s.charAt(0) === "+") {
      sign = s.charAt(0) === "-" ? -1 : 1;
      s = s.slice(1);
    }
    if (s === "0") {
      return 0;
    }
    if (!s) {
      return NaN;
    }
    var total = 0;
    while (s) {
      var m = DURATION_PART.exec(s);
      if (!m) {
        return NaN;
      }
      total += parseFloat(m[1]) * DURATION_UNITS[m[2]];
      s = s.slice(m[0].length);
    }
    return sign * total;
  }

  // formatSeconds writes seconds as a duration for the file: 1.5 is "1.5s".
  function formatSeconds(sec) {
    return String(Math.round(sec * 1000) / 1000) + "s";
  }

  function bounded(setting) {
    return !(num(setting.min) === 0 && num(setting.max) === 0);
  }

  // stepFor is a slider's step for a range.
  function stepFor(min, max) {
    var span = max - min;
    if (span <= 2) {
      return 0.01;
    }
    if (span <= 20) {
      return 0.05;
    }
    return 0.1;
  }

  // fieldOf says what the page draws for a setting, from its kind.
  function fieldOf(setting) {
    var min = num(setting.min);
    var max = num(setting.max);
    switch (setting.kind) {
      case "color":
        return { type: "color" };
      case "colors":
        return { type: "colors" };
      case "number":
        if (bounded(setting)) {
          return { type: "range", min: min, max: max, step: stepFor(min, max) };
        }
        return { type: "number" };
      case "choice":
        return { type: "select", choices: Array.isArray(setting.choices) ? setting.choices.slice() : [] };
      case "duration":
        return bounded(setting) ? { type: "seconds", min: min, max: max } : { type: "seconds" };
      case "chord":
        return { type: "chord" };
      case "name":
        return { type: "text" };
    }
    return { type: "text" };
  }

  function given(settings, name) {
    return !!settings && Object.prototype.hasOwnProperty.call(settings, name) &&
      settings[name] !== null && settings[name] !== undefined;
  }

  // newLayer is a new layer of an effect, on and everywhere, with a colour
  // for each colour setting it must have.
  function newLayer(spec) {
    var settings = {};
    settingsOf(spec).forEach(function (st) {
      if (!st.required) {
        return;
      }
      if (st.kind === "color") {
        settings[st.name] = NEW_COLOR;
      } else if (st.kind === "colors") {
        settings[st.name] = [NEW_COLOR, SECOND_COLOR];
      }
    });
    return { effect: str(spec && spec.name), enabled: true, keys: [], leds: [], settings: settings };
  }

  function ledSet(list) {
    var out = [];
    (Array.isArray(list) ? list : []).forEach(function (i) {
      if (typeof i === "number" && out.indexOf(i) < 0) {
        out.push(i);
      }
    });
    return out.sort(function (a, b) {
      return a - b;
    });
  }

  function sameSet(a, b) {
    return JSON.stringify(ledSet(a)) === JSON.stringify(ledSet(b));
  }

  // pickLed adds the LED to the layer's keys or takes it out, in place.
  // Picked keys are one name per LED; back at the LEDs the layer started
  // with, it gets its keys as written again.
  function pickLed(layer, i, names) {
    if (!layer.orig) {
      layer.orig = { keys: (layer.keys || []).slice(), leds: ledSet(layer.leds) };
    }
    var set = ledSet(layer.leds);
    var at = set.indexOf(i);
    if (at >= 0) {
      set.splice(at, 1);
    } else {
      set.push(i);
    }
    layer.leds = ledSet(set);
    if (sameSet(layer.leds, layer.orig.leds)) {
      layer.keys = layer.orig.keys.slice();
      delete layer.ledsEdited;
    } else {
      layer.keys = layer.leds.map(function (n) {
        return names[n] || String(n);
      });
      layer.ledsEdited = true;
    }
    return layer;
  }

  // showEverywhere makes the layer cover the whole keyboard, in place.
  function showEverywhere(layer) {
    if (!layer.orig) {
      layer.orig = { keys: (layer.keys || []).slice(), leds: ledSet(layer.leds) };
    }
    layer.keys = [];
    layer.leds = [];
    delete layer.ledsEdited;
    return layer;
  }

  // payload is the stack as saveDeviceLight and lightFrames take it: the
  // values as received or edited, leds only for a layer picked on the
  // picture.
  function payload(light) {
    return {
      fps: typeof light.fps === "number" ? light.fps : 0,
      layers: layersOf(light).map(function (l) {
        var settings = {};
        Object.keys(l.settings || {}).forEach(function (k) {
          if (given(l.settings, k)) {
            settings[k] = l.settings[k];
          }
        });
        var out = {
          effect: str(l.effect), enabled: l.enabled !== false,
          keys: (Array.isArray(l.keys) ? l.keys : []).filter(function (k) {
            return typeof k === "string";
          }),
          settings: settings
        };
        if (l.ledsEdited) {
          out.leds = ledSet(l.leds);
        }
        return out;
      })
    };
  }

  // Errors: Commander names a layer by its place in the file (1 for the
  // bottom one, switched-off layers counted, 0 for the lighting as a
  // whole) and a setting ("" for the layer). The page files them as
  // "L<layer>" and "L<layer>.<setting>"; "" holds the rest.

  function layerKey(n) {
    return "L" + n;
  }

  function settingKey(n, setting) {
    return "L" + n + "." + setting;
  }

  // parseKey reads "L<layer>.<setting>" or "L<layer>"; null for others.
  function parseKey(key) {
    var m = /^L(\d+)(?:\.(.*))?$/.exec(str(key));
    return m ? { layer: Number(m[1]), setting: m[2] === undefined ? "" : m[2] } : null;
  }

  function errorsByLayer(list, count) {
    var out = {};
    (Array.isArray(list) ? list : []).forEach(function (e) {
      var n = e && typeof e.layer === "number" ? e.layer : 0;
      var text = str(e && e.message);
      if (!text) {
        return;
      }
      var key = "";
      if (n >= 1 && n <= count) {
        key = str(e.setting) ? settingKey(n, e.setting) : layerKey(n);
      }
      out[key] = out[key] || [];
      if (out[key].indexOf(text) < 0) {
        out[key].push(text);
      }
    });
    return out;
  }

  // layersWithErrors lists the layers (1-based, bottom first) errors name.
  function layersWithErrors(errors) {
    var out = [];
    Object.keys(errors || {}).forEach(function (k) {
      var p = parseKey(k);
      if (p && (errors[k] || []).length > 0 && out.indexOf(p.layer) < 0) {
        out.push(p.layer);
      }
    });
    return out.sort(function (a, b) {
      return a - b;
    });
  }

  var logic = {
    effectName: effectName, settingLabel: settingLabel, displayOrder: displayOrder,
    moveLayer: moveLayer, parseDuration: parseDuration, formatSeconds: formatSeconds,
    fieldOf: fieldOf,
    newLayer: newLayer, pickLed: pickLed, showEverywhere: showEverywhere, payload: payload,
    layerKey: layerKey, settingKey: settingKey, parseKey: parseKey,
    errorsByLayer: errorsByLayer, layersWithErrors: layersWithErrors
  };

  // The view
  //
  // host is the Lighting tab:
  //   el(tag, cls, text), keys (CommanderKeys)
  //   info()           the lightingInfo answer, or null
  //   light()          the draft's stack, or null
  //   names()          LED index -> the name that picks it alone
  //   edit(key, fn)    fn changes the draft's stack in place; key names
  //                    what changed, for its errors
  //   restack(fn)      the same for a change that moves layers
  //   clear()          the draft becomes no lighting
  //   errors()         key -> [message], as errorsByLayer files them
  //   picking(), startPicking(key), stopPicking()   the key picker
  //   redraw()         draws the tab again

  function create(host) {
    var el = host.el;
    var K = host.keys;
    var selected = -1; // the selected layer, bottom first
    var stackGen = 0; // counts changes that move layers, so the fields are made again
    var dragFrom = -1;

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

    function errLine() {
      var p = el("p", "note error");
      p.hidden = true;
      return p;
    }

    function showErr(p, lines) {
      var text = (lines || []).join("\n");
      setText(p, text);
      p.hidden = !text;
    }

    var uid = 0;
    function nextId(prefix) {
      uid += 1;
      return prefix + uid;
    }

    function effects() {
      var info = host.info();
      return info && Array.isArray(info.effects) ? info.effects : [];
    }

    function layers() {
      return layersOf(host.light());
    }

    function errs(key) {
      return (host.errors() || {})[key] || [];
    }

    var node = el("div", "ladv-stack");

    // The list of layers, top first.
    var listSec = el("div", "llist");
    var listHead = el("h3", "label-caps", "Layers");
    var listNote = el("p", "note", "The top layer covers the ones below it. Drag a layer, or use Up and Down, to change the order.");
    var list = el("ol", "llayers");
    list.setAttribute("aria-label", "Layers, top first");
    var rows = [];

    var addRow = el("div", "row ladd");
    var addId = nextId("l-add-");
    var addSel = el("select", "binput");
    addSel.id = addId;
    addSel.setAttribute("aria-label", "Effect of the new layer");
    var addBtn = smallBtn("Add layer");
    var shownEffects = "";
    addBtn.addEventListener("click", function () {
      var spec = specOf(effects(), addSel.value);
      if (!spec || !host.light()) {
        return;
      }
      host.stopPicking();
      stopRecording();
      var n = layers().length;
      host.restack(function (light) {
        light.layers = layersOf(light).concat([newLayer(spec)]);
      });
      selected = n;
      stackGen += 1;
      if (spec.needsKeys) {
        host.startPicking(settingKey(n + 1, "keys"));
      }
      host.redraw();
    });
    add(addRow, addSel, addBtn);
    add(listSec, listHead, listNote, list, addRow);

    // The selected layer.
    var detail = el("div", "ldetail");
    var detailHead = el("h3", "ldetail-name");
    var detailHelp = el("p", "note");
    var detailErr = errLine(); // errors about the layer, or a setting not shown
    var detailBody = el("div", "lfields");
    add(detail, detailHead, detailHelp, detailErr, detailBody);
    var detailKey = "";
    var fields = []; // {name, draw(layer)} per field shown
    var where = null;

    add(node, listSec, detail);

    // Moving and removing layers.

    function restack(fn, select) {
      host.stopPicking();
      stopRecording();
      host.restack(fn);
      selected = select;
      stackGen += 1;
      host.redraw();
    }

    function move(from, to) {
      if (to < 0 || to >= layers().length || from === to) {
        return;
      }
      restack(function (light) {
        light.layers = moveLayer(layersOf(light), from, to);
      }, to);
    }

    function removeLayer(i) {
      var n = layers().length;
      if (n <= 1) {
        host.stopPicking();
        stopRecording();
        host.clear();
        return;
      }
      // The selection stays on its layer; a removed selected layer passes
      // it to the one that was above it, or below for the top one.
      var next = selected > i ? selected - 1 : selected;
      restack(function (light) {
        light.layers = layersOf(light).filter(function (l, at) {
          return at !== i;
        });
      }, Math.max(0, Math.min(next, n - 2)));
    }

    // A layer's row in the list, at a place counted from the top.
    function makeRow(pos) {
      var li = el("li", "llayer");
      li.draggable = true;
      var grip = el("span", "lgrip");
      grip.setAttribute("aria-hidden", "true");
      var nameBtn = el("button", "llayer-name");
      nameBtn.type = "button";
      var nameText = el("span", "", "");
      var state = el("span", "llayer-state");
      add(nameBtn, nameText, state);
      var sw = el("button", "toggle lswitch");
      sw.type = "button";
      sw.setAttribute("role", "switch");
      var track = el("span", "track");
      track.appendChild(el("span", "knob"));
      sw.appendChild(track);
      var up = smallBtn("Up");
      var down = smallBtn("Down");
      var remove = smallBtn("Remove");
      add(li, grip, nameBtn, sw, up, down, remove);

      function index() {
        return layers().length - 1 - pos;
      }

      nameBtn.addEventListener("click", function () {
        var i = index();
        if (i !== selected) {
          host.stopPicking();
          stopRecording();
          selected = i;
        }
        host.redraw();
      });
      sw.addEventListener("click", function () {
        var i = index();
        var on = layers()[i] && layers()[i].enabled !== false;
        host.edit(layerKey(i + 1), function (light) {
          light.layers[i].enabled = !on;
        });
      });
      // The focus follows the layer to its new row.
      function follow(r, b) {
        if (r) {
          (b === "up" && r.up.disabled ? r.down : (b === "down" && r.down.disabled ? r.up : r[b])).focus();
        }
      }
      up.addEventListener("click", function () {
        move(index(), index() + 1);
        follow(rows[pos - 1], "up");
      });
      down.addEventListener("click", function () {
        move(index(), index() - 1);
        follow(rows[pos + 1], "down");
      });
      remove.addEventListener("click", function () {
        removeLayer(index());
      });

      li.addEventListener("dragstart", function (e) {
        dragFrom = index();
        if (e.dataTransfer) {
          e.dataTransfer.effectAllowed = "move";
          e.dataTransfer.setData("text/plain", String(dragFrom));
        }
        li.classList.add("dragging");
      });
      li.addEventListener("dragend", function () {
        dragFrom = -1;
        li.classList.remove("dragging");
        rows.forEach(function (r) {
          r.node.classList.remove("drop");
        });
      });
      li.addEventListener("dragover", function (e) {
        if (dragFrom < 0) {
          return;
        }
        e.preventDefault();
        if (e.dataTransfer) {
          e.dataTransfer.dropEffect = "move";
        }
        li.classList.toggle("drop", dragFrom !== index());
      });
      li.addEventListener("dragleave", function () {
        li.classList.remove("drop");
      });
      li.addEventListener("drop", function (e) {
        e.preventDefault();
        li.classList.remove("drop");
        var from = dragFrom;
        dragFrom = -1;
        if (from >= 0 && from !== index()) {
          move(from, index());
        }
      });

      return {
        node: li, up: up, down: down,
        draw: function () {
          var all = layers();
          var i = index();
          var layer = all[i];
          var name = effectName(str(layer.effect));
          var on = layer.enabled !== false;
          var flagged = layersWithErrors(host.errors()).indexOf(i + 1) >= 0;
          setText(nameText, name);
          var words = [];
          if (!on) {
            words.push("Off");
          }
          if (flagged) {
            words.push("Needs a fix");
          }
          setText(state, words.join(", "));
          state.hidden = words.length === 0;
          nameBtn.setAttribute("aria-pressed", i === selected ? "true" : "false");
          li.classList.toggle("on", i === selected);
          li.classList.toggle("off", !on);
          li.classList.toggle("flagged", flagged);
          sw.setAttribute("aria-checked", on ? "true" : "false");
          sw.setAttribute("aria-label", name + " on");
          up.disabled = i >= all.length - 1;
          down.disabled = i <= 0;
          up.setAttribute("aria-label", "Move " + name + " up");
          down.setAttribute("aria-label", "Move " + name + " down");
          remove.setAttribute("aria-label", "Remove " + name);
        }
      };
    }

    // Recording a chord setting: one chord, as a button's keys are recorded.
    var rec = null; // {index, name, key}
    var recorder = K.recorder({
      oneChord: true,
      onDone: function (steps) {
        var keys = steps[0] && Array.isArray(steps[0].keys) ? steps[0].keys : [];
        var r = rec;
        if (!r || keys.length === 0) {
          return;
        }
        host.edit(r.key, function (light) {
          var l = layersOf(light)[r.index];
          if (l) {
            l.settings = l.settings || {};
            l.settings[r.name] = keys.join("+");
          }
        });
      },
      onStop: function () {
        rec = null;
        host.redraw();
      },
      onChange: function () {
        host.redraw();
      }
    });

    function stopRecording() {
      recorder.stop(false);
    }

    // Fields of the selected layer. Each gets the layer's index (bottom
    // first) and its setting, and draws from the layer it is given.

    function setSetting(i, name, v) {
      host.edit(settingKey(i + 1, name), function (light) {
        var l = layersOf(light)[i];
        if (l) {
          l.settings = l.settings || {};
          l.settings[name] = v;
        }
      });
    }

    function clearSetting(i, name) {
      host.edit(settingKey(i + 1, name), function (light) {
        var l = layersOf(light)[i];
        if (l && l.settings) {
          delete l.settings[name];
        }
      });
    }

    function colorInput(label, onInput) {
      var input = el("input", "lcolor");
      input.type = "color";
      input.setAttribute("aria-label", label);
      input.addEventListener("input", function () {
        onInput(String(input.value).toUpperCase());
      });
      return input;
    }

    function colorValue(c) {
      var s = str(c);
      return /^#[0-9a-fA-F]{6}$/.test(s) ? s.toLowerCase() : "#000000";
    }

    function makeField(i, st) {
      var name = st.name;
      var f = fieldOf(st);
      var label = settingLabel(name);
      var id = nextId("l-set-");
      var box = el("div", "lfield");
      var head = el("div", "row lfield-head");
      var lab = el("label", "lfield-label", label);
      lab.htmlFor = id;
      var req = el("span", "lreq", "Required");
      req.hidden = !st.required;
      var defTag = el("span", "ldef-tag", "Default");
      var useDef = smallBtn("Use default");
      useDef.setAttribute("aria-label", "Use the default " + label.toLowerCase());
      useDef.addEventListener("click", function () {
        if (rec && rec.index === i && rec.name === name) {
          stopRecording();
        }
        clearSetting(i, name);
      });
      add(head, lab, req, defTag, useDef);
      var ctl = el("div", "row lfield-ctl");
      var help = el("p", "note", str(st.help));
      help.hidden = !st.help;
      var err = errLine();
      add(box, head, ctl, help, err);

      var drawCtl = makeControl(i, st, f, id, label, ctl);
      return {
        name: name,
        node: box,
        draw: function (layer) {
          var has = given(layer.settings, name);
          defTag.hidden = has || !!st.required;
          useDef.hidden = !has || !!st.required;
          box.classList.toggle("ldef", !has);
          box.classList.toggle("lmissing", !has && !!st.required);
          drawCtl(layer, has);
          showErr(err, errs(settingKey(i + 1, name)));
        }
      };
    }

    // makeControl puts the control for a setting into ctl and returns how
    // it draws: draw(layer, has).
    function makeControl(i, st, f, id, label, ctl) {
      var name = st.name;
      var def = st.default;

      function value(layer) {
        return layer.settings[name];
      }

      if (f.type === "color") {
        var c = colorInput(label, function (v) {
          setSetting(i, name, v);
        });
        c.id = id;
        ctl.appendChild(c);
        return function (layer, has) {
          setValue(c, colorValue(has ? value(layer) : def));
        };
      }

      if (f.type === "colors") {
        var listBox = el("div", "lcolor-list");
        var addC = smallBtn("Add colour");
        addC.id = id;
        var crow = [];
        var shown = [];
        add(ctl, listBox, addC);
        var current = function () {
          var layer = layers()[i];
          var v = layer && given(layer.settings, name) ? value(layer) : def;
          return Array.isArray(v) ? v.slice() : [];
        };
        addC.addEventListener("click", function () {
          var cur = current();
          cur.push(cur.length > 0 ? str(cur[cur.length - 1]).toUpperCase() : NEW_COLOR);
          setSetting(i, name, cur);
        });
        var makeCRow = function (at) {
          var r = el("div", "lcolor-row");
          var input = colorInput(label + " " + (at + 1), function (v) {
            var cur = current();
            cur[at] = v;
            setSetting(i, name, cur);
          });
          var rm = smallBtn("Remove");
          rm.setAttribute("aria-label", "Remove " + label.toLowerCase() + " " + (at + 1));
          rm.addEventListener("click", function () {
            var cur = current();
            if (cur.length > 1) {
              cur.splice(at, 1);
              setSetting(i, name, cur);
            }
          });
          add(r, input, rm);
          return { node: r, input: input, remove: rm };
        };
        return function (layer, has) {
          var v = has ? value(layer) : def;
          shown = Array.isArray(v) ? v : [];
          if (crow.length !== shown.length) {
            crow = shown.map(function (x, at) {
              return makeCRow(at);
            });
            listBox.replaceChildren.apply(listBox, crow.map(function (r) {
              return r.node;
            }));
          }
          crow.forEach(function (r, at) {
            setValue(r.input, colorValue(shown[at]));
            r.remove.disabled = shown.length <= 1;
          });
        };
      }

      if (f.type === "range" || f.type === "number") {
        var range = null;
        if (f.type === "range") {
          range = el("input", "brange");
          range.type = "range";
          range.min = String(f.min);
          range.max = String(f.max);
          range.step = String(f.step);
          range.setAttribute("aria-label", label);
          range.addEventListener("input", function () {
            var v = parseFloat(range.value);
            if (isFinite(v)) {
              setSetting(i, name, Math.round(v * 1000) / 1000);
            }
          });
          ctl.appendChild(range);
        }
        var n = el("input", "binput lnum");
        n.type = "number";
        n.id = id;
        n.step = "any";
        if (f.type === "range") {
          n.min = String(f.min);
          n.max = String(f.max);
        }
        n.addEventListener("input", function () {
          var raw = String(n.value).trim();
          if (raw === "") {
            if (!st.required) {
              clearSetting(i, name);
            }
            return;
          }
          var v = parseFloat(raw);
          if (isFinite(v)) {
            setSetting(i, name, v);
          }
        });
        ctl.appendChild(n);
        return function (layer, has) {
          var v = has ? value(layer) : def;
          var shownNum = typeof v === "number" ? v : NaN;
          if (range) {
            setValue(range, String(isFinite(shownNum) ? shownNum : f.min));
          }
          if (has) {
            setValue(n, typeof v === "number" ? String(v) : "");
            n.placeholder = typeof v === "number" ? "" : String(v);
          } else {
            setValue(n, "");
            n.placeholder = isFinite(shownNum) ? String(shownNum) : "";
          }
        };
      }

      if (f.type === "select") {
        var sel = el("select", "binput");
        sel.id = id;
        sel.addEventListener("change", function () {
          if (sel.value === "") {
            clearSetting(i, name);
          } else {
            setSetting(i, name, sel.value);
          }
        });
        ctl.appendChild(sel);
        var optKey = "";
        return function (layer, has) {
          var v = has ? value(layer) : def;
          var raw = typeof v === "string" ? v : (v === null || v === undefined ? "" : JSON.stringify(v));
          var match = "";
          f.choices.forEach(function (ch) {
            if (raw && ch.toLowerCase() === raw.trim().toLowerCase()) {
              match = ch;
            }
          });
          var opts = [];
          if (!has && (def === null || def === undefined)) {
            opts.push(["", st.required ? "Choose" : "Not set"]);
          }
          f.choices.forEach(function (ch) {
            opts.push([ch, capital(ch)]);
          });
          if (raw && !match) {
            opts.push([raw, raw]); // a value the effect does not have, as written
          }
          var k = JSON.stringify(opts);
          if (k !== optKey) {
            optKey = k;
            sel.replaceChildren.apply(sel, opts.map(function (o) {
              var opt = el("option", "", o[1]);
              opt.value = o[0];
              return opt;
            }));
          }
          setValue(sel, match || raw);
        };
      }

      if (f.type === "seconds") {
        var secs = el("input", "binput lnum");
        secs.type = "number";
        secs.id = id;
        secs.step = "0.1";
        if (f.min !== undefined) {
          secs.min = String(f.min);
          secs.max = String(f.max);
        }
        secs.addEventListener("input", function () {
          var raw = String(secs.value).trim();
          if (raw === "") {
            if (!st.required) {
              clearSetting(i, name);
            }
            return;
          }
          var v = parseFloat(raw);
          if (isFinite(v)) {
            setSetting(i, name, formatSeconds(v));
          }
        });
        add(ctl, secs, el("span", "muted", "seconds"));
        return function (layer, has) {
          var v = has ? value(layer) : def;
          var sec = parseDuration(v);
          if (has) {
            setValue(secs, isFinite(sec) ? String(Math.round(sec * 1000) / 1000) : "");
            secs.placeholder = isFinite(sec) ? "" : str(v);
          } else {
            setValue(secs, "");
            secs.placeholder = isFinite(sec) ? String(Math.round(sec * 1000) / 1000) : "";
          }
        };
      }

      if (f.type === "chord") {
        var caps = el("div", "bcaps");
        var none = el("span", "muted", "None");
        var recBtn = smallBtn("Record key");
        recBtn.id = id;
        var recText = el("p", "note", "Press the key or keys.");
        recText.hidden = true;
        recText.setAttribute("aria-live", "polite");
        recBtn.addEventListener("click", function () {
          if (rec && rec.index === i && rec.name === name) {
            stopRecording();
            return;
          }
          host.stopPicking();
          rec = { index: i, name: name, key: settingKey(i + 1, name) };
          recorder.start();
          host.redraw();
        });
        add(ctl, caps, none, recBtn, recText);
        return function (layer, has) {
          var recording = !!(rec && rec.index === i && rec.name === name);
          var v = has ? value(layer) : def;
          var held = recording ? recorder.chord() : (typeof v === "string" && v ? v.split("+") : []);
          if (caps.getAttribute("data-keys") !== held.join(" ")) {
            caps.setAttribute("data-keys", held.join(" "));
            K.caps(held, caps);
          }
          none.hidden = recording || held.length > 0;
          recText.hidden = !recording;
          setText(recBtn, recording ? "Cancel" : "Record key");
          recBtn.setAttribute("aria-pressed", recording ? "true" : "false");
        };
      }

      // text
      var t = el("input", "binput bwide");
      t.type = "text";
      t.id = id;
      t.autocomplete = "off";
      t.addEventListener("input", function () {
        var v = t.value;
        if (v === "") {
          clearSetting(i, name);
        } else {
          setSetting(i, name, v);
        }
      });
      ctl.appendChild(t);
      return function (layer, has) {
        var v = has ? value(layer) : def;
        if (has) {
          setValue(t, typeof v === "string" ? v : JSON.stringify(v));
          t.placeholder = "";
        } else {
          setValue(t, "");
          t.placeholder = typeof v === "string" ? v : "";
        }
      };
    }

    // A setting the effect does not have, as written, with Remove.
    function makeUnknown(i, name) {
      var box = el("div", "lfield");
      var head = el("div", "row lfield-head");
      var raw = el("code", "lraw");
      var rm = smallBtn("Remove");
      rm.setAttribute("aria-label", "Remove " + name);
      rm.addEventListener("click", function () {
        clearSetting(i, name);
      });
      add(head, el("span", "lfield-label", settingLabel(name)), raw, rm);
      var note = el("p", "note", "This effect does not have this setting.");
      add(box, head, note);
      return {
        name: name,
        node: box,
        draw: function (layer) {
          setText(raw, JSON.stringify(layer.settings[name]));
        }
      };
    }

    // Where the layer shows: everywhere or on the keys picked on the picture.
    function makeWhere(i, spec) {
      var key = settingKey(i + 1, "keys");
      var box = el("div", "lfield");
      var head = el("div", "row lfield-head");
      head.appendChild(el("span", "lfield-label", "Where it shows"));
      var req = el("span", "lreq", "Required");
      req.hidden = !(spec && spec.needsKeys);
      head.appendChild(req);
      var seg = el("div", "seg");
      seg.setAttribute("role", "group");
      seg.setAttribute("aria-label", "Where it shows");
      var every = el("button", "", "Everywhere");
      every.type = "button";
      var pick = el("button", "", "Pick keys on the picture");
      pick.type = "button";
      add(seg, every, pick);
      every.addEventListener("click", function () {
        host.stopPicking();
        host.edit(key, function (light) {
          var l = layersOf(light)[i];
          if (l) {
            showEverywhere(l);
          }
        });
      });
      pick.addEventListener("click", function () {
        if (host.picking() === key) {
          host.stopPicking();
        } else {
          stopRecording();
          host.startPicking(key);
        }
      });
      var capsRow = el("div", "row lkeys");
      var caps = el("div", "bcaps");
      var none = el("span", "muted", "No keys yet");
      add(capsRow, caps, none);
      var note = el("p", "note", "This effect lights only the keys picked on the picture.");
      note.hidden = !(spec && spec.needsKeys);
      var err = errLine();
      add(box, head, seg, capsRow, note, err);
      return {
        name: "keys",
        node: box,
        draw: function (layer) {
          var keys = Array.isArray(layer.keys) ? layer.keys : [];
          var picking = host.picking() === key;
          var picked = keys.length > 0 || picking;
          every.setAttribute("aria-pressed", picked ? "false" : "true");
          pick.setAttribute("aria-pressed", picked ? "true" : "false");
          every.disabled = !!(spec && spec.needsKeys);
          capsRow.hidden = !picked;
          if (caps.getAttribute("data-keys") !== keys.join(" ")) {
            caps.setAttribute("data-keys", keys.join(" "));
            K.caps(keys, caps);
          }
          none.hidden = keys.length > 0;
          showErr(err, errs(key));
        }
      };
    }

    function buildDetail(i, layer) {
      var spec = specOf(effects(), str(layer.effect));
      fields = [];
      if (spec) {
        settingsOf(spec).forEach(function (st) {
          fields.push(makeField(i, st));
        });
      }
      where = makeWhere(i, spec);
      fields.unshift(where);
      detailBody.replaceChildren.apply(detailBody, fields.map(function (fl) {
        return fl.node;
      }));
    }

    // Settings in the layer the effect does not have get a row each; they
    // come and go with the layer's settings.
    function drawUnknown(i, layer, spec) {
      var known = {};
      settingsOf(spec).forEach(function (st) {
        known[st.name] = true;
      });
      var names = Object.keys(layer.settings || {}).filter(function (k) {
        return !known[k] && given(layer.settings, k);
      }).sort();
      var have = fields.filter(function (fl) {
        return fl.unknown;
      }).map(function (fl) {
        return fl.name;
      });
      if (JSON.stringify(have) !== JSON.stringify(names)) {
        fields = fields.filter(function (fl) {
          if (fl.unknown) {
            detailBody.removeChild(fl.node);
          }
          return !fl.unknown;
        });
        names.forEach(function (k) {
          var u = makeUnknown(i, k);
          u.unknown = true;
          fields.push(u);
          detailBody.appendChild(u.node);
        });
      }
      return known;
    }

    function draw() {
      var all = layers();
      if (!host.light()) {
        return;
      }
      if (selected < 0 || selected >= all.length) {
        selected = all.length - 1;
      }

      if (rows.length !== all.length) {
        rows = displayOrder(all.length).map(function (fileIndex, pos) {
          return makeRow(pos);
        });
        list.replaceChildren.apply(list, rows.map(function (r) {
          return r.node;
        }));
      }
      rows.forEach(function (r) {
        r.draw();
      });

      var effs = effects();
      var wanted = effs.map(function (e) {
        return e.name;
      }).join(" ");
      if (wanted !== shownEffects) {
        shownEffects = wanted;
        addSel.replaceChildren.apply(addSel, effs.map(function (e) {
          var o = el("option", "", effectName(str(e.name)));
          o.value = str(e.name);
          return o;
        }));
      }
      addRow.hidden = effs.length === 0;

      detail.hidden = selected < 0;
      if (selected < 0) {
        return;
      }
      var layer = all[selected];
      layer.settings = layer.settings || {};
      var spec = specOf(effs, str(layer.effect));
      var key = selected + ":" + str(layer.effect) + ":" + stackGen + ":" + (spec ? "known" : "unknown");
      if (key !== detailKey) {
        if (rec) {
          stopRecording();
        }
        detailKey = key;
        buildDetail(selected, layer);
      }
      setText(detailHead, effectName(str(layer.effect)));
      var helpText = spec ? str(spec.help) : (effs.length > 0 ? "Commander does not know this effect." : "");
      setText(detailHelp, helpText);
      detailHelp.hidden = !helpText;
      var known = drawUnknown(selected, layer, spec);
      fields.forEach(function (fl) {
        fl.draw(layer);
      });

      // Errors about the layer as a whole, or about a setting that has no
      // field here, show under its name.
      var n = selected + 1;
      var lines = errs(layerKey(n)).slice();
      var errors = host.errors() || {};
      Object.keys(errors).forEach(function (k) {
        var p = parseKey(k);
        if (p && p.layer === n && p.setting && p.setting !== "keys" && !known[p.setting]) {
          (errors[k] || []).forEach(function (m) {
            if (lines.indexOf(m) < 0) {
              lines.push(m);
            }
          });
        }
      });
      showErr(detailErr, lines);
    }

    // reset forgets the selection: a new draft starts at the top layer.
    function reset() {
      stopRecording();
      selected = -1;
      stackGen += 1;
    }

    // showFirstError selects the top layer an error names.
    function showFirstError() {
      var bad = layersWithErrors(host.errors());
      if (bad.length > 0 && bad[bad.length - 1] <= layers().length) {
        if (selected !== bad[bad.length - 1] - 1) {
          stopRecording();
        }
        selected = bad[bad.length - 1] - 1;
      }
    }

    // pickedSet is the LEDs the layer a picker key names lights, or null.
    function pickedSet(key) {
      var p = parseKey(key);
      var l = p ? layers()[p.layer - 1] : null;
      return l ? ledSet(l.leds) : null;
    }

    // togglePick adds the LED i to the keys of the layer key names, or takes
    // it out.
    function togglePick(key, i) {
      var p = parseKey(key);
      if (!p) {
        return;
      }
      var names = host.names();
      host.edit(key, function (light) {
        var l = layersOf(light)[p.layer - 1];
        if (l) {
          pickLed(l, i, names);
        }
      });
    }

    return {
      node: node,
      draw: draw,
      reset: reset,
      showFirstError: showFirstError,
      pickedSet: pickedSet,
      togglePick: togglePick,
      stopRecording: stopRecording,
      isKey: function (key) {
        return !!parseKey(key);
      }
    };
  }

  window.CommanderLightAdvanced = { create: create, logic: logic };
})();
