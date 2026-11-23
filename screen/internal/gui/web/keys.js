/* SPDX-License-Identifier: GPL-2.0-or-later */
"use strict";

// The keys of the KB7 page, shared by the tabs that record them: the key
// table (KeyboardEvent.code to Commander's key name), the key caps and the
// key recorder. Built with createElement and textContent only.

(function () {
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
  var RECORD_IDLE = 10000; // ms without a key that ends a recording

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


  // caps draws key names as key caps into the element box.
  function caps(names, box) {
    var kids = names.map(function (k) {
      var cap = keyCap(k);
      var n = document.createElement("kbd");
      n.className = "bcap";
      n.textContent = cap.text;
      if (cap.title) {
        n.title = cap.title;
      }
      return n;
    });
    box.replaceChildren.apply(box, kids);
  }

  // recorder listens to the keys on the document, before anything else
  // hears them, while it runs. The options:
  //   oneChord  stop after the first chord (the keys held together)
  //   onDone    gets the steps a recording caught: {keys} and {waitMs}
  //             steps, as soon as each is complete (all at once on stop
  //             when oneChord)
  //   onStop    runs when the recording has ended, after onDone
  //   onChange  optional: runs when what is held changed, for drawing it
  //   ignore    optional: a key event it returns true for is left alone
  // It ends by itself after RECORD_IDLE ms without a key, or when the
  // window loses focus. Beyond start, stop and running it offers chord
  // (the keys picked and held now), pick (add a key by name) and skipped
  // (the key codes Commander cannot press that the last recording met).
  // Only one recorder runs in the window: starting one stops the one that
  // ran, dropping what that one held.
  function recorder(options) {
    var oneChord = !!options.oneChord;
    var state = null; // {data, picked, timer}
    var skippedLast = [];
    var self;

    function arm() {
      if (state.timer) {
        clearTimeout(state.timer);
      }
      state.timer = setTimeout(function () {
        finish(true);
      }, RECORD_IDLE);
    }

    function onBlur() {
      finish(true);
    }

    function onKey(e) {
      if (!state) {
        return;
      }
      if (options.ignore && options.ignore(e)) {
        return;
      }
      e.preventDefault();
      e.stopPropagation();
      var t = typeof e.timeStamp === "number" ? e.timeStamp : Date.now();
      var out = e.type === "keydown" ? recordDown(state.data, e.code, t) : recordUp(state.data, e.code, t);
      skippedLast = state.data.skipped.slice();
      arm();
      if (oneChord) {
        var chord = out.filter(function (s) {
          return s.keys;
        });
        if (chord.length > 0) {
          finish(true, chord[0].keys);
          return;
        }
      } else if (out.length > 0) {
        options.onDone(out);
        return;
      }
      if (options.onChange) {
        options.onChange();
      }
    }

    // finish ends the recording. With keep, what it caught goes to onDone:
    // a chord still held, or, when oneChord, the picked and the held keys.
    function finish(keep, chord) {
      if (!state) {
        return;
      }
      var s = state;
      state = null;
      if (active === self) {
        active = null;
      }
      clearTimeout(s.timer);
      document.removeEventListener("keydown", onKey, true);
      document.removeEventListener("keyup", onKey, true);
      window.removeEventListener("blur", onBlur);
      var left = recordEnd(s.data);
      skippedLast = s.data.skipped.slice();
      var steps = [];
      if (keep && !oneChord) {
        steps = left;
      } else if (keep) {
        var keys = s.picked.slice();
        (chord || (left[0] ? left[0].keys : [])).forEach(function (k) {
          if (keys.indexOf(k) < 0) {
            keys.push(k);
          }
        });
        if (keys.length > 0) {
          steps = [{ keys: keys }];
        }
      }
      if (steps.length > 0) {
        options.onDone(steps);
      }
      options.onStop();
    }

    self = {
      start: function () {
        finish(true);
        if (active && active !== self) {
          active.stop(false);
        }
        active = self;
        state = { data: newRecording(), picked: [], timer: null };
        skippedLast = [];
        document.addEventListener("keydown", onKey, true);
        document.addEventListener("keyup", onKey, true);
        window.addEventListener("blur", onBlur);
        arm();
      },
      // stop ends the recording; stop(false) drops what it holds.
      stop: function (keep) {
        finish(keep !== false);
      },
      running: function () {
        return state !== null;
      },
      chord: function () {
        var names = state ? state.picked.slice() : [];
        if (state) {
          state.data.chord.forEach(function (k) {
            if (names.indexOf(k) < 0) {
              names.push(k);
            }
          });
        }
        return names;
      },
      // pick adds a key by its name; it tells whether it was new.
      pick: function (name) {
        if (!state || !name || state.picked.indexOf(name) >= 0) {
          return false;
        }
        state.picked.push(name);
        arm();
        return true;
      },
      skipped: function () {
        return skippedLast.slice();
      }
    };
    return self;
  }

  var active = null; // the recorder that runs, if any

  // stopAll ends the recording that runs anywhere in the window, dropping
  // what it held.
  function stopAll() {
    if (active) {
      active.stop(false);
    }
  }

  window.CommanderKeys = {
    codeToName: function (code) {
      return KEY_CODES[code];
    },
    capLabel: function (name) {
      return keyCap(name).text;
    },
    capTitle: function (name) {
      return keyCap(name).title;
    },
    caps: caps,
    recorder: recorder,
    stopAll: stopAll,
    KEY_CODES: KEY_CODES, KEY_LIST: KEY_LIST, WAIT_MAX: WAIT_MAX, keyCap: keyCap, keyWords: keyWords,
    newRecording: newRecording, recordDown: recordDown, recordUp: recordUp, recordEnd: recordEnd
  };
})();
