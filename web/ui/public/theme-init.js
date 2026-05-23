// Apply the saved (or system) theme before first paint to avoid a flash of the wrong theme.
// Served as a same-origin asset (not inline) so it is allowed under the operator's strict CSP
// (default-src 'self'), which forbids inline scripts.
(function () {
  try {
    var stored = localStorage.getItem("ksail-theme");
    var dark = stored ? stored === "dark" : matchMedia("(prefers-color-scheme: dark)").matches;
    if (dark) document.documentElement.classList.add("dark");
  } catch (e) {
    /* ignore: theme is best-effort */
  }
})();
