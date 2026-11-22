/* SPDX-License-Identifier: GPL-2.0-or-later */
"use strict";

(function () {
  var view = window.chrome && window.chrome.webview;
  var status = document.getElementById("status");
  if (!view) {
    status.textContent = "No window bridge";
    return;
  }
  view.addEventListener("message", function (e) {
    var m;
    try {
      m = JSON.parse(e.data);
    } catch (err) {
      return;
    }
    // The binding echoes our own calls back; only answers matter here.
    if (!m || typeof m !== "object" || "call" in m || m.id !== 1) {
      return;
    }
    if (typeof m.error === "string") {
      status.textContent = "Error: " + m.error;
    } else if (m.result && m.result.status !== undefined) {
      status.textContent = String(m.result.status);
    }
  });
  view.postMessage(JSON.stringify({ id: 1, call: "state" }));
})();
