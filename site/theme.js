(function () {
  var root = document.documentElement;
  var media = window.matchMedia('(prefers-color-scheme: light)');
  var key = 'commander-theme';
  var stored = null;
  var button = null;

  try {
    stored = localStorage.getItem(key);
  } catch (e) {
    stored = null;
  }
  if (stored !== 'light' && stored !== 'dark') {
    stored = null;
  }

  function system() {
    return media.matches ? 'light' : 'dark';
  }

  function apply() {
    var theme = stored || system();
    root.setAttribute('data-theme', theme);
    if (button) {
      var other = theme === 'light' ? 'dark' : 'light';
      button.setAttribute('aria-label', 'Switch to ' + other + ' theme');
      button.setAttribute('title', 'Switch to ' + other + ' theme');
    }
  }

  // A choice equal to the system setting is not kept, so the page follows
  // the system again from then on.
  function toggle() {
    var next = (stored || system()) === 'light' ? 'dark' : 'light';
    stored = next === system() ? null : next;
    try {
      if (stored) {
        localStorage.setItem(key, stored);
      } else {
        localStorage.removeItem(key);
      }
    } catch (e) {
      // Without storage the choice lasts for this page view only.
    }
    apply();
  }

  apply();

  media.addEventListener('change', apply);

  document.addEventListener('DOMContentLoaded', function () {
    button = document.getElementById('theme-toggle');
    if (!button) {
      return;
    }
    button.hidden = false;
    button.addEventListener('click', toggle);
    apply();
  });
})();
