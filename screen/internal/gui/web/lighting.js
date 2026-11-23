/* SPDX-License-Identifier: GPL-2.0-or-later */
"use strict";

// The Lighting tab of the KB7 page: a live preview drawn by Commander's
// lighting engine, which doubles as the key picker, the simple view of a
// profile's lighting, what the keyboard shows when Commander is not running,
// and the draft the player edits until Save. Built with createElement and
// textContent only.

(function () {
  var K = window.CommanderKeys; // key caps and the recorder (keys.js)
  var A = window.CommanderLightAdvanced; // the advanced view (lightadv.js)
  var NEW_COLOR = "#00AAFF";
  var WAVE_COLORS = ["#00AAFF", "#FF9A3D"];
  var RIPPLE_COLOR = "#FFFFFF";
  var HIGHLIGHT_COLOR = "#FF9A3D";
  var STATE_COLOR = "#FF4D6D";
  var SPEED_MIN = 0.05; // wave cycles per second, the slider's range
  var SPEED_MAX = 5;
  var PERIOD_SLOW = 10; // seconds per breath at the slow end of the slider
  var PERIOD_FAST = 1;
  var PREVIEW_PAUSE = 300; // ms after the last edit before new frames are asked for
  var FRAME_MS = 50;

  var BASES = [
    ["static", "One colour"],
    ["wave", "Wave"],
    ["breathing", "Breathing"],
    ["off", "Off"]
  ];

  // Draft logic: plain functions, no page involved.
  //
  // A draft is {kind, simple, light, stored}. kind is "none" (no [light]:
  // the keyboard shows its own), "simple" (simple holds the simple view) or
  // "light" (the advanced view: light holds the stack as Commander sent it,
  // with the player's edits). The keyed parts of the simple view, the highlighted
  // keys and each key state, hold {keys, orig, leds}: the names as written,
  // the LEDs those names light, and the LEDs picked now.

  function copy(v) {
    return JSON.parse(JSON.stringify(v));
  }

  // canonical writes a value with its object keys in order.
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

  function str(v) {
    return typeof v === "string" ? v : "";
  }

  function num(v) {
    return typeof v === "number" && isFinite(v) ? v : 0;
  }

  function upper(c) {
    return str(c).trim().toUpperCase();
  }

  function colorList(v) {
    return Array.isArray(v) ? v.map(upper) : [];
  }

  // ledSet is a sorted list of distinct LED indices.
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
    return canonical(ledSet(a)) === canonical(ledSet(b));
  }

  // toggleLed adds the LED to the set or takes it out; a new set.
  function toggleLed(set, i) {
    var out = ledSet(set);
    var at = out.indexOf(i);
    if (at >= 0) {
      out.splice(at, 1);
    } else {
      out.push(i);
    }
    return ledSet(out);
  }

  // Colour lists: each returns a new list.

  function addColor(list, c) {
    return list.concat([upper(c)]);
  }

  // removeColor takes out the colour at i, unless the list would get
  // shorter than min.
  function removeColor(list, i, min) {
    if (list.length <= min || i < 0 || i >= list.length) {
      return list.slice();
    }
    return list.slice(0, i).concat(list.slice(i + 1));
  }

  function setColor(list, i, c) {
    var out = list.slice();
    if (i >= 0 && i < out.length) {
      out[i] = upper(c);
    }
    return out;
  }

  // Sliders: the wave's speed on a log scale from slow to fast, 0 to 100;
  // the breathing's speed from a 10 s breath to a 1 s breath, 0 to 90.

  function speedToSlider(speed) {
    var s = Math.min(SPEED_MAX, Math.max(SPEED_MIN, num(speed) || 0.5));
    return Math.round(100 * Math.log(s / SPEED_MIN) / Math.log(SPEED_MAX / SPEED_MIN));
  }

  function sliderToSpeed(p) {
    var s = SPEED_MIN * Math.pow(SPEED_MAX / SPEED_MIN, Math.min(100, Math.max(0, p)) / 100);
    return Math.round(s * 100) / 100;
  }

  function periodToSlider(period) {
    var p = Math.min(PERIOD_SLOW, Math.max(PERIOD_FAST, num(period) || 4));
    return Math.round((PERIOD_SLOW - p) * 10);
  }

  function sliderToPeriod(v) {
    var p = PERIOD_SLOW - Math.min(90, Math.max(0, v)) / 10;
    return Math.round(p * 10) / 10;
  }

  function keyed(keys, leds, color) {
    var set = ledSet(leds);
    return {
      keys: Array.isArray(keys) ? keys.filter(function (k) {
        return typeof k === "string";
      }) : [],
      orig: set, leds: set.slice(), color: upper(color)
    };
  }

  function layerLeds(layer) {
    return layer && Array.isArray(layer.leds) ? layer.leds : [];
  }

  // simpleFrom turns deviceLight's simple view into the draft's; layers is
  // the same stack, bottom first, for the LEDs the keys light.
  function simpleFrom(s, layers) {
    var out = {
      base: str(s.base), colors: colorList(s.colors), speed: num(s.speed), period: num(s.period),
      direction: str(s.direction), ripple: null, highlight: null, states: []
    };
    var at = out.base ? 1 : 0;
    if (s.ripple && typeof s.ripple === "object") {
      out.ripple = { colors: colorList(s.ripple.colors) };
      at += 1;
    }
    if (s.highlight && typeof s.highlight === "object") {
      out.highlight = keyed(s.highlight.keys, layerLeds(layers[at]), s.highlight.color);
      at += 1;
    }
    (Array.isArray(s.states) ? s.states : []).forEach(function (st) {
      var k = keyed(st.keys, layerLeds(layers[at]), st.color);
      k.name = str(st.name);
      k.toggle = str(st.toggle);
      out.states.push(k);
      at += 1;
    });
    return out;
  }

  function storedFrom(st) {
    st = st && typeof st === "object" ? st : {};
    return {
      mode: st.mode === "color" ? "color" : "still",
      color: upper(st.color) || NEW_COLOR,
      brightness: typeof st.brightness === "number" ? st.brightness : null
    };
  }

  // fromSaved is the draft for a deviceLight answer.
  function fromSaved(saved) {
    var d = { kind: "none", simple: null, light: null, stored: storedFrom(saved.stored) };
    if (!saved.light || typeof saved.light !== "object") {
      return d;
    }
    if (saved.simple && typeof saved.simple === "object") {
      d.kind = "simple";
      d.simple = simpleFrom(saved.simple, Array.isArray(saved.light.layers) ? saved.light.layers : []);
    } else {
      d.kind = "light";
      d.light = copy(saved.light);
    }
    return d;
  }

  // newSimple is what "Set lighting" starts with.
  function newSimple() {
    return {
      base: "static", colors: [NEW_COLOR], speed: 0, period: 0, direction: "",
      ripple: null, highlight: null, states: []
    };
  }

  // switchBase picks another base effect, keeping the colours that still
  // fit. defaults may hold the engine's defaults: {speed, direction, period}.
  function switchBase(s, base, defaults) {
    var d = defaults || {};
    var out = copy(s);
    var cols = s.colors.filter(Boolean);
    out.base = base;
    if (base === "static") {
      out.colors = [cols[0] || NEW_COLOR];
    } else if (base === "wave") {
      if (cols.length >= 2) {
        out.colors = cols;
      } else if (cols.length === 1) {
        out.colors = [cols[0], cols[0] === WAVE_COLORS[1] ? WAVE_COLORS[0] : WAVE_COLORS[1]];
      } else {
        out.colors = WAVE_COLORS.slice();
      }
      out.speed = s.speed > 0 ? s.speed : (d.speed || 0.5);
      out.direction = s.direction || d.direction || "right";
    } else if (base === "breathing") {
      out.colors = cols.length > 0 ? cols : [NEW_COLOR];
      out.period = s.period >= PERIOD_FAST && s.period <= PERIOD_SLOW ? s.period : (d.period || 4);
    }
    return out;
  }

  // newStateName is "State N" for the first N no state has yet.
  function newStateName(states) {
    var names = states.map(function (s) {
      return s.name.trim().toLowerCase();
    });
    for (var n = 1; ; n++) {
      if (names.indexOf("state " + n) < 0) {
        return "State " + n;
      }
    }
  }

  function newState(states) {
    var k = keyed([], [], STATE_COLOR);
    k.name = newStateName(states);
    k.toggle = "";
    return k;
  }

  // keysOf is what a keyed part is written with: its names as written while
  // the picked LEDs are the ones those light, else one name per LED. names
  // maps an LED index to its name.
  function keysOf(item, names) {
    if (sameSet(item.leds, item.orig)) {
      return item.keys.slice();
    }
    return item.leds.map(function (i) {
      return names[i] || String(i);
    });
  }

  // simplePayload is the simple view as saveDeviceLight and lightFrames take
  // it: only the values of the chosen base effect.
  function simplePayload(s, names) {
    var out = {
      base: s.base, colors: [], speed: 0, period: 0, direction: "",
      ripple: null, highlight: null, states: []
    };
    if (s.base === "static") {
      out.colors = s.colors.slice(0, 1).map(upper);
    } else if (s.base === "wave") {
      out.colors = s.colors.map(upper);
      out.speed = s.speed;
      out.direction = s.direction;
    } else if (s.base === "breathing") {
      out.colors = s.colors.map(upper);
      out.period = s.period;
    }
    if (s.ripple) {
      out.ripple = { colors: s.ripple.colors.map(upper) };
    }
    if (s.highlight) {
      out.highlight = { keys: keysOf(s.highlight, names), color: upper(s.highlight.color) };
    }
    out.states = s.states.map(function (st) {
      return { name: st.name.trim(), toggle: st.toggle, keys: keysOf(st, names), color: upper(st.color) };
    });
    return out;
  }

  // lightPayload is the lighting part of a call: exactly one of simple and
  // light, where a light of null is no lighting.
  function lightPayload(draft, names) {
    if (draft.kind === "simple" && draft.simple) {
      return { simple: simplePayload(draft.simple, names || {}) };
    }
    if (draft.kind === "light" && draft.light) {
      return { light: A.logic.payload(draft.light) };
    }
    return { light: null };
  }

  function storedPayload(st) {
    return {
      mode: st.mode === "color" ? "color" : "still",
      color: st.mode === "color" ? upper(st.color) : "",
      brightness: typeof st.brightness === "number" ? Math.round(st.brightness) : null
    };
  }

  function storedChanged(draft, base) {
    return canonical(storedPayload(draft.stored)) !== canonical(storedPayload(base.stored));
  }

  function differs(draft, base, names) {
    return canonical(lightPayload(draft, names)) !== canonical(lightPayload(base, names)) || storedChanged(draft, base);
  }

  // controlsOf names the simple view's control for each layer it writes,
  // bottom first as Commander writes them: the base effect, the ripple,
  // the highlighted keys, then each key state.
  function controlsOf(s) {
    var out = [];
    if (!s) {
      return out;
    }
    if (s.base) {
      out.push("base");
    }
    if (s.ripple) {
      out.push("ripple");
    }
    if (s.highlight) {
      out.push("highlight");
    }
    s.states.forEach(function (st, i) {
      out.push("state:" + i);
    });
    return out;
  }

  // errorsByControl files Commander's errors ({layer, setting, message},
  // layer 1 for the bottom one, 0 for the lighting as a whole) under the
  // control that writes the layer; "" holds the rest.
  function errorsByControl(list, s) {
    var controls = controlsOf(s);
    var out = {};
    (Array.isArray(list) ? list : []).forEach(function (e) {
      var layer = e && typeof e.layer === "number" ? e.layer : 0;
      var key = (layer >= 1 && controls[layer - 1]) || "";
      var text = str(e && e.message);
      if (!text) {
        return;
      }
      out[key] = out[key] || [];
      if (out[key].indexOf(text) < 0) {
        out[key].push(text);
      }
    });
    return out;
  }

  // pageChecks are the checks the page makes before it asks Commander to
  // save, as {control, message}.
  function pageChecks(draft, names) {
    var out = [];
    if (draft.kind !== "simple" || !draft.simple) {
      return out;
    }
    var s = draft.simple;
    if (s.base === "wave" && s.colors.length < 2) {
      out.push({ control: "base", message: "A wave needs two colours or more." });
    }
    if (s.highlight && keysOf(s.highlight, names || {}).length === 0) {
      out.push({ control: "highlight", message: "Pick at least one key on the picture." });
    }
    s.states.forEach(function (st, i) {
      if (!st.name.trim()) {
        out.push({ control: "state:" + i, message: "Give the key state a name." });
      }
      if (keysOf(st, names || {}).length === 0) {
        out.push({ control: "state:" + i, message: "Pick at least one key on the picture." });
      }
    });
    return out;
  }

  // saveNotes are the lines after a save that went through.
  function saveNotes(res) {
    var lines = ["Saved."];
    if (res.change === "apply") {
      lines.push("Putting the lighting on the KB7");
    } else if (res.change === "reload") {
      lines.push("Commander shows the new lighting now");
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

  // decodeFrame turns a base64 frame into its bytes: R, G, B per LED index.
  function decodeFrame(b64, atobFn) {
    var bin = atobFn(String(b64));
    var out = new Uint8Array(bin.length);
    for (var i = 0; i < bin.length; i++) {
      out[i] = bin.charCodeAt(i) & 255;
    }
    return out;
  }

  // boardGeometry scales the LED positions (key units, centres) to a
  // picture width in pixels.
  function boardGeometry(leds, width, pad) {
    if (!leds.length || !(width > 0)) {
      return null;
    }
    var minX = Infinity;
    var maxX = -Infinity;
    var minY = Infinity;
    var maxY = -Infinity;
    leds.forEach(function (l) {
      minX = Math.min(minX, l.x);
      maxX = Math.max(maxX, l.x);
      minY = Math.min(minY, l.y);
      maxY = Math.max(maxY, l.y);
    });
    var unit = (width - 2 * pad) / (maxX - minX + 1);
    return {
      unit: unit,
      ox: pad + unit / 2 - minX * unit,
      oy: pad + unit / 2 - minY * unit,
      height: Math.ceil((maxY - minY + 1) * unit + 2 * pad)
    };
  }

  // ledAt is the LED whose square holds the point (pixels), or -1.
  function ledAt(leds, g, px, py) {
    if (!g) {
      return -1;
    }
    var x = (px - g.ox) / g.unit;
    var y = (py - g.oy) / g.unit;
    var best = -1;
    var bestD = Infinity;
    leds.forEach(function (l) {
      var dx = Math.abs(l.x - x);
      var dy = Math.abs(l.y - y);
      if (dx <= 0.5 && dy <= 0.5 && dx + dy < bestD) {
        bestD = dx + dy;
        best = l.index;
      }
    });
    return best;
  }

  var logic = {
    canonical: canonical, ledSet: ledSet, sameSet: sameSet, toggleLed: toggleLed,
    addColor: addColor, removeColor: removeColor, setColor: setColor,
    speedToSlider: speedToSlider, sliderToSpeed: sliderToSpeed,
    periodToSlider: periodToSlider, sliderToPeriod: sliderToPeriod,
    fromSaved: fromSaved, newSimple: newSimple, switchBase: switchBase, newState: newState,
    keysOf: keysOf, simplePayload: simplePayload, lightPayload: lightPayload,
    storedPayload: storedPayload, storedChanged: storedChanged, differs: differs,
    controlsOf: controlsOf, errorsByControl: errorsByControl, pageChecks: pageChecks,
    saveNotes: saveNotes, decodeFrame: decodeFrame, boardGeometry: boardGeometry, ledAt: ledAt
  };

  // The editor

  function create(api) {
    var el = api.el;

    var ed = {
      profile: -1,
      saved: null, // the deviceLight answer the draft started from
      base: null, // the draft of what is saved
      draft: null,
      loadError: "",
      saving: null, // the Promise of a save that runs
      notes: [], // lines after a save
      barError: "",
      changed: false, // the lighting changed outside Commander since it was read
      errors: {}, // control -> [message], from the last save or the page's checks
      previewErrors: {}, // control -> [message], from the last lightFrames answer
      picking: "", // the control whose keys the picture picks, or ""
      fit: null, // {key, simple, light}: lightFrames' last answer, for the draft key
      toAdvanced: "", // the draft key waiting for its stack to open in Advanced
      reloading: false, // a save went through and its reload has not answered yet
      gen: 0, // counts profile loads, so an overtaken answer is ignored
      wasDirty: false
    };

    var info = null; // the lightingInfo answer
    var infoError = "";
    var infoAsked = false;
    var names = {}; // LED index -> the name that picks it alone
    var defaults = {}; // the engine's defaults for the simple view's controls
    var directions = ["right", "left", "up", "down"];

    // Nodes, made once and kept: a redraw only changes them.

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

    function showErr(p, lines) {
      var text = (lines || []).join("\n");
      setText(p, text);
      p.hidden = !text;
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

    function setColorValue(input, c) {
      setValue(input, str(c).toLowerCase() || "#000000");
    }

    function checkbox(text) {
      var wrap = el("label", "bcheck");
      var box = el("input");
      box.type = "checkbox";
      add(wrap, box, el("span", "", text));
      return { node: wrap, box: box };
    }

    var uid = 0;
    function nextId(prefix) {
      uid += 1;
      return prefix + uid;
    }

    var node = el("div", "ledit");

    // The bar with Save and Discard, as in the Buttons tab.
    var bar = el("div", "bbar");
    bar.hidden = true;
    var barText = el("p", "bbar-text");
    barText.setAttribute("aria-live", "polite");
    var barActs = el("div", "bbar-acts");
    var saveBtn = bigBtn("Save", "primary");
    saveBtn.addEventListener("click", function () {
      save();
    });
    var discardBtn = bigBtn("Discard");
    discardBtn.addEventListener("click", function () {
      if (!ed.saving) {
        discard();
      }
    });
    add(barActs, saveBtn, discardBtn);
    add(bar, barText, barActs);

    var problem = el("p", "note error");
    problem.hidden = true;

    var work = el("div", "lwork");
    work.hidden = true;

    // The preview, which is also the key picker.
    var board = el("div", "lboard");
    var canvas = el("canvas", "lcanvas");
    canvas.setAttribute("role", "img");
    canvas.setAttribute("aria-label", "Preview of the lighting on the KB7");
    board.appendChild(canvas);
    var pickNote = el("p", "note", "Click keys on the picture to add or remove them.");
    pickNote.hidden = true;
    var previewNote = errLine();
    var wholeErr = errLine(); // errors about the lighting as a whole

    // Not set
    var notSet = el("div", "lnotset");
    var setBtn = bigBtn("Set lighting", "primary");
    setBtn.addEventListener("click", function () {
      edit("", function (d) {
        d.kind = "simple";
        d.simple = newSimple();
        d.light = null;
      });
      draw();
    });
    add(notSet, el("p", "", "Commander does not control this profile's lighting; the keyboard shows its own."), setBtn);

    // The way to the advanced view.
    var viewRow = el("div", "row lview");
    var advancedBtn = smallBtn("Advanced");
    advancedBtn.addEventListener("click", function () {
      enterAdvanced();
    });
    viewRow.appendChild(advancedBtn);

    // The simple view.
    var simpleBox = el("div", "lsimple");

    var baseSec = el("div", "lsec");
    baseSec.appendChild(el("h3", "label-caps", "Base effect"));
    var cards = el("div", "lcards");
    cards.setAttribute("role", "group");
    cards.setAttribute("aria-label", "Base effect");
    var cardBtns = BASES.map(function (b) {
      var c = el("button", "lcard");
      c.type = "button";
      c.appendChild(el("span", "lcard-name", b[1]));
      c.addEventListener("click", function () {
        if (!simple() || simple().base === b[0]) {
          return;
        }
        edit("base", function (d) {
          d.simple = switchBase(d.simple, b[0], defaults);
        });
      });
      cards.appendChild(c);
      return c;
    });

    // The base effect's controls.
    var baseCtl = el("div", "lctl");
    var oneRow = el("div", "row");
    var oneLabel = el("span", "", "Colour");
    var oneColor = colorInput("Colour", function (v) {
      edit("base", function (d) {
        d.simple.colors = [v];
      });
    });
    add(oneRow, oneColor, oneLabel);

    var baseColors = makeColorList("Colour", "base", function (d) {
      return d.simple.colors;
    }, function (d, list) {
      d.simple.colors = list;
    });

    var speedId = nextId("l-speed-");
    var speedRow = el("div", "row lrange-row");
    var speedLabel = el("label", "", "Speed");
    speedLabel.htmlFor = speedId;
    var speedIn = el("input", "brange");
    speedIn.type = "range";
    speedIn.id = speedId;
    speedIn.min = "0";
    speedIn.step = "1";
    speedIn.addEventListener("input", function () {
      var v = parseFloat(speedIn.value);
      edit("base", function (d) {
        if (d.simple.base === "wave") {
          d.simple.speed = sliderToSpeed(v);
        } else if (d.simple.base === "breathing") {
          d.simple.period = sliderToPeriod(v);
        }
      });
    });
    add(speedRow, speedLabel, el("span", "muted", "Slow"), speedIn, el("span", "muted", "Fast"));

    var dirId = nextId("l-dir-");
    var dirRow = el("div", "row");
    var dirLabel = el("label", "", "Direction");
    dirLabel.htmlFor = dirId;
    var dirIn = el("select", "binput");
    dirIn.id = dirId;
    dirIn.addEventListener("change", function () {
      var v = dirIn.value;
      edit("base", function (d) {
        d.simple.direction = v;
      });
    });
    add(dirRow, dirLabel, dirIn);
    var shownDirections = "";

    var offNote = el("p", "note", "Every key stays dark.");
    var baseErr = errLine();
    add(baseCtl, oneRow, baseColors.node, speedRow, dirRow, offNote, baseErr);

    var removeBtn = bigBtn("Remove lighting", "danger");
    removeBtn.addEventListener("click", function () {
      stopPicking();
      stopRecording();
      edit("", function (d) {
        d.kind = "none";
        d.simple = null;
        d.light = null;
      });
    });
    var removeRow = el("div", "row lremove");
    removeRow.appendChild(removeBtn);
    add(baseSec, cards, baseCtl, removeRow);

    // Extras
    var extraSec = el("div", "lsec");
    extraSec.appendChild(el("h3", "label-caps", "Extras"));

    // Ripple when a key is pressed
    var rippleBox = el("div", "lextra");
    var rippleTick = checkbox("Ripple when a key is pressed");
    rippleTick.box.addEventListener("change", function () {
      var on = !!rippleTick.box.checked;
      edit("ripple", function (d) {
        d.simple.ripple = on ? { colors: [RIPPLE_COLOR] } : null;
      });
    });
    var rippleCtl = el("div", "lctl");
    var rippleColors = makeColorList("Ripple colour", "ripple", function (d) {
      return d.simple.ripple.colors;
    }, function (d, list) {
      d.simple.ripple.colors = list;
    });
    var rippleErr = errLine();
    add(rippleCtl, rippleColors.node, rippleErr);
    add(rippleBox, rippleTick.node, rippleCtl);

    // Highlight keys
    var hlBox = el("div", "lextra");
    var hlTick = checkbox("Highlight keys");
    hlTick.box.addEventListener("change", function () {
      var on = !!hlTick.box.checked;
      if (!on && ed.picking === "highlight") {
        stopPicking();
      }
      edit("highlight", function (d) {
        d.simple.highlight = on ? keyed([], [], HIGHLIGHT_COLOR) : null;
      });
      if (on) {
        startPicking("highlight");
      }
    });
    var hlCtl = el("div", "lctl");
    var hl = makeKeysRow("highlight");
    var hlColorRow = el("div", "row");
    var hlColor = colorInput("Colour of the highlighted keys", function (v) {
      edit("highlight", function (d) {
        d.simple.highlight.color = v;
      });
    });
    add(hlColorRow, hlColor, el("span", "", "Colour"));
    var hlErr = errLine();
    add(hlCtl, hl.node, hlColorRow, hlErr);
    add(hlBox, hlTick.node, hlCtl);

    // Key states
    var stBox = el("div", "lextra");
    var stTick = checkbox("Key states");
    stTick.box.addEventListener("change", function () {
      var on = !!stTick.box.checked;
      stopRecording();
      if (!on && ed.picking.indexOf("state:") === 0) {
        stopPicking();
      }
      edit("", function (d) {
        d.simple.states = on ? [newState([])] : [];
      });
    });
    var stCtl = el("div", "lctl");
    var stList = el("ol", "lstates");
    var stRows = [];
    var addStateBtn = smallBtn("Add key state");
    addStateBtn.addEventListener("click", function () {
      edit("", function (d) {
        d.simple.states.push(newState(d.simple.states));
      });
      var last = stRows[stRows.length - 1];
      if (last) {
        last.name.focus();
      }
    });
    var addStateRow = el("div", "row");
    addStateRow.appendChild(addStateBtn);
    add(stCtl, stList, addStateRow);
    add(stBox, stTick.node, stCtl);

    add(extraSec, rippleBox, hlBox, stBox);
    add(simpleBox, baseSec, extraSec);

    // The advanced view (lightadv.js), with why the lighting opened there
    // and the way back to the simple view.
    var advBox = el("div", "ladv");
    var advReason = el("p", "lreason");
    var advBody = el("div", "ladv-body");
    var adv = A.create({
      el: el,
      keys: K,
      info: function () {
        return info;
      },
      light: function () {
        return ed.draft && ed.draft.kind === "light" ? ed.draft.light : null;
      },
      names: function () {
        return names;
      },
      edit: function (key, fn) {
        if (ed.draft && ed.draft.kind === "light") {
          edit(key, function (d) {
            fn(d.light);
          });
        }
      },
      restack: function (fn) {
        if (!ed.draft || ed.draft.kind !== "light") {
          return;
        }
        fn(ed.draft.light);
        // The errors named the layers by their place; those moved.
        ed.errors = {};
        ed.previewErrors = {};
        changed();
      },
      clear: function () {
        edit("", function (d) {
          d.kind = "none";
          d.simple = null;
          d.light = null;
        });
      },
      errors: function () {
        var out = {};
        Object.keys(ed.errors).concat(Object.keys(ed.previewErrors)).forEach(function (k) {
          out[k] = errorsFor(k);
        });
        return out;
      },
      picking: function () {
        return ed.picking;
      },
      startPicking: startPicking,
      stopPicking: stopPicking,
      redraw: function () {
        draw();
        paint();
      }
    });
    advBody.appendChild(adv.node);
    var simpleRow = el("div", "row");
    var simpleBtn = smallBtn("Simple view");
    simpleBtn.addEventListener("click", function () {
      backToSimple();
    });
    var noSimple = el("p", "note", "This lighting does not fit the simple view.");
    add(simpleRow, simpleBtn, noSimple);
    add(advBox, advReason, advBody, simpleRow);

    // When Commander is not running
    var storedSec = el("div", "lsec");
    storedSec.appendChild(el("h3", "label-caps", "When Commander is not running"));
    var storedSeg = el("div", "seg");
    storedSeg.setAttribute("role", "group");
    storedSeg.setAttribute("aria-label", "When Commander is not running");
    var stillBtn = el("button", "", "A still of this lighting");
    stillBtn.type = "button";
    stillBtn.addEventListener("click", function () {
      edit("stored", function (d) {
        d.stored.mode = "still";
      });
    });
    var oneBtn = el("button", "", "One colour");
    oneBtn.type = "button";
    oneBtn.addEventListener("click", function () {
      edit("stored", function (d) {
        d.stored.mode = "color";
      });
    });
    add(storedSeg, stillBtn, oneBtn);
    var storedColorRow = el("div", "row");
    var storedColor = colorInput("Colour when Commander is not running", function (v) {
      edit("stored", function (d) {
        d.stored.color = v;
      });
    });
    add(storedColorRow, storedColor, el("span", "", "Colour"));
    var brightHead = el("p", "label-caps lsub", "Brightness");
    var keepBright = checkbox("Keep the keyboard's brightness");
    keepBright.box.addEventListener("change", function () {
      var keep = !!keepBright.box.checked;
      edit("stored", function (d) {
        d.stored.brightness = keep ? null : 100;
      });
    });
    var brightRow = el("div", "row lrange-row");
    var brightIn = el("input", "brange");
    brightIn.type = "range";
    brightIn.min = "0";
    brightIn.max = "100";
    brightIn.step = "1";
    brightIn.setAttribute("aria-label", "Brightness");
    brightIn.addEventListener("input", function () {
      var v = Math.round(parseFloat(brightIn.value));
      edit("stored", function (d) {
        d.stored.brightness = Math.min(100, Math.max(0, v));
      });
    });
    var brightVal = el("span", "lrange-val");
    add(brightRow, brightIn, brightVal);
    var storedErr = errLine();
    add(storedSec, storedSeg, storedColorRow, brightHead, keepBright.node, brightRow, storedErr);

    add(work, board, pickNote, previewNote, wholeErr, notSet, viewRow, simpleBox, advBox, storedSec);
    add(node, bar, problem, work);

    // A list of colour pickers with add and remove. get and set reach the
    // list in a draft; control names it for its errors.
    function makeColorList(label, control, get, set) {
      var box = el("div", "lcolors");
      var list = el("div", "lcolor-list");
      var addBtn = smallBtn("Add colour");
      var rows = [];
      var min = 1;
      addBtn.addEventListener("click", function () {
        edit(control, function (d) {
          var cur = get(d);
          set(d, addColor(cur, cur.length > 0 ? cur[cur.length - 1] : NEW_COLOR));
        });
      });
      add(box, list, addBtn);

      function makeRow(i) {
        var row = el("div", "lcolor-row");
        var input = colorInput(label + " " + (i + 1), function (v) {
          edit(control, function (d) {
            set(d, setColor(get(d), i, v));
          });
        });
        var remove = smallBtn("Remove");
        remove.setAttribute("aria-label", "Remove " + label.toLowerCase() + " " + (i + 1));
        remove.addEventListener("click", function () {
          edit(control, function (d) {
            set(d, removeColor(get(d), i, min));
          });
        });
        add(row, input, remove);
        return { node: row, input: input, remove: remove };
      }

      return {
        node: box,
        draw: function (colors, atLeast) {
          min = atLeast;
          if (rows.length !== colors.length) {
            rows = colors.map(function (c, i) {
              return makeRow(i);
            });
            list.replaceChildren.apply(list, rows.map(function (r) {
              return r.node;
            }));
          }
          rows.forEach(function (r, i) {
            setColorValue(r.input, colors[i]);
            r.remove.disabled = colors.length <= min;
          });
        }
      };
    }

    // The keys a control lights: "Pick keys on the picture" and the keys
    // picked, as key caps.
    function makeKeysRow(control) {
      var row = el("div", "row lkeys");
      var pickBtn = smallBtn("Pick keys on the picture");
      pickBtn.addEventListener("click", function () {
        if (ed.picking === control) {
          stopPicking();
        } else {
          startPicking(control);
        }
      });
      var capsBox = el("div", "bcaps");
      var none = el("span", "muted", "No keys yet");
      add(row, pickBtn, capsBox, none);
      return {
        node: row,
        pick: pickBtn,
        draw: function (item) {
          var on = ed.picking === control;
          pickBtn.setAttribute("aria-pressed", on ? "true" : "false");
          var shown = item ? keysOf(item, names) : [];
          if (capsBox.getAttribute("data-keys") !== shown.join(" ")) {
            capsBox.setAttribute("data-keys", shown.join(" "));
            K.caps(shown, capsBox);
          }
          none.hidden = shown.length > 0;
        }
      };
    }

    // A key state's row: name, the key that flips it, the keys it lights,
    // its colour, and Remove.
    function makeStateRow(i) {
      var control = "state:" + i;
      var li = el("li", "lstate");
      var nameId = nextId("l-state-");
      var nameRow = el("div", "row");
      var nameLabel = el("label", "", "Name");
      nameLabel.htmlFor = nameId;
      var name = el("input", "binput");
      name.type = "text";
      name.id = nameId;
      name.autocomplete = "off";
      name.addEventListener("input", function () {
        var v = name.value;
        edit(control, function (d) {
          d.simple.states[i].name = v;
        });
      });
      var remove = smallBtn("Remove");
      remove.setAttribute("aria-label", "Remove this key state");
      remove.addEventListener("click", function () {
        stopRecording();
        if (ed.picking.indexOf("state:") === 0) {
          stopPicking();
        }
        edit("", function (d) {
          d.simple.states.splice(i, 1);
        });
        // The errors named the states by their place; those moved.
        ed.errors = {};
        draw();
      });
      add(nameRow, nameLabel, name, remove);

      var toggleRow = el("div", "row");
      toggleRow.appendChild(el("span", "", "Key"));
      var toggleCaps = el("div", "bcaps");
      var noKey = el("span", "muted", "None: only a screen button flips it");
      var recordBtn = smallBtn("Record key");
      recordBtn.addEventListener("click", function () {
        if (rec && rec.target === i) {
          stopRecording();
        } else {
          startRecording(i);
        }
      });
      var clearBtn = smallBtn("No key");
      clearBtn.addEventListener("click", function () {
        stopRecording();
        edit(control, function (d) {
          d.simple.states[i].toggle = "";
        });
      });
      add(toggleRow, toggleCaps, noKey, recordBtn, clearBtn);
      var recText = el("p", "note", "Press the key or keys that flip this state.");
      recText.hidden = true;
      recText.setAttribute("aria-live", "polite");

      var keys = makeKeysRow(control);
      var colorRow = el("div", "row");
      var color = colorInput("Colour while the state is on", function (v) {
        edit(control, function (d) {
          d.simple.states[i].color = v;
        });
      });
      add(colorRow, color, el("span", "", "Colour"));
      var err = errLine();
      add(li, nameRow, toggleRow, recText, keys.node, colorRow, err);

      return {
        node: li, name: name,
        draw: function (st, errs) {
          setValue(name, st.name);
          var recording = !!(rec && rec.target === i);
          var held = recording ? toggleRec.chord() : [];
          var shown = recording ? held : (st.toggle ? st.toggle.split("+") : []);
          if (toggleCaps.getAttribute("data-keys") !== shown.join(" ")) {
            toggleCaps.setAttribute("data-keys", shown.join(" "));
            K.caps(shown, toggleCaps);
          }
          noKey.hidden = recording || !!st.toggle;
          recText.hidden = !recording;
          setText(recordBtn, recording ? "Cancel" : "Record key");
          recordBtn.setAttribute("aria-pressed", recording ? "true" : "false");
          clearBtn.hidden = recording || !st.toggle;
          keys.draw(st);
          setColorValue(color, st.color);
          showErr(err, errs);
        }
      };
    }

    // Recording a state's key: one chord, as a button's keys are recorded.
    var rec = null; // {target: the state's index}
    var toggleRec = K.recorder({
      oneChord: true,
      onDone: function (steps) {
        var target = rec ? rec.target : -1;
        var keys = steps[0] && Array.isArray(steps[0].keys) ? steps[0].keys : [];
        if (target < 0 || keys.length === 0 || !simple() || !simple().states[target]) {
          return;
        }
        edit("state:" + target, function (d) {
          d.simple.states[target].toggle = keys.join("+");
        });
      },
      onStop: function () {
        rec = null;
        draw();
      },
      onChange: function () {
        draw();
      }
    });

    function startRecording(i) {
      if (K.stopAll) {
        K.stopAll(); // a recording in another tab ends first
      }
      stopPicking();
      rec = { target: i };
      toggleRec.start();
      draw();
    }

    function stopRecording() {
      toggleRec.stop(false);
      adv.stopRecording();
    }

    // Picking keys on the picture.

    function startPicking(control) {
      ed.picking = control;
      draw();
      paint();
    }

    function stopPicking() {
      if (ed.picking) {
        ed.picking = "";
        draw();
        paint();
      }
    }

    function simple() {
      return ed.draft && ed.draft.kind === "simple" ? ed.draft.simple : null;
    }

    // itemOf is the keyed part of the simple view a control edits.
    function itemOf(s, control) {
      if (!s) {
        return null;
      }
      if (control === "highlight") {
        return s.highlight;
      }
      if (control.indexOf("state:") === 0) {
        return s.states[Number(control.slice(6))] || null;
      }
      return null;
    }

    canvas.addEventListener("click", function (e) {
      if (!ed.picking || !geom) {
        return;
      }
      var r = canvas.getBoundingClientRect();
      var i = ledAt(ledList(), geom, e.clientX - r.left, e.clientY - r.top);
      if (i < 0) {
        return;
      }
      var control = ed.picking;
      if (adv.isKey(control)) {
        adv.togglePick(control, i);
        return;
      }
      edit(control, function (d) {
        var item = itemOf(d.simple, control);
        if (item) {
          item.leds = toggleLed(item.leds, i);
        }
      });
    });

    // pickedLeds is the set of LEDs the picture picks for now.
    function pickedLeds() {
      if (!ed.picking) {
        return [];
      }
      if (adv.isKey(ed.picking)) {
        return adv.pickedSet(ed.picking) || [];
      }
      var item = itemOf(simple(), ed.picking);
      return item ? item.leds : [];
    }

    // The preview: frames from Commander's lighting engine, looped.

    var frames = []; // Uint8Array per frame
    var frameAt = 0;
    var frameMs = FRAME_MS;
    var animTimer = null;
    var previewTimer = null;
    var frameGen = 0;
    var geom = null;
    var drawnWidth = 0;
    // playingKey is the canonical payload whose frames play ("" for none);
    // framesWanted says frames were asked for while the tab was not shown.
    var playingKey = "";
    var framesWanted = false;

    // themeColor reads a colour of the current theme from the page's CSS,
    // so the picture follows the theme; fallback when there is none.
    function themeColor(name, fallback) {
      var cs = window.getComputedStyle && document.documentElement ?
        window.getComputedStyle(document.documentElement) : null;
      var v = cs && cs.getPropertyValue ? String(cs.getPropertyValue(name) || "").trim() : "";
      return v || fallback;
    }

    function ledList() {
      return info && Array.isArray(info.leds) ? info.leds : [];
    }

    function fitCanvas() {
      var width = canvas.clientWidth || 0;
      if (!(width > 0)) {
        return false;
      }
      if (width !== drawnWidth || !geom) {
        drawnWidth = width;
        geom = boardGeometry(ledList(), width, 10);
        if (!geom) {
          return false;
        }
        var dpr = window.devicePixelRatio || 1;
        canvas.width = Math.round(width * dpr);
        canvas.height = Math.round(geom.height * dpr);
        canvas.style.height = geom.height + "px";
      }
      return true;
    }

    function paint() {
      var ctx = canvas.getContext ? canvas.getContext("2d") : null;
      if (!ctx || !info || !fitCanvas()) {
        return;
      }
      var dpr = window.devicePixelRatio || 1;
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, drawnWidth, geom.height);
      var frame = frames.length > 0 ? frames[frameAt % frames.length] : null;
      var pickedSet = pickedLeds();
      var pickedColor = themeColor("--accent", "#00aaff");
      var edgeColor = themeColor("--line", "#222c47");
      var size = geom.unit * 0.82;
      var radius = geom.unit * 0.16;
      ledList().forEach(function (l) {
        var x = geom.ox + l.x * geom.unit - size / 2;
        var y = geom.oy + l.y * geom.unit - size / 2;
        var r = 0;
        var g = 0;
        var b = 0;
        if (frame && frame.length >= l.index * 3 + 3) {
          r = frame[l.index * 3];
          g = frame[l.index * 3 + 1];
          b = frame[l.index * 3 + 2];
        }
        ctx.beginPath();
        if (ctx.roundRect) {
          ctx.roundRect(x, y, size, size, radius);
        } else {
          ctx.rect(x, y, size, size);
        }
        ctx.fillStyle = "rgb(" + r + "," + g + "," + b + ")";
        ctx.fill();
        var on = pickedSet.indexOf(l.index) >= 0;
        ctx.lineWidth = on ? 3 : 1;
        ctx.strokeStyle = on ? pickedColor : edgeColor;
        ctx.stroke();
      });
    }

    function animate() {
      animTimer = null;
      if (!canvas.isConnected) {
        return; // the tab is not shown; show() starts it again
      }
      paint();
      if (frames.length > 1) {
        frameAt = (frameAt + 1) % frames.length;
        animTimer = setTimeout(animate, frameMs);
      }
    }

    function startAnimation() {
      if (!animTimer) {
        animate();
      }
    }

    // askFrames asks for the frames of the draft as it is now; until the
    // answer comes, and when it names errors, the old frames keep playing.
    function askFrames() {
      clearTimeout(previewTimer);
      previewTimer = null;
      if (!ed.draft || !info) {
        return;
      }
      if (!canvas.isConnected) {
        // The tab is not shown: show() asks once it is.
        framesWanted = true;
        return;
      }
      framesWanted = false;
      frameGen += 1;
      var gen = frameGen;
      var profileGen = ed.gen;
      var forSimple = simple();
      var forLight = ed.draft.kind === "light" && ed.draft.light ? ed.draft.light.layers.length : -1;
      var sent = lightPayload(ed.draft, names);
      var sentKey = canonical(sent);
      api.call("lightFrames", sent).then(function (res) {
        if (gen !== frameGen || profileGen !== ed.gen) {
          return;
        }
        res = res && typeof res === "object" ? res : {};
        setText(previewNote, "");
        previewNote.hidden = true;
        if (Array.isArray(res.errors) && res.errors.length > 0) {
          ed.fit = null;
          ed.toAdvanced = ""; // the errors show in the simple view
          ed.previewErrors = forLight >= 0 ? A.logic.errorsByLayer(res.errors, forLight) : errorsByControl(res.errors, forSimple);
          draw();
          return;
        }
        ed.fit = {
          key: sentKey,
          simple: res.simple && typeof res.simple === "object" ? res.simple : null,
          light: res.light && typeof res.light === "object" ? res.light : null
        };
        if (ed.toAdvanced && ed.toAdvanced === sentKey) {
          ed.toAdvanced = "";
          openAdvanced(res.light && typeof res.light === "object" ? copy(res.light) : null);
        }
        ed.previewErrors = {};
        if (Array.isArray(res.frames)) {
          try {
            frames = res.frames.map(function (f) {
              return decodeFrame(f, window.atob);
            });
          } catch (err) {
            frames = [];
          }
          // The same lighting keeps playing where it is.
          if (sentKey !== playingKey) {
            frameAt = 0;
            clearTimeout(animTimer);
            animTimer = null;
          }
          playingKey = frames.length > 0 ? sentKey : "";
        }
        draw();
        startAnimation();
      }, function (err) {
        if (gen !== frameGen || profileGen !== ed.gen) {
          return;
        }
        ed.toAdvanced = "";
        showErr(previewNote, [api.plainError("Cannot show the preview", err)]);
        draw();
      });
    }

    function previewSoon() {
      clearTimeout(previewTimer);
      previewTimer = setTimeout(askFrames, PREVIEW_PAUSE);
    }

    function loadInfo() {
      if (info || infoAsked) {
        return;
      }
      infoAsked = true;
      api.call("lightingInfo").then(function (res) {
        if (!res || typeof res !== "object" || !Array.isArray(res.leds)) {
          infoAsked = false;
          return;
        }
        info = res;
        infoError = "";
        names = {};
        res.leds.forEach(function (l) {
          names[l.index] = str(l.name);
        });
        if (typeof res.frameMs === "number" && res.frameMs > 0) {
          frameMs = res.frameMs;
        }
        (Array.isArray(res.effects) ? res.effects : []).forEach(function (e) {
          (Array.isArray(e.settings) ? e.settings : []).forEach(function (st) {
            if (e.name === "wave" && st.name === "speed" && typeof st.default === "number") {
              defaults.speed = st.default;
            } else if (e.name === "wave" && st.name === "direction") {
              defaults.direction = str(st.default);
              if (Array.isArray(st.choices) && st.choices.length > 0) {
                directions = st.choices.slice();
              }
            } else if (e.name === "breathing" && st.name === "period") {
              var p = parseFloat(str(st.default));
              if (p >= PERIOD_FAST && p <= PERIOD_SLOW) {
                defaults.period = p;
              }
            }
          });
        });
        geom = null;
        draw();
        askFrames();
      }, function (err) {
        infoAsked = false;
        infoError = api.plainError("Cannot show the preview", err);
        draw();
      });
    }

    // Drawing

    function errorsFor(control) {
      var out = (ed.errors[control] || []).slice();
      (ed.previewErrors[control] || []).forEach(function (m) {
        if (out.indexOf(m) < 0) {
          out.push(m);
        }
      });
      return out;
    }

    function drawBar() {
      var dirty = isDirty();
      var lines = [];
      var cls = "bbar-text";
      if (ed.changed) {
        lines.push("This profile's lighting was changed outside Commander since you opened it. Save again to replace that change, or Discard to load it.");
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
      setText(barText, lines.join("\n"));
      barText.className = cls;
      var acts = dirty || ed.changed;
      barActs.hidden = !acts;
      bar.hidden = lines.length === 0 && !acts;
      bar.classList.toggle("dirty", acts);
      setText(saveBtn, ed.saving ? "Saving" : "Save");
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

    function drawSimple(s) {
      cardBtns.forEach(function (c, i) {
        var on = s.base === BASES[i][0];
        c.setAttribute("aria-pressed", on ? "true" : "false");
        c.classList.toggle("on", on);
      });
      baseCtl.hidden = !s.base;
      oneRow.hidden = s.base !== "static";
      if (s.base === "static") {
        setColorValue(oneColor, s.colors[0]);
      }
      var list = s.base === "wave" || s.base === "breathing";
      baseColors.node.hidden = !list;
      if (list) {
        baseColors.draw(s.colors, s.base === "wave" ? 2 : 1);
      }
      speedRow.hidden = !list;
      if (s.base === "wave") {
        speedIn.max = "100";
        setValue(speedIn, String(speedToSlider(s.speed)));
        speedIn.setAttribute("aria-valuetext", s.speed + " waves per second");
      } else if (s.base === "breathing") {
        speedIn.max = "90";
        setValue(speedIn, String(periodToSlider(s.period)));
        speedIn.setAttribute("aria-valuetext", "One breath every " + s.period + " seconds");
      }
      dirRow.hidden = s.base !== "wave";
      if (s.base === "wave") {
        var wanted = directions.join(" ");
        if (shownDirections !== wanted) {
          shownDirections = wanted;
          dirIn.replaceChildren.apply(dirIn, directions.map(function (d) {
            var o = el("option", "", d.charAt(0).toUpperCase() + d.slice(1));
            o.value = d;
            return o;
          }));
        }
        setValue(dirIn, s.direction);
      }
      offNote.hidden = s.base !== "off";
      showErr(baseErr, errorsFor("base"));

      rippleTick.box.checked = !!s.ripple;
      rippleCtl.hidden = !s.ripple;
      if (s.ripple) {
        rippleColors.draw(s.ripple.colors, 1);
      }
      showErr(rippleErr, errorsFor("ripple"));

      hlTick.box.checked = !!s.highlight;
      hlCtl.hidden = !s.highlight;
      if (s.highlight) {
        hl.draw(s.highlight);
        setColorValue(hlColor, s.highlight.color);
      }
      showErr(hlErr, errorsFor("highlight"));

      stTick.box.checked = s.states.length > 0;
      stCtl.hidden = s.states.length === 0;
      if (stRows.length !== s.states.length) {
        stRows = s.states.map(function (st, i) {
          return makeStateRow(i);
        });
        stList.replaceChildren.apply(stList, stRows.map(function (r) {
          return r.node;
        }));
      }
      stRows.forEach(function (r, i) {
        r.draw(s.states[i], errorsFor("state:" + i));
      });
    }

    function drawStored(st) {
      stillBtn.setAttribute("aria-pressed", st.mode === "still" ? "true" : "false");
      oneBtn.setAttribute("aria-pressed", st.mode === "color" ? "true" : "false");
      storedColorRow.hidden = st.mode !== "color";
      setColorValue(storedColor, st.color);
      var keep = typeof st.brightness !== "number";
      keepBright.box.checked = keep;
      brightRow.hidden = keep;
      if (!keep) {
        setValue(brightIn, String(st.brightness));
        setText(brightVal, st.brightness + " %");
      }
      showErr(storedErr, errorsFor("stored"));
    }

    function draw() {
      var ready = !!(ed.saved && ed.draft);
      setText(problem, ed.loadError);
      problem.hidden = !ed.loadError;
      work.hidden = !ready;
      if (ready) {
        var d = ed.draft;
        var none = d.kind === "none";
        notSet.hidden = !none;
        var advanced = d.kind === "light";
        viewRow.hidden = none || advanced;
        advancedBtn.disabled = switching() || !!ed.toAdvanced;
        simpleBox.hidden = none || advanced;
        advBox.hidden = !advanced;
        if (d.kind === "simple") {
          drawSimple(d.simple);
        }
        if (advanced) {
          // Why the saved stack opened here; empty when it fits the simple
          // view.
          var reason = ed.saved.simple ? "" : str(ed.saved.reason);
          setText(advReason, reason);
          advReason.hidden = !reason;
          adv.draw();
          var fit = currentFit();
          simpleBtn.disabled = !fit || switching();
          noSimple.hidden = !!fit;
        }
        storedSec.hidden = none;
        drawStored(d.stored);
        pickNote.hidden = !ed.picking;
        canvas.classList.toggle("picking", !!ed.picking);
        if (infoError && !info) {
          showErr(previewNote, [infoError]);
        }
        showErr(wholeErr, errorsFor(""));
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

    // edit changes the draft: fn gets it and changes it in place. control
    // names the control the change came from; its errors from the last save
    // go, as the page shows the new value.
    function edit(control, fn) {
      if (!ed.draft) {
        return;
      }
      fn(ed.draft);
      ed.toAdvanced = ""; // a stack asked for the draft before is no longer it
      delete ed.errors[control];
      // What the keyboard shows without Commander is not in the preview.
      changed(control !== "stored");
    }

    // changed is called after every change to the draft; with preview the
    // preview asks for new frames shortly.
    function changed(preview) {
      ed.notes = [];
      ed.barError = "";
      draw();
      paint();
      if (preview !== false) {
        previewSoon();
      }
    }

    // Between the views. The advanced view edits a stack; the simple view
    // its own draft. Going to Advanced turns the simple draft into its
    // stack: the saved one as Commander sent it when nothing was changed,
    // else the one the simple view writes. Going back takes the simple view
    // Commander found in the stack.

    // lightBase is a saved draft as the advanced view compares with it: a
    // simple one as its stack, as saved.
    function lightBase(b) {
      if (b.kind !== "simple" || !ed.saved || !ed.saved.light) {
        return b;
      }
      return { kind: "light", simple: null, light: copy(ed.saved.light), stored: b.stored };
    }

    // switching is whether the views cannot be switched now: while a save
    // and its reload run, the saved stack is not the one on disk yet.
    function switching() {
      return !!ed.saving || ed.reloading;
    }

    // enterAdvanced opens the simple draft in the advanced view: unchanged,
    // as the saved stack; else as the stack Commander writes for it, from
    // the last lightFrames answer for the draft or from one asked for now.
    function enterAdvanced() {
      var d = ed.draft;
      if (!d || d.kind !== "simple" || switching()) {
        return;
      }
      stopPicking();
      stopRecording();
      var unchanged = ed.base && ed.base.kind === "simple" && ed.saved && ed.saved.light &&
        canonical(lightPayload(d, names)) === canonical(lightPayload(ed.base, names));
      if (unchanged) {
        openAdvanced(copy(ed.saved.light));
        return;
      }
      var fit = fitNow();
      if (fit) {
        openAdvanced(fit.light ? copy(fit.light) : null);
        return;
      }
      if (!info) {
        return; // lightFrames needs lightingInfo first
      }
      ed.toAdvanced = canonical(lightPayload(d, names));
      draw();
      askFrames();
    }

    // openAdvanced makes stack the draft; null is no lighting.
    function openAdvanced(stack) {
      var d = ed.draft;
      if (!d || d.kind !== "simple" || switching()) {
        return;
      }
      ed.base = lightBase(ed.base);
      d.kind = stack ? "light" : "none";
      d.light = stack;
      d.simple = null;
      ed.errors = {};
      ed.previewErrors = {};
      adv.reset();
      changed();
    }

    // fitNow is lightFrames' last answer when it was for the draft as it is
    // now; else null.
    function fitNow() {
      if (!ed.fit || !ed.draft) {
        return null;
      }
      return ed.fit.key === canonical(lightPayload(ed.draft, names)) ? ed.fit : null;
    }

    // currentFit is the simple view of the draft as it is now, when the
    // last lightFrames answer was for it and has one; else null.
    function currentFit() {
      var fit = fitNow();
      return fit ? fit.simple : null;
    }

    function backToSimple() {
      var fit = currentFit();
      if (!fit || ed.draft.kind !== "light" || switching()) {
        return;
      }
      stopPicking();
      stopRecording();
      if (ed.base && ed.base.kind === "light" && ed.saved && ed.saved.simple) {
        var b = fromSaved(ed.saved);
        b.stored = ed.base.stored;
        ed.base = b;
      }
      var layers = Array.isArray(ed.draft.light.layers) ? ed.draft.light.layers : [];
      ed.errors = {};
      ed.previewErrors = {};
      edit("", function (d) {
        d.kind = "simple";
        d.simple = simpleFrom(fit, layers);
        d.light = null;
      });
    }

    // Loading and saving

    function isDirty() {
      return !!(ed.saved && ed.draft && ed.base) && differs(ed.draft, ed.base, names);
    }

    function validAnswer(res) {
      return res && typeof res === "object" && typeof res.version === "string" &&
        Object.prototype.hasOwnProperty.call(res, "light") && res.stored && typeof res.stored === "object";
    }

    // load fetches the profile's lighting. A draft that differs from what
    // was saved when the answer arrives stays; otherwise the draft becomes
    // the file.
    function load() {
      var gen = ed.gen;
      return api.call("deviceLight", { device: "kb7", profile: ed.profile }).then(function (res) {
        if (gen !== ed.gen || !validAnswer(res)) {
          return;
        }
        var wasDirty = isDirty();
        var fresh = !ed.saved || ed.saved.version !== res.version;
        // A player in the advanced view stays there, unless the lighting
        // changed outside Commander and the draft follows it.
        var stay = !!ed.draft && ed.draft.kind === "light" && (wasDirty || !fresh);
        ed.loadError = "";
        ed.saved = res;
        ed.base = fromSaved(res);
        if (stay) {
          ed.base = lightBase(ed.base);
        }
        if (!wasDirty) {
          ed.draft = copy(ed.base);
          if (fresh) {
            ed.picking = "";
            adv.reset();
          }
          // A refresh that finds the same lighting leaves the preview as
          // it plays.
          if (fresh || canonical(lightPayload(ed.draft, names)) !== playingKey) {
            askFrames();
          }
        }
        draw();
      }, function (err) {
        if (gen !== ed.gen) {
          return;
        }
        ed.loadError = api.plainError("Could not read this profile's lighting", err);
        draw();
      });
    }

    function show(profile) {
      // The panel is put in the page after this call: the preview starts
      // playing once it is there.
      Promise.resolve().then(function () {
        if (framesWanted && canvas.isConnected) {
          askFrames();
        }
        startAnimation();
      });
      if (!info) {
        loadInfo();
      }
      if (profile === ed.profile) {
        return;
      }
      stopRecording();
      ed.gen += 1;
      ed.profile = profile;
      ed.saved = null;
      ed.base = null;
      ed.draft = null;
      ed.loadError = "";
      ed.notes = [];
      ed.barError = "";
      ed.changed = false;
      ed.errors = {};
      ed.previewErrors = {};
      ed.picking = "";
      ed.fit = null;
      ed.toAdvanced = "";
      ed.reloading = false;
      adv.reset();
      frames = [];
      frameAt = 0;
      playingKey = "";
      framesWanted = false;
      draw();
      if (profile >= 0) {
        load();
      }
    }

    // refresh reads the lighting again when nothing is being edited, so a
    // change made outside Commander shows.
    function refresh() {
      if (!info) {
        loadInfo();
      }
      if (ed.profile < 0 || ed.saving || ed.changed || isDirty()) {
        return;
      }
      load();
    }

    function discard() {
      if (!ed.saved) {
        return;
      }
      stopRecording();
      ed.draft = copy(ed.base);
      ed.picking = "";
      adv.reset();
      ed.changed = false;
      ed.errors = {};
      ed.previewErrors = {};
      changed();
    }

    // showErrors keeps the errors of a save per control.
    function showErrors(byControl) {
      ed.errors = byControl;
      ed.notes = [];
      ed.barError = "Not saved: the lighting needs a fix.";
      if (ed.draft && ed.draft.kind === "light") {
        adv.showFirstError();
      }
      draw();
    }

    // save saves the whole draft. Its Promise says whether it went through.
    // The page's own checks come first; Commander is not asked when one
    // fails.
    function save() {
      if (ed.saving) {
        return ed.saving;
      }
      if (!ed.saved || !ed.draft) {
        return Promise.resolve(false);
      }
      stopRecording();
      var bad = pageChecks(ed.draft, names);
      if (bad.length > 0) {
        var by = {};
        bad.forEach(function (e) {
          (by[e.control] = by[e.control] || []).push(e.message);
        });
        showErrors(by);
        return Promise.resolve(false);
      }
      var gen = ed.gen;
      var sent = copy(ed.draft);
      var args = lightPayload(sent, names);
      args.device = "kb7";
      args.profile = ed.profile;
      args.version = ed.saved.version;
      if (storedChanged(sent, ed.base)) {
        args.stored = storedPayload(sent.stored);
        // Only the not-running choice changed: [light] stays as written.
        if (canonical(lightPayload(sent, names)) === canonical(lightPayload(ed.base, names))) {
          delete args.simple;
          delete args.light;
        }
      }
      ed.barError = "";
      ed.saving = api.call("saveDeviceLight", args).then(function (res) {
        ed.saving = null;
        if (gen !== ed.gen) {
          return false;
        }
        res = res && typeof res === "object" ? res : {};
        if (res.ok) {
          ed.errors = {};
          ed.changed = false;
          ed.saved.version = str(res.version);
          ed.base = copy(sent);
          ed.notes = saveNotes(res);
          // The lighting as written. Until it is read, the views stay as
          // they are: the saved stack is not the new one yet.
          ed.reloading = true;
          draw();
          load().then(function () {
            if (gen === ed.gen) {
              ed.reloading = false;
              draw();
            }
          });
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
          showErrors(sent.kind === "light" && sent.light ?
            A.logic.errorsByLayer(res.errors, sent.light.layers.length) :
            errorsByControl(res.errors, sent.kind === "simple" ? sent.simple : null));
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

    window.addEventListener("resize", function () {
      geom = null;
      paint();
    });

    // A theme change redraws the picture in the new theme's colours.
    if (typeof window.MutationObserver === "function" && document.documentElement) {
      new window.MutationObserver(function () {
        paint();
      }).observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
    }

    draw();

    return {
      node: node,
      show: show,
      refresh: refresh,
      dirty: isDirty,
      save: save,
      discard: discard,
      // stopRecording ends a key recording, for when the tab is left.
      stopRecording: stopRecording
    };
  }

  window.CommanderLighting = { create: create, logic: logic };
})();
